# Review 修复（P0 + 配置接线）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `frontend/package.json` | 新增 dompurify 依赖 |
| 修改 | `frontend/src/components/MarkdownRenderer.vue` | 接入 DOMPurify 消毒 |
| 修改 | `internal/api/handler_chat.go` | ChatStream 补 nil engine 判断 |
| 修改 | `internal/eval/evaluator.go` | sessionID 唯一 + sources 传递 + recover |
| 修改 | `internal/app/app.go` | webui.Register 失败路径补 vs.Close() |
| 修改 | `internal/reranker/reranker.go` | topN 回退到 config.TopN |
| 修改 | `internal/search/bocha.go` | 加 rate.Limiter |
| 修改 | `internal/embedding/embedder.go` | NewEmbedder 按 provider 分发 + 返回 error |
| 修改 | `internal/app/rebuild.go` | NewEmbedder 调用点处理 error |
| 修改 | `internal/rag/engine.go` | HyDE 入口改调 shouldHyde |
| 修改 | `internal/api/router.go` | 补 BearerAuth 安全定义 |
| 重新生成 | `internal/api/docs/docs.go` | swag init 重生成 |

## T1: 前端 XSS 消毒

**文件：** `frontend/package.json`、`frontend/src/components/MarkdownRenderer.vue`
**依赖：** 无
**步骤：**
1. `cd frontend && pnpm add dompurify && pnpm add -D @types/dompurify`
2. `MarkdownRenderer.vue` 中 import DOMPurify，`marked.parse` 输出经 `DOMPurify.sanitize()` 后再赋给渲染变量

**验证：** `cd frontend && npx vitest run` 全绿；构造含 `<script>` 的测试用例确认被剥离

## T2: ChatStream nil engine 防护

**文件：** `internal/api/handler_chat.go`
**依赖：** 无
**步骤：**
1. 在 `ChatStream` 函数 `h.engine().StreamAsk(...)` 调用前加 nil 判断，与 `Chat` 函数（约 :81）的写法对齐
2. engine 为 nil 时 `Fail(c, CodeInternal, "引擎未就绪")` 并 return

**验证：** `go build ./internal/api/...` 编译通过

## T3: eval 三处修复

**文件：** `internal/eval/evaluator.go`
**依赖：** 无
**步骤：**
1. `evalAsk`（:164）sessionID 改为 `fmt.Sprintf("eval-%d-%s", sampleIndex, truncate(s.Question, 20))`，需给 `evalAsk` 加 `sampleIndex int` 参数并更新调用方
2. `truncate` 函数改为按 `[]rune` 截断
3. `evalJudge`（:191）`JudgeFaithfulness` 第 4 参数从 `nil` 改为 `res.Sources`
4. `runConcurrent`（:226）`fn(i)` 调用外包 `defer recover()`，panic 时 `slog.Error` 记录

**验证：** `go build ./internal/eval/... && go test ./internal/eval/...` 通过

## T4: 启动失败资源清理

**文件：** `internal/app/app.go`
**依赖：** 无
**步骤：**
1. `app.go:174` `webui.Register` 失败的 if 块内，`cancel()` 之后加 `vs.Close()`

**验证：** `go build ./internal/app/...` 编译通过

## T5: reranker.top_n 接线

**文件：** `internal/reranker/reranker.go`
**依赖：** 无
**步骤：**
1. `apiReranker.Rerank`（:73）函数开头加：`if topN <= 0 { topN = r.config.TopN }`
2. 同样检查 `llmReranker.Rerank`（reranker_llm.go），如有需要也加

**验证：** `go build ./internal/reranker/... && go test ./internal/reranker/...` 通过

## T6: web_search.qps 接线

**文件：** `internal/search/bocha.go`
**依赖：** 无
**步骤：**
1. `bochaProvider` 结构体加 `limiter *rate.Limiter` 字段
2. 构造函数中初始化 `limiter: rate.NewLimiter(rate.Limit(cfg.QPS), cfg.QPS)`，加 qps≤0 兜底（参考 reranker.go:46-52 的写法）
3. `Search` 方法开头加 `limiter.Wait(ctx)`

**验证：** `go build ./internal/search/... && go test ./internal/search/...` 通过

## T7: embedder.provider 接线

**文件：** `internal/embedding/embedder.go`、`internal/app/rebuild.go`、`internal/app/app.go`
**依赖：** 无
**步骤：**
1. `NewEmbedder` 签名改为 `func NewEmbedder(cfg config.EmbedderConfig) (*Embedder, error)`
2. 函数开头按 `strings.ToLower(cfg.Provider)` 分发：`""` 或 `"openai"` 走现有逻辑返回 `(&Embedder{...}, nil)`；其他值返回 `(nil, fmt.Errorf("未知 embedding provider: %s", cfg.Provider))`
3. `rebuild.go:34`、`app.go:84`、`app.go:364` 三处调用点改为 `emb, err := embedding.NewEmbedder(...)`，err 非 nil 时返回/记录错误

**验证：** `go build ./... && go test ./internal/embedding/... ./internal/app/...` 通过

## T8: hyde_skip_simple 接线

**文件：** `internal/rag/engine.go`
**依赖：** 无
**步骤：**
1. `engine.go:966` 附近，找到 HyDE 入口判断 `eff.HyDE == "on" && e.embedder != nil && eff.Query != "multi"`
2. 确认该处能拿到 `route` 变量（路由判定结果），将判断改为调用 `e.shouldHyde(route)`，同时保留 `eff.Query != "multi"` 的排除条件
3. 若该处没有 `route` 变量，需向上找到路由判定结果传入

**验证：** `go build ./internal/rag/... && go test ./internal/rag/...` 通过

## T9: Swagger 重生成

**文件：** `internal/api/router.go`、`internal/api/docs/docs.go`
**依赖：** T1-T8（最后执行，避免重复生成）
**步骤：**
1. `router.go` 的 swagger 注解区补 `BearerAuth` 安全定义（`@securityDefinitions.apikey BearerAuth`）
2. 重跑 `swag init`（确认 swag 已安装：`which swag || go install github.com/swaggo/swag/cmd/swag@latest`）
3. 检查生成的 `docs.go` 包含 8 条缺失路由

**验证：** `go build ./internal/api/...` 通过；grep 确认 `supported-types`、`/videos/{id}/stream`、`/mcp/my/` 在 docs.go 中

## 执行顺序

```
T1（前端）─┐
T2（api） ─┤
T3（eval）─┤
T4（app） ─┼─ 全部可并行，无相互依赖
T5（reranker）─┤
T6（search）─┤
T7（embedding+app）─┤
T8（rag） ─┘
     │
     ▼
T9（swagger，最后执行）
```
