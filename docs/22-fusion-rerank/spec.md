# 融合后整体重排 Spec

## 背景

RAG 多路检索路径存在"重排效果被融合冲掉"的问题：

- **现状**：`SearchMulti`（多查询）、`tryDecompose`（子问题分解）、`hydeSearch`（HyDE 双路）、`tryStepBack`（回退合并）都是**每路各 rerank 一次**，然后跨路 RRF 融合——融合后的最终 Top-K 顺序按 **RRF 融合分数**排序，与每路 rerank 的相关性分数**脱节**。
- **代价**：多路场景每路调用一次 rerank API（N 次调用），但最终顺序并不由 rerank 分数决定——重排的"精排"效果被融合冲掉，成本与收益不匹配。
- 单路场景（`query: single`）没有此问题：检索 → RRF 融合 → rerank，顺序与 rerank 分数一致。

## 目标

- G1: 多路场景（multi-query / 子问题分解 / HyDE / 回退）融合或汇总后，对整个候选集**整体 rerank 一次**，最终 Top-K 顺序由 rerank 相关性分数决定。
- G2: 多路场景**路内不再 rerank**，rerank API 调用次数从 N 次降至 1 次。
- G3: 单路场景行为**完全不变**（保留现有路内 rerank）。
- G4: rerank 关闭或失败时，多路场景返回融合/汇总结果（现有降级语义保持）。
- G5: 思考链路的 rerank 环节展示"融合前 → 整体 rerank 后"对比，与单路语义一致。

## 功能需求

- F1: **多路检索整体重排**。`SearchMulti`（多查询）每路检索不再触发 rerank（保留向量+BM25 路内融合），跨路 RRF 融合后对整个候选集整体 rerank 一次，最终 Top-K 顺序与 rerank 分数一致。
- F2: **HyDE 双路整体重排**。`hydeSearch` 的 HyDE 向量路 + 原查询路融合后，整体 rerank 一次。
- F3: **子问题分解汇总后整体重排**。`tryDecompose` 各子问题检索结果汇总后，整体 rerank 一次再进入上下文组装。
- F4: **回退查询合并后整体重排**。`tryStepBack` 的回退问题 + 原问题检索结果合并后，整体 rerank 一次。
- F5: **rerank 分数决定最终顺序**。整体重排后，返回的 `RetrieveResult` 顺序与 Score 均以 rerank 相关性分数为准（不再是 RRF 融合分数）。
- F6: **单路场景不变**。`query: single` 的直接检索（`Search`）保持现有行为：检索 → RRF 融合 → 路内 rerank，调用次数与顺序均不变。
- F7: **思考链路 rerank 展示**。多路场景的 rerank 环节展示 Before=融合结果、After=整体 rerank 结果；单路场景维持现状展示。
- F8: **降级语义**。整体 rerank 失败（API 错误/限流超限）时，多路场景返回融合/汇总结果并告警，不阻塞主链路（复用现有 rerank 失败降级逻辑）。

## 非功能需求

- N1: **成本下降**。多路场景 rerank API 调用次数从 N 次降至 1 次（每请求），不再为多路重复调用。
- N2: **语义一致性**。多路与单路的最终排序都统一为"rerank 相关性分数决定顺序"，消除"RRF 分数排序但标着 rerank"的脱节。
- N3: **兼容性**。`EnableReranker` / `TopN` / reranker 配置语义不变；`retriever.Retriever` 接口签名不变（内部实现调整）。
- N4: **并发安全**。多路并行检索（SearchMulti 的并发 goroutine）不引入新的数据竞争；整体 rerank 在主 goroutine 串行执行。
- N5: **思考链路顺序**。多路场景的 thinking 事件顺序为：路由/改写 → 逐路检索（无 rerank 步骤）→ 融合 → rerank（Before=融合后）→ 目标片段。
- N6: **性能**。整体 rerank 只对融合后的 Top-K 候选执行（候选集 ≤ 融合结果数），不额外扩大 rerank 输入规模。

## 不做的事

- **不做 rerank 结果缓存**：不缓存跨请求的 rerank 分数/结果，每次请求独立重排。
- **不改变单路场景行为**：`query: single` 的现有路内 rerank 不动（F6 已明确）。
- **不改 reranker 接口与实现**：`apiReranker`、限流、重试逻辑保持原样，只在调用时机上调整。
- **不做分数归一化**：不把 rerank 分数与 RRF 分数做加权混合或归一化——整体重排后直接以 rerank 分数排序，融合分数仅用于融合阶段的候选筛选。
- **不引入新的融合算法**：跨路融合仍用现有 `FuseMultiQuery`（RRF），只在其后追加整体 rerank。
- **不改配置结构**：不新增开关字段（是否整体重排由现有 `EnableReranker` 与多路路径自动决定）。

## 验收标准

- AC1（F1/F5）: 多查询请求（`query: multi`）最终 `sources` 的顺序与 rerank 分数一致（按 rerank 分数降序），不再按 RRF 融合分数排序。
- AC2（F1/N1）: 多查询请求的 rerank API 调用次数为 **1**（而非每路各 1 次）。
- AC3（F2）: HyDE 启用时，双路融合后整体 rerank 一次，最终顺序与 rerank 分数一致。
- AC4（F3/F4）: 子问题分解、回退查询路径，汇总/合并后整体 rerank 一次，进入上下文的顺序与 rerank 分数一致。
- AC5（F6）: `query: single` 请求的 rerank 调用次数与最终顺序和改动前完全一致（单路行为无回归）。
- AC6（F8）: 整体 rerank 失败（mock reranker 报错）时，多路场景返回融合结果而非报错，主链路继续。
- AC7（F7/N5）: 多路场景 thinking 的 rerank 环节 Before=融合结果、After=整体 rerank 结果；逐路检索环节不出现 rerank 步骤。
- AC8（N3/N4）: `go build ./...` 通过、`go test ./...` 全绿（含 reranker/retriever/thinking 相关测试），race 检测通过。
