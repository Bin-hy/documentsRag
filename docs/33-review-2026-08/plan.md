# Review 修复（P0 + 配置接线）Plan

## 架构概览

本次修复不涉及新架构，全部是在现有模块内的定点修复。按模块分组：

- **前端**：`MarkdownRenderer.vue` 接入 DOMPurify
- **API 层**：`handler_chat.go` ChatStream 补 nil engine 判断；`router.go` 补 BearerAuth 定义
- **eval 包**：sessionID 唯一化、sources 传递、recover 捕获
- **app 包**：`webui.Register` 失败路径补 `vs.Close()`
- **配置接线**：reranker 读 top_n、bocha 加限流、embedder 按 provider 分发、rag 接 shouldHyde
- **Swagger**：重跑 `swag init`

## 核心接口与数据结构变化

### 签名变化（1 处）

```go
// internal/embedding/embedder.go
// 修复前
func NewEmbedder(cfg config.EmbedderConfig) *Embedder
// 修复后：provider 未知时返回 error
func NewEmbedder(cfg config.EmbedderConfig) (*Embedder, error)
```

影响面：`internal/app/rebuild.go` 的 `BuildRuntime` 和 `AssembleEvalDeps` 两处调用点处理 error。

### 行为变化（无签名变化）

```go
// internal/reranker/reranker.go
// topN <= 0 时回退到 r.config.TopN
func (r *apiReranker) Rerank(ctx, query, candidates, topN) ([]RerankResult, error)

// internal/search/bocha.go — bochaProvider 新增 limiter 字段
type bochaProvider struct {
    // ... 现有字段
    limiter *rate.Limiter
}

// internal/rag/engine.go — HyDE 入口改调 e.shouldHyde(route)
```

### eval 包内部变化

```go
// sessionID 加样本序号
sessionID := fmt.Sprintf("eval-%d-%s", sampleIndex, truncate(s.Question, 20))

// truncate 改为按 rune 截断
func truncate(s string, n int) string

// runConcurrent 的 fn 调用处补 recover
defer func() {
    if r := recover(); r != nil {
        slog.Error("评测样本 panic", "index", i, "panic", r)
    }
}()
```

### 前端依赖

```
frontend/package.json 新增：dompurify + @types/dompurify
```

## 模块交互

不改变模块间调用关系，仅在现有调用链上修补：

```
前端渲染链：marked.parse → DOMPurify.sanitize（新增）→ v-html
ChatStream：h.engine() → nil 判断（新增）→ StreamAsk
eval 链：evalAsk 收集 res.Sources → evalJudge 传给 JudgeFaithfulness（修复断点）
配置接线：config 快照 → reranker/bocha/embedder/rag 读取（修复断点）
```

## 文件组织

```
docs-rag/
├── frontend/
│   ├── package.json                          — 新增 dompurify 依赖
│   └── src/components/MarkdownRenderer.vue   — 接入 DOMPurify
├── internal/
│   ├── api/
│   │   ├── handler_chat.go                   — ChatStream 补 nil 判断
│   │   ├── router.go                         — 补 BearerAuth 安全定义
│   │   └── docs/docs.go                      — swag init 重生成
│   ├── app/app.go                            — webui.Register 失败路径补 vs.Close()
│   ├── eval/evaluator.go                     — sessionID 唯一 + sources 传递 + recover
│   ├── reranker/reranker.go                  — topN 回退到 config.TopN
│   ├── search/bocha.go                       — 加 rate.Limiter
│   ├── embedding/embedder.go                 — NewEmbedder 按 provider 分发 + 返回 error
│   ├── app/rebuild.go                        — NewEmbedder 调用点处理 error
│   └── rag/
│       ├── routing.go                        — shouldHyde 保持现有逻辑
│       └── engine.go                         — HyDE 入口改调 shouldHyde
└── configs/config.yaml                       — 无需改动（配置项已存在）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| XSS 消毒库 | DOMPurify | 事实标准，维护活跃，与 marked 搭配是社区惯例 |
| `NewEmbedder` 签名变化 | 返回 `(*Embedder, error)` | provider 分发必须有错误出口；仅 2 处调用点，影响可控 |
| `reranker.top_n` 接线方式 | `topN <= 0` 时回退配置值 | 向后兼容：现有调用方都显式传 topN，行为不变；配置值作为兜底默认 |
| `hyde_skip_simple` 接线方式 | engine.go 改调 `shouldHyde(route)` | 函数已存在且逻辑正确，只需接上调用点 |
| sessionID 唯一性 | 加样本序号 `eval-{index}-{question}` | 简单直接，不引入额外依赖 |
| Swagger 重生成 | 重跑 `swag init` | 注解已在源码里，只是生成物过时 |
