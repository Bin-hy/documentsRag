# 融合后整体重排 Plan

## 架构概览

**核心思路**：把"路内 rerank"从每路检索中剥离，收敛为一个**统一的整体重排入口**，供所有多路路径在融合/汇总后调用一次。

```
单路（现状不变）：
  向量+BM25 → RRF → topK → 整体重排（路内 rerank）→ 返回

多路（本次改造）：
  每路：向量+BM25 → RRF（不 rerank）         ← 复用 searchFused
       ↘
  跨路融合（FuseMultiQuery / append 汇总）   ← 现有逻辑
       ↓
  整体重排一次（rerankIfEnabled / Rerank 接口）
       ↓
  Top-K（顺序 = rerank 分数）→ 上下文组装
```

## 核心数据结构与接口

```go
// retriever 包：RetrieveRequest 增加跳过重排标记（零值 false = 现状，兼容）
type RetrieveRequest struct {
    Query  string
    TopK   int
    Filter map[string]any
    Trace  func(t RetrieveTrace) // nil=关闭
    SkipRerank bool              // true=本检索不做 rerank（由调用方在融合/汇总后统一重排）
}

// retriever 包：Retriever 接口新增整体重排方法（rag 层融合/汇总后调用）
type Retriever interface {
    Search(ctx context.Context, req RetrieveRequest) ([]RetrieveResult, error)
    SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error)
    SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error)
    // Rerank 整体重排：EnableReranker 关闭或 reranker 失败时原样返回 results（不阻塞，F8）
    Rerank(ctx context.Context, query string, results []RetrieveResult, topN int) ([]RetrieveResult, error)
}
```

关键点：
- `SkipRerank` 是内部协作标记（rag 层给 hydeSearch/子问题检索用），默认 false 保持现有行为，不对外暴露语义（N3）。
- `Rerank` 接口方法让 rag 层拿到整体重排能力，reranker 仍封装在 retriever 内部。
- 单路 `Search` 路径完全复用现有逻辑，行为不变（F6/AC5）。

## 模块设计

### internal/retriever/retriever.go

```go
// searchFused 核心检索：向量 + BM25 → RRF 融合，不做 rerank、不截断（供 Search/SearchMulti 复用）
func (r *defaultRetriever) searchFused(ctx context.Context, query string, topK int, filter map[string]any) []RetrieveResult

// rerankIfEnabled 统一的整体重排入口（带降级 + Trace 前后对比，F8/F7）
func (r *defaultRetriever) rerankIfEnabled(ctx context.Context, query string, results []RetrieveResult, topN int, trace func(RetrieveTrace)) []RetrieveResult {
    if !r.config.EnableReranker || r.reranker == nil || len(results) == 0 {
        return results
    }
    candidates := ... // results → reranker.RerankCandidate
    reranked, err := r.reranker.Rerank(ctx, query, candidates, topN)
    if err != nil {
        slog.Warn("整体重排失败，降级返回融合结果", "err", err)
        return results // 降级（F8）
    }
    out := ... // reranker.RerankResult → []RetrieveResult
    if trace != nil {
        trace(RetrieveTrace{Query: query, RerankBefore: toRankedItems(results), RerankAfter: toRankedItems(out)})
    }
    return out
}

// Search 重构：searchFused → topK 截断 → rerankIfEnabled（行为不变，F6）
func (r *defaultRetriever) Search(ctx context.Context, req RetrieveRequest) ([]RetrieveResult, error) {
    fused := r.searchFused(ctx, req.Query, topK, req.Filter)
    if req.Trace != nil { req.Trace(RetrieveTrace{Query, Method, Recalled}) }  // 现有逻辑
    if len(fused) > topK { fused = fused[:topK] }
    if !req.SkipRerank {
        fused = r.rerankIfEnabled(ctx, req.Query, fused, topK, req.Trace)  // 单路路内 rerank（现状）
    }
    return fused, nil
}

// SearchMulti 改造：每路 searchFused（不 rerank）→ FuseMultiQuery → 整体 rerankIfEnabled
func (r *defaultRetriever) SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error) {
    // 每路：results[i] = r.searchFused(ctx, q, topK, req.Filter)   ← 原来调 r.Search，现在跳过路内 rerank
    fused := FuseMultiQuery(results, 60, topK)
    if req.Trace != nil { req.Trace(RetrieveTrace{Method: "multi_fusion", PerQuery, Recalled}) }  // 现有逻辑
    fused = r.rerankIfEnabled(ctx, req.Query, fused, topK, req.Trace)   // 整体重排（AC2：1 次调用）
    return fused, nil
}

// Rerank 接口方法：整体重排入口（rag 层用），内部即 rerankIfEnabled（带降级）
func (r *defaultRetriever) Rerank(ctx context.Context, query string, results []RetrieveResult, topN int) ([]RetrieveResult, error) {
    return r.rerankIfEnabled(ctx, query, results, topN, nil), nil
}
```

### internal/rag/routing.go — hydeSearch

```go
// 原查询路：跳过路内 rerank（融合后统一重排）
origResults, err := e.retriever.Search(ctx, retriever.RetrieveRequest{
    Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID),
    SkipRerank: true,   // ← 新增
})
// ...（HyDE 向量路 SearchByVector 本就不 rerank，不变）

// 融合后整体重排（F2）
fused := retriever.FuseMultiQuery([][]retriever.RetrieveResult{hydeResults, origResults}, 60, e.cfg.TopK)
fused, err = e.retriever.Rerank(ctx, query, fused, e.cfg.TopK)  // ← 新增
```

### internal/rag/decompose.go — tryDecompose / tryStepBack

```go
// searchSubQuery：增加 SkipRerank（不路内 rerank，由调用方统一重排）
// SearchMulti 路径（已整体 rerank）不变；Search 路径传 SkipRerank: true

// tryDecompose：汇总 allChunks 后整体重排（F3）
allChunks := append([]retriever.RetrieveResult{}, subChunks...)
allChunks, err = e.retriever.Rerank(ctx, question, allChunks, e.cfg.MaxChunks)  // ← 新增

// tryStepBack：合并 backChunks + origChunks 后整体重排（F4）
allChunks := append(backChunks, origChunks...)
allChunks, err = e.retriever.Rerank(ctx, question, allChunks, e.cfg.MaxChunks)  // ← 新增
```

## 思考链路行为（F7/N5）

| 场景 | thinking 事件序 | StepRerank 数据 |
|------|---------------|-----------------|
| 单路（single） | 改写 → 检索 → **rerank** → chunks | Before=RRF 融合结果、After=路内 rerank 结果（现状不变） |
| 多路（multi_query） | 改写 → 检索（逐路，**无 rerank 步骤**）→ 融合 → **rerank** → chunks | Before=跨路融合结果、After=整体 rerank 结果 |
| 分解 / 回退 | 子问题检索（**无 rerank**）→ 汇总 → **rerank** → chunks | Before=汇总结果、After=整体 rerank 结果 |
| HyDE | 假设文档 → 双路检索（**无 rerank**）→ 融合 → **rerank** → chunks | Before=融合结果、After=整体 rerank 结果 |

- `rerankIfEnabled` 的 Trace（RerankBefore/After）由 `traceSinkForRequest` 翻译为 `StepRerank`，与单路共用同一翻译逻辑，前端展示无需改动。
- 逐路检索环节（StepRetrieval）不再带 RerankAfter（路内没 rerank），前端自然不渲染 rerank 区块（N5）。

## 文件组织

```
project/
├── docs/22-fusion-rerank/
│   ├── spec.md                  — 已批准
│   └── plan.md                  — 本文档
├── internal/retriever/
│   ├── retriever.go             — 修改：searchFused/rerankIfEnabled 抽取、Search/SearchMulti 重构、Rerank 接口方法
│   ├── types.go                 — 修改：RetrieveRequest+SkipRerank
│   └── retriever_test.go        — 修改：SkipRerank/整体重排/Trace 断言
├── internal/rag/
│   ├── routing.go               — 修改：hydeSearch 原查询路 SkipRerank + 融合后 Rerank
│   ├── decompose.go             — 修改：searchSubQuery SkipRerank、tryDecompose/tryStepBack 汇总后 Rerank
│   ├── engine_test.go           — 修改：fakeRetriever+Rerank 方法、多路重排断言
│   └── thinking_test.go         — 修改（如需要）：多路 rerank 步骤断言
└── 测试全量：go test ./... + race
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 路内跳过 rerank 的标记 | `RetrieveRequest.SkipRerank bool`（零值 false=现状） | 内部协作标记，默认兼容；不加接口方法、不改配置 |
| 整体重排入口 | `Retriever` 接口新增 `Rerank(ctx, query, results, topN)` | rag 层需要跨包调用整体重排，reranker 仍封装在 retriever 内 |
| SearchMulti 每路实现 | 由 `r.Search` 改为 `r.searchFused`（内部） | 复用核心检索逻辑，零重复；路内天然跳过 rerank |
| 整体重排 topN | 单路用检索 TopK；多路用 `MaxChunks` | 多路场景目标就是决定"进上下文的顺序"，MaxChunks 即最终入上下文数量 |
| 已重排结果再次 Rerank | 允许（幂等语义，不跳过） | 简化代码：`Rerank` 对任意候选集重排，最终以最后一次为准；分解+多查询组合场景多一次调用，可接受（YAGNI 不做去重） |
| Rerank 失败 | 内部降级返回原结果 + error=nil | 调用方无需错误分支，主链路不阻塞（F8） |
| `Reranker.TopN` 死配置 | 本次不修 | 探索发现该配置未被使用（实际用检索 TopK），但与本次范围无关，留作后续 |
