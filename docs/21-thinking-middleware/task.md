# 思考链路中间件 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/rag/thinking.go` | ThinkingStep/TraceSink/各环节 Data/sliceSink/recordStep |
| 修改 | `internal/config/config.go` | StrategyConfig + Thinking 字段 |
| 修改 | `internal/rag/strategy.go` | ResolveStrategy 合并 Thinking、ValidateStrategy 校验 |
| 修改 | `internal/retriever/types.go` | RetrieveRequest+Trace、RetrieveTrace/PerQueryTrace/RankedItem |
| 修改 | `internal/retriever/retriever.go` | Search/SearchMulti/SearchByVector 内 Trace 回调 |
| 修改 | `internal/rag/engine.go` | RAGResult/StreamEvent/AskOptions 扩展、sinkFor/withThinking、prepare 埋点 |
| 修改 | `internal/rag/decompose.go` | tryDecompose/tryStepBack 埋点 |
| 修改 | `internal/rag/routing.go` | tryMultiQuery/hydeSearch 埋点 |
| 修改 | `internal/api/handler_chat.go` | ChatStream thinking 事件 + WithThinking/WithSink |
| 新建 | `internal/rag/thinking_test.go` | 采集器/翻译单元测试 |
| 修改 | `internal/rag/engine_test.go` | 埋点断言 |
| 修改 | `internal/retriever/retriever_test.go` | Trace 回调断言 |
| 修改 | `frontend/src/api/types.ts` | thinking 事件/类型 |
| 修改 | `frontend/src/api/chat.ts` | 解析 thinking 事件 |
| 修改 | `frontend/src/stores/chat.ts` | LocalMessage+thinking |
| 新建 | `frontend/src/components/ThinkingPanel.vue` | 可折叠思考面板 |
| 修改 | `frontend/src/components/ChatMessage.vue` | 接入 ThinkingPanel |
| 修改 | `frontend/src/api/chat.test.ts` | thinking 事件解析测试 |

## T1: config 增加 thinking 开关字段

**文件：** `internal/config/config.go`、`internal/rag/strategy.go`
**依赖：** 无
**步骤：**
1. `StrategyConfig` 增加字段 `Thinking string \`yaml:"thinking" json:"thinking,omitempty"\``（注释注明 `off / on`，空=继承）
2. `EffectiveStrategy` 增加 `Thinking string` 字段
3. `ResolveStrategy` 的 `pick` 链合并 `eff.Thinking`（默认 `"off"`）
4. `ValidateStrategy` 校验 `off/on`

**验证：** `go build ./internal/config/... ./internal/rag/...` 编译通过；`go test ./internal/rag/ -run Strategy` 通过（现有策略测试仍绿）

## T2: retriever 增加 Trace 回调与数据结构

**文件：** `internal/retriever/types.go`
**依赖：** 无
**步骤：**
1. `RetrieveRequest` 增加 `Trace func(t RetrieveTrace)` 字段（注释：nil=关闭）
2. 新增 `RetrieveTrace`（Query/Method/Recalled/PerQuery/RerankBefore/RerankAfter）
3. 新增 `PerQueryTrace`（Query/Method/Recalled）与 `RankedItem`（ID/Filename/Score/Rank）

**验证：** `go build ./internal/retriever/...` 编译通过

## T3: rag 新建 thinking.go 采集层

**文件：** `internal/rag/thinking.go`（新建）
**依赖：** 无
**步骤：**
1. 定义 `ThinkingStepType` 常量：routing/query_rewrite/multi_query/retrieval/rerank/chunks/decompose/step_back/hyde
2. 定义 `ThinkingStep{Type, Label, ElapsedMS, Data}`（json tag 与 plan 一致）
3. 定义 `TraceSink` 接口 + `TraceSinkFunc` 适配器（func 转接口）
4. 定义各环节 Data 结构：RoutingData/RewriteData/MultiQueryData/RetrievalData/PerQueryRet/RerankData/RankedItem/ChunksData/ChunkInfo/DecomposeData/StepBackData/HyDEData
5. 实现 `sliceSink`（`steps []ThinkingStep` + `Record` 追加）
6. 实现 `recordStep(sink, step)`（nil 直接返回，N2）

**验证：** `go build ./internal/rag/...` 编译通过

## T4: retriever 埋点回调

**文件：** `internal/retriever/retriever.go`
**依赖：** T2
**步骤：**
1. `Search`：向量/BM25 汇合后、切片 topK 前，调用 `req.Trace`（非 nil 时）上报 `RetrieveTrace{Query, Method: vector/bm25/hybrid, Recalled: len(fusedResults)}`（BM25 未启用或空 → vector；否则 hybrid）
2. `Search`：rerank 成功后上报第二次 Trace（`RerankBefore`=融合结果 Top-N 的 RankedItem、`RerankAfter`=rerank 结果），`EnableReranker` 关闭或失败时不报（F5）
3. `SearchMulti`：`wg.Wait()` 后、FuseMultiQuery 前，按路顺序填 `PerQuery`；融合后上报 `RetrieveTrace{Query: req.Query, Method: multi_fusion, PerQuery, Recalled: len(fused)}`（N7）
4. `SearchByVector`：上报 `RetrieveTrace{Method: hyde_vector/…, Recalled}`

**验证：** `go build ./internal/retriever/...` 编译通过；`go test ./internal/retriever/...` 现有测试全绿

## T5: engine.go 数据结构与开关扩展

**文件：** `internal/rag/engine.go`
**依赖：** T1、T3
**步骤：**
1. `RAGResult` 增加 `Thinking []ThinkingStep \`json:"thinking,omitempty"\``（F8）
2. `StreamEvent` 增加 `Thinking *ThinkingStep` 字段；事件常量前插 `EventThinking`（保持 iota 顺序：thinking→sources→chunk→done→error）
3. `AskOptions` 增加 `Thinking bool` 与 `Sink TraceSink`
4. 新增 `WithThinking(on bool)` 与 `WithSink(sink TraceSink)` 两个 AskOption
5. 实现 `sinkFor(o *AskOptions, streamSink TraceSink) TraceSink`（按 plan：Thinking=false 返回 nil；streamSink 非 nil 用之；否则建 sliceSink）——注意与 `effective()` 配合：开关最终值 = `eff.Thinking=="on" && o.Thinking`
6. 实现 `withThinking(o *AskOptions, res *RAGResult) *RAGResult`（sliceSink 有数据时附到 res.Thinking）

**验证：** `go build ./internal/rag/...` 编译通过

## T6: prepare 埋点（改写/多查询/检索/rerank/chunks）

**文件：** `internal/rag/engine.go`（prepare 与 Ask/StreamAsk）
**依赖：** T4、T5
**步骤：**
1. `Ask`/`StreamAsk`：`eff := e.effective(o)` 后计算 `sink := e.sinkFor(&o, 流式sink)`，把 sink 传入 `prepare`（新增参数）与各策略入口
2. `prepare` 内：`rewriteQuery` 成功后 `recordStep(sink, StepRewrite{original, rewritten})`；失败降级走原问题时 `recordStep(StepRewrite{fallback:true})`（F3）
3. `multiQuery` 成功后 `recordStep(StepMultiQuery{variants})`（F3）
4. 单路检索 `Search` 后：把 `RetrieveRequest.Trace` 回调收到的 `retriever.RetrieveTrace` 翻译为 `StepRetrieval`（F4）；`Trace.RerankAfter` 非空时翻译为 `StepRerank`（F5）
5. 多路检索 `SearchMulti` 同理（PerQuery → RetrievalData.PerQuery，F4/N7）
6. `buildContext` 后 `recordStep(StepChunks)`（从 items/sources 构造 ChunksData，F6）
7. `Ask` 各 `return &RAGResult{...}` 处改为 `return withThinking(&o, &RAGResult{...})`（F8）

**验证：** `go build ./internal/rag/...` 编译通过

## T7: decompose.go 埋点

**文件：** `internal/rag/decompose.go`
**依赖：** T5、T6
**步骤：**
1. `tryDecompose` 增加 sink 参数；`judgeDecompose` 成功且 should=true 时 `recordStep(StepDecompose{should, sub_questions})`（F7）
2. 每子问题 `searchSubQuery` 成功后 `recordStep(StepRetrieval{Query: 子问题, Recalled})`（逐子问题一条，F7）
3. `tryStepBack`：`judgeStepBack` 成功时 `recordStep(StepStepBack{step_back_query})`；回退/原问题两次检索各 `recordStep(StepRetrieval)`（F7）
4. 两个函数的返回处接 `withThinking`

**验证：** `go build ./internal/rag/...` 编译通过

## T8: routing.go 埋点（tryMultiQuery + hydeSearch）

**文件：** `internal/rag/routing.go`
**依赖：** T5、T6
**步骤：**
1. `tryMultiQuery`：`multiQuery` 成功后 `recordStep(StepMultiQuery{variants})`；`SearchMulti` 后翻译 Trace → `StepRetrieval`（F3/F4）
2. `hydeSearch`：假设文档生成后 `recordStep(StepHyDE{hypo_doc 截断 500})`（F7/N3）；向量路 `SearchByVector` 与原查询路 `Search` 各 `recordStep(StepRetrieval{Method: hyde_vector/hyde_orig})`（F7）
3. 返回处接 `withThinking`

**验证：** `go build ./internal/rag/...` 编译通过

## T9: handler_chat.go 事件分发与注入

**文件：** `internal/api/handler_chat.go`
**依赖：** T5
**步骤：**
1. `Chat`（非流式）：`Ask` 调用增加 `rag.WithThinking(true)`（开关最终由 engine 内 effective 判定）
2. `ChatStream`：构造转发 sink `TraceSinkFunc(func(step) { sendEvent(ctx, out, StreamEvent{Type: EventThinking, Thinking: &step}) })`，传给 `StreamAsk` 的 `rag.WithThinking(true), rag.WithSink(sink)`
3. 事件循环加 `case rag.EventThinking: c.SSEvent("thinking", ev.Thinking)`
4. 更新 Chat 的 `@Description` 注解：事件序列改为 `thinking×N → sources → chunk×N → done`

**验证：** `go build ./internal/api/...` 编译通过；`go test ./internal/api/...` 现有测试全绿

## T10: 后端测试

**文件：** `internal/rag/thinking_test.go`（新建）、`internal/rag/engine_test.go`、`internal/retriever/retriever_test.go`
**依赖：** T6、T7、T8、T9
**步骤：**
1. `thinking_test.go`：sliceSink 追加顺序；recordStep(nil) 不 panic；retriever.RetrieveTrace → ThinkingStep 翻译正确（含 PerQuery、Rerank 前后）
2. `engine_test.go`：开启 thinking 时 `Ask` 返回的 `Thinking` 非空且事件顺序符合执行顺序（先路由/改写 → 检索 → rerank → chunks）；关闭时 `Thinking` 为空（F9/AC9）；流式事件流中 thinking 事件全在 sources 前（AC1）
3. `retriever_test.go`：`Search` 带 Trace 回调时收到 vector/hybrid 与 rerank 回调；`SearchMulti` 收到 multi_fusion 回调且 PerQuery 顺序与传入 queries 一致（N7/AC11）

**验证：** `go test ./internal/rag/... ./internal/retriever/...` 全部通过

## T11: 前端类型与事件解析

**文件：** `frontend/src/api/types.ts`、`frontend/src/api/chat.ts`
**依赖：** 无（纯前端）
**步骤：**
1. `types.ts`：`StrategyConfig` 增加 `thinking?: 'off' | 'on'`（与后端对齐）
2. `types.ts`：新增 `ThinkingStep` 接口（type/label/elapsed_ms/data，data 为 `any`）与 `ThinkingData` 各环节载荷类型（RoutingData/RewriteData/MultiQueryData/RetrievalData/RerankData/ChunksData/DecomposeData/StepBackData/HyDEData）
3. `types.ts`：`SSEEvent` 增加 `{ type: 'thinking'; step: ThinkingStep }`
4. `chat.ts`：`toEvent` 增加 `case 'thinking': return { type: 'thinking', step: data as ThinkingStep }`

**验证：** `cd frontend && npx tsc --noEmit` 通过

## T12: 前端状态累积 thinking

**文件：** `frontend/src/stores/chat.ts`
**依赖：** T11
**步骤：**
1. `LocalMessage` 增加 `thinking?: ThinkingStep[]`
2. `send` 事件处理加 `case 'thinking': this.messages[assistantIndex].thinking.push(ev.step)`（未定义时初始化为 `[]`）
3. `switchSession` 历史加载不涉及 thinking（N4 不持久化，无需处理）

**验证：** `cd frontend && npx tsc --noEmit` 通过；`npx vitest run src/stores/chat.test.ts` 通过

## T13: ThinkingPanel 组件与接入

**文件：** `frontend/src/components/ThinkingPanel.vue`（新建）、`frontend/src/components/ChatMessage.vue`
**依赖：** T12
**步骤：**
1. `ThinkingPanel.vue`：props 接收 `steps: ThinkingStep[]`；可折叠（默认收起 + 打开时逐环节渲染）；按 `type` 渲染各环节（路由/改写/多查询/检索/rerank/chunks/分解/回退/HyDE 各自标题与数据展示）；chunk 内容 CSS `-webkit-line-clamp` 截断 + 「展开」按钮（N3）；`elapsed_ms` 显示耗时
2. `ChatMessage.vue`：assistant 消息、内容区上方插入 `<ThinkingPanel v-if="message.thinking?.length" :steps="message.thinking" />`
3. 样式沿用现有 CSS 变量（`--br-bg-card`、`--br-border` 等）

**验证：** `cd frontend && npm run build` 成功（含类型检查）；`npx vitest run` 全绿

## 执行顺序

```
T1 → T2 → T3 → T4 ─┬→ T5 → T6 → T7 → T8 → T9 → T10
                    └→ T11 ─→ T12 ─→ T13（可并行于后端）
```

后端链 T1-T10 严格串行（依赖链：T5 依赖 T1/T3，T6 依赖 T4/T5，T7/T8 依赖 T6）；前端链 T11-T13 与后端互不依赖，可与 T5 起并行。
