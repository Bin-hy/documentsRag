# RAG 路由 + HyDE 假设文档 Plan

## 架构概览

两个策略在 `rag.Engine` 编排层实现，作为 **prepare 之前的外层分支**（与阶段二同模式）：

1. **RAG 路由**：Ask/StreamAsk 入口先做复杂度判定 → 按 strategy 分流：
   - `direct` → 常规 prepare（或阶段一多查询）
   - `multi_query` → 走阶段一多查询路径
   - `decomposition` → 走阶段二分解路径
   - 判定失败 → 回退默认策略（`routing_fallback`，默认 multi_query）
2. **HyDE**：作为**检索增强层**——生成假设文档 → embed → 用假设文档向量检索 + 原查询检索 → RRF 融合。在路由分流后、实际检索时应用（作用于所选路径的检索环节）。

两者独立开关；HyDE 叠加在路由选定路径的检索之上。

### 关键设计：HyDE 向量检索能力

现有 `retriever.Search` 接收文本 Query 并内部 embed。HyDE 需要「用假设文档向量直接检索」。新增：
- `retriever.SearchByVector(ctx, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error)`：公开方法，内部走现有 vectorSearch 的向量检索逻辑（不含 BM25/重排——HyDE 融合后再统一走重排）
- rag 层持有 `embedding.Embedder`（NewEngine 加参数），HyDE 生成假设文档 → `embedder.Embed` → `SearchByVector`

## 核心数据结构

### RAGConfig 扩展（internal/config/config.go）
```go
type RAGConfig struct {
    // ... 现有
    RoutingEnabled    *bool  `yaml:"routing_enabled"`      // nil 视为关闭
    RoutingFallback   string `yaml:"routing_fallback"`     // direct / multi_query / decomposition，默认 multi_query
    HyDEEnabled       *bool  `yaml:"hyde_enabled"`         // nil 视为关闭
    HyDESkipSimple    *bool  `yaml:"hyde_skip_simple"`     // nil 视为 true（默认跳过简单查询）
    RoutingTemplatePath  string `yaml:"routing_template_path"`
    HyDETemplatePath     string `yaml:"hyde_template_path"`
}
func (c RAGConfig) RoutingOn() bool { return c.RoutingEnabled != nil && *c.RoutingEnabled }
func (c RAGConfig) HyDEOn() bool    { return c.HyDEEnabled != nil && *c.HyDEEnabled }
```

### promptTemplates 扩展（internal/rag/prompt.go）
```go
type promptTemplates struct {
    // ... 现有
    routing string // 复杂度判定模板（JSON：{complexity, strategy, reasoning}）
    hyde    string // 假设文档生成模板
}
```

### 内部结构（internal/rag/routing.go 新建）
```go
// routeResult 路由判定结果
type routeResult struct {
    Complexity string // simple / medium / complex
    Strategy   string // direct / multi_query / decomposition
    Reasoning  string
}
```

### RAGEngine 扩展（internal/rag/engine.go）
```go
type RAGEngine struct {
    // ... 现有
    embedder embedding.Embedder // HyDE 用（nil 时 HyDE 禁用）
}
func NewEngine(cfg config.RAGConfig, l llm.LLM, rt retriever.Retriever, hs HistoryStore, emb embedding.Embedder) Engine
```

## 模块设计

### retriever（internal/retriever/retriever.go）
**职责：** 暴露按向量检索。
```go
// SearchByVector 按向量检索（HyDE 用）：不走查询 embed，直接向量搜索
func (r *defaultRetriever) SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error)
```
实现：复用 vectorSearch 内核对 vectorstore.Search 的调用（抽出公共方法 `vectorSearchByVec`）。

### rag.Engine 编排（internal/rag/engine.go + routing.go 新建）
**职责：** 路由分流 + HyDE 增强。
**改动：**
- `Ask`/`StreamAsk` 开头（现有策略分支前）：
  ```go
  if e.cfg.RoutingOn() {
      route, ok, err := e.routeQuery(ctx, question)
      if err == nil && ok {
          // 按 route.Strategy 分流：direct→常规；multi_query→阶段一路径；decomposition→阶段二
          // 判定失败（err!=nil 或 !ok）→ fallback 策略
      }
  }
  ```
- `routeQuery(ctx, question) (routeResult, bool, error)`：LLM 判定 `{complexity, strategy, reasoning}`；解析失败 → 返回 fallback 策略（如 multi_query），ok=true（不落常规）
- **HyDE 集成**：在路由选定路径的检索环节应用。抽出 `e.hydeEnhance(ctx, query, o)`：
  ```go
  // 生成假设文档 → embed → SearchByVector（HyDE 向量路）+ Search（原查询路）→ FuseMultiQuery 融合
  func (e *RAGEngine) hydeSearch(ctx, query string, o AskOptions) ([]retriever.RetrieveResult, error)
  ```
  - `hyde_skip_simple` 且路由判定 simple → 跳过 HyDE
  - Embedding 失败 → 降级为原查询 Search，不中断
- 现有 `searchSubQuery`（阶段二）也接入 HyDE（子问题检索可叠加 HyDE）

### config（internal/config/config.go、configs/config.yaml）
字段 + applyDefaults（`RoutingFallback` 默认 "multi_query"、`HyDESkipSimple` 默认 true）+ yaml 注释示例（默认关闭）。

### 测试
- `internal/rag/engine_test.go`：
  - `TestAsk_RoutingDirect`：判定 simple/direct → 常规路径
  - `TestAsk_RoutingMultiQuery`：判定 medium/multi_query → 多查询路径（SearchMulti 被调）
  - `TestAsk_RoutingDecomposition`：判定 complex/decomposition → 分解路径
  - `TestAsk_RoutingFallback`：判定失败 → 回退 multi_query
  - `TestAsk_HyDE`：启用 HyDE → 假设文档生成 + SearchByVector + 融合
  - `TestAsk_HyDESkipSimple`：simple 查询跳过 HyDE
  - `TestAsk_HyDEEmbedFail`：embedding 失败 → 降级原查询
- fakeRetriever 需补 `SearchByVector`
- fakeLLM 按 prompt 特征返回路由 JSON

## 模块交互

```
Ask/StreamAsk
  ├─ RoutingOn?
  │   ├─ 是：routeQuery → {strategy}
  │   │     ├─ direct → 常规 prepare（可叠加 HyDE）
  │   │     ├─ multi_query → 阶段一多查询路径（可叠加 HyDE）
  │   │     ├─ decomposition → 阶段二分解路径（子问题检索可叠加 HyDE）
  │   │     └─ 判定失败 → fallback（默认 multi_query）
  │   └─ 否 → 现有策略分支（Decomposition/Step-Back）→ 常规
  └─ HyDE 增强（各路径检索环节）：
      hydeSearch：生成假设文档 → embedder.Embed → SearchByVector（HyDE 路）
                 + Search（原查询路）→ FuseMultiQuery 融合
```

## 文件组织

```
internal/config/
├── config.go            — 修改：5 字段 + RoutingOn/HyDEOn + applyDefaults
configs/config.yaml      — 修改：rag 段加 routing/hyde 注释示例
internal/retriever/
├── retriever.go         — 修改：SearchByVector + 抽出 vectorSearchByVec
internal/rag/
├── engine.go            — 修改：RAGEngine 加 embedder、NewEngine 签名、Ask/StreamAsk 路由分支
├── routing.go           — 新建：routeQuery / hydeSearch / 分流执行
├── prompt.go            — 修改：routing/hyde 模板 + render
├── engine_test.go       — 修改：7 个新用例 + fakeRetriever/fakeLLM 扩展
internal/app/
├── app.go               — 修改：NewEngine 传 emb（两处装配）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| HyDE 向量检索 | retriever 新增 SearchByVector + rag 持 embedder | 用户确认「引擎内嵌 embedder」；真正的「假设文档向量检索」 |
| 路由分流实现 | Ask/StreamAsk 入口分支，按 strategy 调已有路径 | 复用阶段一/二已有实现，不复制逻辑 |
| 判定失败 | 回退配置的默认策略（默认 multi_query） | spec F3；不落常规（避免简单问题绕开增强） |
| HyDE 融合 | FuseMultiQuery（HyDE 路 + 原查询路） | 复用阶段一融合函数，双路互补 |
| skip_simple | 默认 true | 简单查询 HyDE 收益低、成本高 |
| Embedding 失败 | 降级原查询 Search | N4 配额问题兜底，问答不中断 |
| StreamAsk | 路由判定同步，HyDE 检索同步，最终流式 | 与阶段二一致，前端无改动 |
| 默认关闭 | routing/hyde 均默认关 | 保持现状，用户显式开启（阶段四再细化） |
