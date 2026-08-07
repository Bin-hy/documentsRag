# 融合后整体重排 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/retriever/types.go` | RetrieveRequest + SkipRerank |
| 修改 | `internal/retriever/retriever.go` | searchFused/rerankIfEnabled 抽取、Search/SearchMulti 重构、Rerank 接口方法 |
| 修改 | `internal/rag/routing.go` | hydeSearch 原查询路 SkipRerank + 融合后 Rerank |
| 修改 | `internal/rag/decompose.go` | searchSubQuery SkipRerank、tryDecompose/tryStepBack 汇总后 Rerank |
| 修改 | `internal/retriever/retriever_test.go` | SkipRerank/整体重排/Trace 断言 |
| 修改 | `internal/rag/engine_test.go` | fakeRetriever + Rerank 方法、多路重排断言 |

## T1: retriever 抽取 searchFused 与 rerankIfEnabled，重构 Search

**文件：** `internal/retriever/retriever.go`
**依赖：** 无
**步骤：**
1. 从 `Search` 中抽取 `searchFused(ctx, query, topK, filter) []RetrieveResult`：向量+BM25 并行 → `FuseRRF` → 返回融合结果（**不含** rerank、不含 topK 截断、不含 Trace）
2. 新增 `rerankIfEnabled(ctx, query, results, topN, trace) []RetrieveResult`：
   - `!EnableReranker || reranker==nil || len(results)==0` 直接返回原结果
   - 构造 candidates → `r.reranker.Rerank` → 失败 `slog.Warn` + 返回原结果（F8 降级）
   - 成功：转换结果；`trace != nil` 时上报 `RetrieveTrace{RerankBefore, RerankAfter}`（toRankedItems）
3. `Search` 重构：`searchFused` → 现有 Trace(method/recalled) → topK 截断 → `if !req.SkipRerank { rerankIfEnabled }`（单路行为不变，F6）

**验证：** `go build ./internal/retriever/...` 编译通过；`go test ./internal/retriever/...` 现有测试全绿（Search 行为无回归）

## T2: SkipRerank 标记 + Rerank 接口方法 + SearchMulti 改造

**文件：** `internal/retriever/types.go`、`internal/retriever/retriever.go`
**依赖：** T1
**步骤：**
1. `types.go`：`RetrieveRequest` 增加 `SkipRerank bool`（注释：true=跳过路内 rerank，由调用方融合后统一重排）
2. `retriever.go`：`Retriever` 接口增加 `Rerank(ctx, query, results, topN, trace func(RetrieveTrace)) ([]RetrieveResult, error)`（注释：整体重排，关闭/失败降级返回原结果；trace 用于思考链路前后对比）
3. `defaultRetriever` 实现 `Rerank` = `rerankIfEnabled(ctx, query, results, topN, trace)`, nil
4. `SearchMulti` 每路：`r.Search(...)` 改为 `r.searchFused(ctx, q, topK, req.Filter)`（路内不再 rerank，N1）
5. `SearchMulti` 融合后：`fused = r.rerankIfEnabled(ctx, req.Query, fused, topK, req.Trace)`（整体重排，AC2）

**验证：** `go build ./internal/retriever/...` 编译通过；`go test ./internal/retriever/...` 现有测试全绿

## T3: rag 层 hydeSearch 融合后整体重排

**文件：** `internal/rag/routing.go`
**依赖：** T2
**步骤：**
1. `hydeSearch` 原查询路 `Search` 调用增加 `SkipRerank: true`
2. `FuseMultiQuery` 融合后、返回前：`fused, err = e.retriever.Rerank(ctx, query, fused, e.cfg.TopK, traceSinkForRequest(sink, query))`（err 理论上为 nil，Rerank 内部降级；防御性处理）
3. 思考链路：Rerank 的 trace 回调使整体重排产生 `StepRerank`（Before=融合结果、After=整体重排结果）

**验证：** `go build ./internal/rag/...` 编译通过；HyDE 相关测试（TestAsk_HyDE 等）全绿

## T4: rag 层 tryDecompose / tryStepBack 汇总后整体重排

**文件：** `internal/rag/decompose.go`
**依赖：** T2
**步骤：**
1. `searchSubQuery` 的单路 `Search` 调用增加 `SkipRerank: true`（MultiQueryOn 时的 SearchMulti 路径已整体 rerank，不变）
2. `tryDecompose`：`allChunks` 汇总后、空检查之前，增加 `allChunks, err = e.retriever.Rerank(ctx, question, allChunks, e.cfg.MaxChunks, traceSinkForRequest(sink, question))`；err 处理为防御（正常 nil）
3. `tryStepBack`：`allChunks := append(backChunks, origChunks...)` 后增加同样的 `Rerank(ctx, question, allChunks, e.cfg.MaxChunks, traceSinkForRequest(sink, question))`，空检查之前
4. 两处 Rerank 均传入思考链路 trace（保证 StepRerank 展示）

**验证：** `go build ./internal/rag/...` 编译通过；Decomposition/StepBack 测试全绿

## T5: 整体重排的思考链路 trace 接入

**文件：** `internal/rag/routing.go`、`internal/rag/decompose.go`
**依赖：** T3、T4
**步骤：**
1. 确认三个 `Rerank` 调用点均传入 `traceSinkForRequest(sink, query)` 回调（sink 为各函数已有的 `o.Sink`）
2. `Rerank` 接口签名含 `trace func(RetrieveTrace)` 参数（T2 已按最终签名实现，避免返工）
3. 验证 thinking：多路场景出现 `StepRerank`（Before=融合/汇总结果、After=整体重排结果），逐路检索无 rerank 步骤

**验证：** `go build ./internal/rag/...` 编译通过；`go test ./internal/rag/...` 全绿

## T6: 测试更新（fakeRetriever + 断言）

**文件：** `internal/rag/engine_test.go`、`internal/retriever/retriever_test.go`
**依赖：** T1-T5
**步骤：**
1. `fakeRetriever` 增加 `Rerank` 方法实现（返回原结果，测试可配置重排行为）
2. retriever_test.go 新增：
   - `TestSearch_SkipRerank`：`SkipRerank: true` 时 Search 不触发 rerank（mockReranker 调用计数为 0）
   - `TestSearchMulti_OverallRerank`：多路融合后 rerank 恰好 1 次；Trace 含 RerankBefore（融合结果）/RerankAfter
3. engine_test.go 新增：
   - `TestAsk_MultiQuerySingleRerank`：`query: multi` + thinking 断言 thinking 含单个 `StepRerank`（After 为整体重排结果）、逐路检索无 rerank 步骤
   - 现有多路/分解测试回归全绿（fakeRetriever.Rerank 返回原结果 → 行为等价）

**验证：** `go test ./internal/rag/... ./internal/retriever/...` 全绿

## 执行顺序

```
T1 → T2 → T3 → T5 → T4 → T6
```

依赖链：T2 依赖 T1；T3/T4 依赖 T2；T5 依赖 T3/T4。T2 实现 `Rerank` 接口时就按最终签名（含 trace 参数）写，T3/T4 直接按最终签名实现，避免返工。
