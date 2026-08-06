# Multi-Query 多查询 + RAG-Fusion 融合 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | RAGConfig 加 3 字段 + MultiQueryOn + applyDefaults |
| 修改 | `configs/config.yaml` | rag 段加 multi_query 注释示例 |
| 修改 | `internal/retriever/retriever.go` | SearchMulti 实现 |
| 修改 | `internal/retriever/rrf.go` | FuseMultiQuery 实现 |
| 修改 | `internal/retriever/retriever_test.go` | TestFuseMultiQuery 用例 |
| 修改 | `internal/rag/prompt.go` | multiQuery 模板 + 渲染 |
| 修改 | `internal/rag/engine.go` | prepare 分支 + multiQuery 方法 |
| 修改 | `internal/rag/engine_test.go` | fakeRetriever 补 SearchMulti + 多查询用例 |

## T1: config 多查询配置项

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. `RAGConfig` 增加字段：`MultiQueryEnabled *bool \`yaml:"multi_query_enabled"\``、`MultiQueryCount int \`yaml:"multi_query_count"\``、`MultiQueryConcurrency int \`yaml:"multi_query_concurrency"\``
2. 新增方法 `func (c RAGConfig) MultiQueryOn() bool { return c.MultiQueryEnabled != nil && *c.MultiQueryEnabled }`（nil 视为关闭）
3. `applyDefaults`：`MultiQueryCount` 默认 3、`MultiQueryConcurrency` 默认 3（<=0 时）
4. config.yaml rag 段加注释示例（默认关闭，用户可开启）

**验证：** `go build ./internal/config/...` 通过；`go test ./internal/config/`（如有）通过

## T2: retriever 多路融合函数

**文件：** `internal/retriever/rrf.go`、`internal/retriever/retriever_test.go`
**依赖：** 无（与 T1 并行）
**步骤：**
1. `rrf.go` 新增 `FuseMultiQuery(listOfResults [][]RetrieveResult, k, topK int) []RetrieveResult`：
   - 每路结果按 rank 贡献 `1/(k+rank+1)` 累加（k<=0 时默认 60）
   - 跨路同 ID 分数累加 → 按分数降序 → 取前 topK
   - 用 `findInVector` 从任一路取完整文档信息（Content/Metadata）
2. `retriever_test.go` 新增 `TestFuseMultiQuery`：
   - 用例 A：两路结果有交集，交集文档分数更高排前
   - 用例 B：三路无交集，顺序按各自 rank 融合
   - 用例 C：topK 截断生效
   - 用例 D：结果去重（无重复 ID）

**验证：** `go test ./internal/retriever/ -run TestFuseMultiQuery -v` 全绿

## T3: retriever SearchMulti 多路检索

**文件：** `internal/retriever/retriever.go`
**依赖：** T2（FuseMultiQuery）
**步骤：**
1. `defaultRetriever` 新增方法：
   ```go
   func (r *defaultRetriever) SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error)
   ```
2. 实现：`errgroup` 并发每路 `Search(ctx, RetrieveRequest{Query: q, TopK: req.TopK, Filter: req.Filter})`，上限 `r.config.MultiQueryConcurrency`（<=0 时 3）
3. 单路失败：记录 warn 忽略该路；全部失败才返回 error
4. 融合：`FuseMultiQuery(results, 60, req.TopK)` 返回
5. `Retriever` 接口增加 `SearchMulti` 签名（retriever.go 顶部接口定义）

**验证：** `go build ./internal/retriever/...` 通过（engine/eval 的 fake 先不实现，会编译失败——见 T4 一并处理）

## T4: rag 模板与引擎多查询编排

**文件：** `internal/rag/prompt.go`、`internal/rag/engine.go`
**依赖：** T1、T3
**步骤：**
1. `prompt.go`：
   - 新增 `defaultMultiQueryTemplate`（输出 JSON 数组，含 History/Question/Count）
   - `promptTemplates` 增加 `multiQuery` 字段；`loadPromptTemplates` 加载（新增 `MultiQueryTemplatePath` 配置或直接内置）
   - 新增 `renderMultiQuery(history, question, count, tpl)` 渲染函数
2. `engine.go`：
   - `prepare` 增加分支：`if e.cfg.MultiQueryOn() { ... }` 调用 `multiQuery` 生成变体 → `SearchMulti`；失败 `slog.Warn` 降级走现有单查询路径
   - 新增 `multiQuery(ctx, history, question) ([]string, error)`：调 LLM 生成 JSON 数组，解析失败返回 error；返回 `[]string{question}` + 变体（主查询=原问题）
   - 日志：`slog.Info("多查询生成完成", "变体数", n)`、`slog.Info("多路检索完成", "路数", n, "融合数", len(chunks))`
3. `RAGConfig` 增加 `MultiQueryTemplatePath`（可选，复用模板加载模式）

**验证：** `go build ./...` 通过（此时 engine_test 的 fakeRetriever 需补 SearchMulti——见 T5）

## T5: 测试适配与新增用例

**文件：** `internal/rag/engine_test.go`
**依赖：** T4
**步骤：**
1. `fakeRetriever` 增加 `SearchMulti` 实现（模拟多路检索：每路返回固定结果，可配置 error）
2. 新增用例：
   - `TestAsk_MultiQueryEnabled`：`MultiQueryEnabled=true` 且 fakeLLM 返回变体 JSON → 断言调用 SearchMulti、日志含变体数、回答正常
   - `TestAsk_MultiQueryFallback`：fakeLLM 生成变体失败 → 降级单查询（走 Search），回答正常不报错
   - `TestAsk_MultiQueryDisabled`：`MultiQueryEnabled=false` → 走现有 Search 路径（断言 Search 被调、SearchMulti 未调）
3. `TestAsk_RewriteDisabled` 等现有用例不回归（多查询默认关，行为不变）

**验证：** `go test ./internal/rag/ -v` 全部通过

## T6: 全量验证 + 冒烟

**文件：** 无新增
**依赖：** T1-T5
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿
2. 临时配置开启 `multi_query_enabled: true`，启动服务，问答观察日志出现「多查询生成完成」「多路检索完成」（真实 LLM 环境）
3. 关闭配置确认行为与之前一致（单查询路径日志）
4. 冒烟后恢复默认配置

**验证：** 上述命令全绿；冒烟日志符合预期

## 执行顺序

```
T1 ─┐
    ├→ T4 → T5 → T6
T2 → T3 ─┘
```

T1（config）与 T2（FuseMultiQuery）并行；T3 依赖 T2；T4 依赖 T1+T3；T5 依赖 T4；T6 收尾。
