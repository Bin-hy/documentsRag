# Multi-Query 多查询 + RAG-Fusion 融合 Plan

## 架构概览

在现有「单查询改写 → 单路检索」链路上新增**多查询编排层**，改动集中在三层：

1. **retriever 层**：新增 `SearchMulti(ctx, req, queries []string)` —— 接收多路查询，并行检索（向量+BM25 每路内部 RRF），跨查询再按 RRF 融合为 Top-K。新增独立 `FuseMultiQuery`（多路结果 RRF 融合）函数，复用现有 `FuseRRF` 的思路与 `RetrieveResult` 类型。
2. **rag.Engine 层**：`prepare` 中，当 `multiQueryEnabled` 时，用 LLM 生成 N 个变体（替代单查询改写），构造 `queries = [主查询] + 变体`，调用 `SearchMulti`；生成失败降级为单查询走现有 `Search`。关闭时行为与现状完全一致。
3. **config 层**：`RAGConfig` 新增 `MultiQueryEnabled *bool`、`MultiQueryCount int`（默认 3）、`MultiQueryConcurrency int`（默认 3）；`applyDefaults` 设默认值。

## 核心数据结构

### RAGConfig 扩展（internal/config/config.go）
```go
type RAGConfig struct {
    // ... 现有字段
    MultiQueryEnabled    *bool `yaml:"multi_query_enabled"`     // nil 视为关闭（保守默认，保持现状）
    MultiQueryCount      int   `yaml:"multi_query_count"`       // 变体数，默认 3
    MultiQueryConcurrency int  `yaml:"multi_query_concurrency"` // 多路并发上限，默认 3
}
// MultiQueryOn 是否启用多查询（nil 视为关闭）
func (c RAGConfig) MultiQueryOn() bool {
    return c.MultiQueryEnabled != nil && *c.MultiQueryEnabled
}
```

### retriever 新接口
```go
// SearchMulti 多查询检索：每路并行 Search，跨查询 RRF 融合为 Top-K
func (r *defaultRetriever) SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error)

// FuseMultiQuery 多路结果 RRF 融合（每路是一个排名列表）
// listOfResults: 每路查询的检索结果（已排序）；k: RRF 常数；topK: 返回条数
func FuseMultiQuery(listOfResults [][]RetrieveResult, k, topK int) []RetrieveResult
```

### promptTemplates 扩展（internal/rag/prompt.go）
```go
const defaultMultiQueryTemplate = `根据用户问题生成 N 个不同表达角度的检索查询变体，用于多路召回。变体应覆盖同义改写、不同细节粒度、可能的隐含子主题。结合对话历史消解指代。只输出 JSON 数组字符串，如 ["变体1","变体2","变体3"]，不要任何解释。
{{if .History}}
对话历史：
{{- range .History}}
{{.Role}}: {{.Content}}
{{- end}}
{{end}}
用户问题：{{.Question}}
变体数：{{.Count}}
查询变体：`

type promptTemplates struct {
    system  string
    context string
    rewrite string
    multiQuery string // 多查询变体生成模板
}
```

## 模块设计

### retriever（internal/retriever/retriever.go）
**职责：** 多路检索与融合。
**对外接口：** `SearchMulti`、`FuseMultiQuery`。
**实现要点：**
- `SearchMulti`：`errgroup` 并发执行每路 `Search`（上限 `MultiQueryConcurrency`），收集各路结果 → `FuseMultiQuery` 融合 → Top-K
- 单路失败：降级忽略该路（记录 warn），不整体失败（至少一路成功即可）
- `FuseMultiQuery`：每路结果按 rank 贡献 `1/(k+rank)`，跨路累加，按分数降序取 topK；复用 `findInVector` 取完整文档信息
- 融合逻辑与现有 `FuseRRF` 正交（一个融合向量+BM25 两路，一个融合多查询多路），两者不冲突

### rag.Engine（internal/rag/engine.go）
**职责：** 编排多查询 vs 单查询。
**改动：**
- `prepare` 中：
  ```go
  if e.cfg.MultiQueryOn() {
      queries, err := e.multiQuery(ctx, history, question) // 主查询 + N 变体
      if err != nil {
          slog.Warn("多查询生成失败，降级单查询", "err", err)
          // 走现有单查询路径（改写 or 原文）
      } else {
          chunks, err := e.retriever.SearchMulti(ctx, req, queries)
          // 跳过单次 Search，直接用多路融合结果
      }
  } else {
      // 现有逻辑不变（单查询改写 + Search）
  }
  ```
- `multiQuery(ctx, history, question) ([]string, error)`：调 LLM 生成变体 JSON 数组，解析失败返回 error（触发降级）；返回 `[]string{question}` + 变体（或改写后主查询 + 变体——按已决策「多查询替代单查询改写」，主查询用原问题，变体由 LLM 基于原文+历史生成）
- 日志：`slog.Info("多查询生成完成", "变体数", len(queries), "变体", queries)`；`slog.Info("多路检索完成", "路数", len(queries), "融合数", len(chunks))`

### config（internal/config/config.go、configs/config.yaml）
**职责：** 开关与参数。
**改动：** 字段 + `applyDefaults`（`MultiQueryCount` 默认 3、`MultiQueryConcurrency` 默认 3）；config.yaml 注释示例（默认关闭 `multi_query_enabled: false`，用户可开）。

### 测试
- `internal/retriever/retriever_test.go`：`TestFuseMultiQuery`（多路融合去重、跨路重复靠前、topK 截断）
- `internal/rag/engine_test.go`：mock retriever 验证多查询路径调用 `SearchMulti`、生成失败降级走单查询、关闭时走现有路径
- `internal/config/`：默认值测试（若现有 config 测试存在）

## 模块交互

```
rag.Engine.prepare
  ├─ multiQueryOn？
  │   ├─ 是：LLM 生成 N 变体（JSON）→ queries=[原文]+变体
  │   │        → retriever.SearchMulti(ctx, req, queries)
  │   │            ├─ errgroup 并行 N+1 路 Search（每路向量+BM25 RRF）
  │   │            └─ FuseMultiQuery 多路 RRF → Top-K
  │   │        → 重排序（现有）→ 上下文组装（现有）
  │   └─ 否：现有单查询改写 → Search → RRF（现状不变）
```

## 文件组织

```
internal/config/
├── config.go            — 修改：RAGConfig 加 3 字段 + applyDefaults
configs/config.yaml      — 修改：rag 段加 multi_query 注释示例
internal/retriever/
├── retriever.go         — 修改：SearchMulti 实现
├── rrf.go               — 修改：FuseMultiQuery 实现
├── rrf_test.go 或 retriever_test.go — 修改：TestFuseMultiQuery
internal/rag/
├── engine.go            — 修改：prepare 分支 + multiQuery 方法
├── prompt.go            — 修改：multiQuery 模板 + 渲染
├── engine_test.go       — 修改：多查询路径/降级/关闭用例
internal/api/docs/       — 修改：如 handler 无变更则不动（策略默认关，接口不变）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 融合入口 | retriever 新增 SearchMulti | 用户选定；检索融合逻辑归属 retriever，rag.Engine 只编排 |
| 多查询 vs 改写 | 多查询替代单查询改写（互斥） | 用户选定；避免两级 LLM 调用成本 |
| 主查询 | 用原问题作为第 1 路 | 保留原始意图，变体补充多角度；改写消解指代由变体 prompt 承担 |
| 每路失败 | 忽略该路（warn）不整体失败 | N1 降级：至少一路成功即可；与单查询降级一致 |
| 融合算法 | 统一 RRF（跨路 1/(k+rank) 累加） | 与现有 FuseRRF 思路一致，K 默认 60，跨查询重复命中靠前 |
| 并发 | errgroup + 上限（默认 3） | N2 性能：避免 N+1 倍串行延迟 |
| 默认开关 | 关闭（nil=false） | 保守默认保持现状，用户显式开启（阶段四策略配置再细化） |
| 降级 | 变体生成失败 → 单查询（原问题） | N1：LLM 异常不影响问答可用性 |
