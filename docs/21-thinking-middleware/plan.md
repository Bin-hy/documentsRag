# 思考链路中间件 Plan

## 架构概览

思考链路中间件 = **一个可插拔的采集器（TraceSink），贯穿 RAG 引擎各环节埋点，两个出口**：

```
                    ┌─────────────────────────────────────────────┐
                    │                 RAGEngine                     │
 路由判定 ──┐        │   routeQuery ─ multiQuery/rewrite ─ retriever │
 改写 ──────┼──埋点──▶   (Search/SearchMulti 带 Trace 回调)          │
 检索 ──────┘        │        └─ buildContext ─ llm.Generate        │
                    └───────────────┬─────────────────────────────┘
                                    │ TraceSink.Record(ThinkingStep)
                        ┌───────────┴───────────┐
                        ▼                       ▼
              StreamAsk：每步完成              Ask：收集到 slice
              立即发 thinking SSE 事件          附到 RAGResult.Thinking
                        │                       │
                        ▼                       ▼
              前端思考面板（分步展示）       JSON 响应（一次性完整链路）
```

要点：

1. **埋点粒度对齐"环节"而非子步骤**。每个环节（路由判定、改写、多查询、单路检索、多路融合、rerank、chunks、子问题、回退、HyDE）在**同步点**完成后 Record 一次——这天然解决并发乱序（N7）：`SearchMulti`/并行分解的并发检索都在 `wg.Wait()` 之后按路顺序回调，每路内部不拆分并发子步骤。
2. **开关即 nil 判断**。`TraceSink` 为 nil 时所有 `Record` 直接跳过（N2 零开销）。开关来自 `StrategyConfig.Thinking`（复用三级覆盖体系，F9）。
3. **retriever 不依赖 rag**。`retriever` 包定义自己的 `RetrieveTraceStep` 结构，通过 `RetrieveRequest.Trace` 回调上抛；`rag` 层把它翻译成 `ThinkingStep` 再 Record——无循环依赖。
4. **`prepare` 增加 sink 参数**。流式传入"转发到 SSE 通道"的 sink，非流式传入"追加到 slice"的 sink，同一套埋点、两个出口（G4/F8）。

## 核心数据结构

### `internal/rag/thinking.go`（新增）

```go
// ThinkingStepType 思考环节类型
type ThinkingStepType string

const (
    StepRouting       ThinkingStepType = "routing"        // 路由判定
    StepRewrite       ThinkingStepType = "query_rewrite"  // 单查询改写
    StepMultiQuery    ThinkingStepType = "multi_query"    // 多查询变体生成
    StepRetrieval     ThinkingStepType = "retrieval"      // 检索（单路或多路）
    StepRerank        ThinkingStepType = "rerank"         // 重排序前后对比
    StepChunks        ThinkingStepType = "chunks"         // 最终目标 chunks
    StepDecompose     ThinkingStepType = "decompose"      // 分解判定 + 子问题列表
    StepStepBack      ThinkingStepType = "step_back"      // 回退问题
    StepHyDE          ThinkingStepType = "hyde"           // 假设文档
)

// ThinkingStep 思考链路环节（JSON 直接序列化给前端）
type ThinkingStep struct {
    Type      ThinkingStepType `json:"type"`
    Label     string           `json:"label"`               // 前端展示标题
    ElapsedMS int64            `json:"elapsed_ms,omitempty"` // 环节耗时
    Data      any              `json:"data,omitempty"`       // 环节结构化数据
}

// TraceSink 思考链路采集器；nil 表示关闭（所有 Record 直接跳过，N2）
type TraceSink interface {
    Record(step ThinkingStep)
}
```

### 环节数据结构（各环节的 `Data` 载荷）

```go
// 路由判定
type RoutingData struct {
    Complexity string `json:"complexity"`           // simple / medium / complex
    Strategy   string `json:"strategy"`             // direct / multi_query / decomposition
    Reasoning  string `json:"reasoning,omitempty"`
}

// 单查询改写（含失败降级）
type RewriteData struct {
    Original  string `json:"original"`
    Rewritten string `json:"rewritten"`
    Fallback  bool   `json:"fallback,omitempty"`    // true=改写失败，使用原问题
}

// 多查询变体（含原问题）
type MultiQueryData struct {
    Variants []string `json:"variants"`
}

// 检索：单路或按查询分组的检索结果
type RetrievalData struct {
    Query    string        `json:"query"`             // 主查询（多路时为汇总展示用）
    PerQuery []PerQueryRet `json:"per_query,omitempty"` // 多路时逐路；单路时省略
    Method   string        `json:"method,omitempty"`  // 单路时：vector / bm25 / hybrid
    Recalled int           `json:"recalled"`          // 召回数（单路）或融合结果数（多路）
}
type PerQueryRet struct {
    Query    string `json:"query"`
    Method   string `json:"method"`
    Recalled int    `json:"recalled"`
}

// rerank 前后对比
type RerankData struct {
    Query  string       `json:"query"`
    Before []RankedItem `json:"before"`
    After  []RankedItem `json:"after"`
}
type RankedItem struct {
    ID       string  `json:"id"`
    Filename string  `json:"filename"`
    Score    float32 `json:"score"`
    Rank     int     `json:"rank"`
}

// 最终目标 chunks（与 sources 同集合，含内容预览）
type ChunksData struct {
    Chunks []ChunkInfo `json:"chunks"`
}
type ChunkInfo struct {
    ID       string  `json:"id"`
    Filename string  `json:"filename"`
    Heading  string  `json:"heading"`
    Score    float32 `json:"score"`
    Content  string  `json:"content"`  // 完整内容（前端 CSS 截断 + 展开，N3 由 maxChunks 控量）
}

// 分解判定 + 子问题
type DecomposeData struct {
    ShouldDecompose bool     `json:"should_decompose"`
    SubQuestions    []string `json:"sub_questions,omitempty"`
}

// 回退问题
type StepBackData struct {
    StepBackQuery string `json:"step_back_query"`
}

// HyDE 假设文档
type HyDEData struct {
    HypoDoc string `json:"hypo_doc"` // 服务端截断（如 500 字符）
}
```

关键点：`ThinkingStep.Data` 用 `any` 承载上述载荷，`json.Marshal` 直接序列化；`RankedItem` 分数与 `Source.Score` 语义一致。

## 接口与事件协议改造

### `internal/rag/engine.go` 改造

```go
// RAGResult 增加 Thinking（非流式一次性完整链路，F8）
type RAGResult struct {
    Answer   string         `json:"answer"`
    Sources  []Source       `json:"sources"`
    Thinking []ThinkingStep `json:"thinking,omitempty"` // 关闭时省略
}

// StreamEvent 增加 Thinking（流式分步，F1）
type StreamEvent struct {
    Type      EventType
    Content   string
    Sources   []Source
    Thinking  *ThinkingStep  // EventThinking 时有效
    Err       error
}

// 新增事件类型（放在 EventSources 之前）
const (
    EventThinking EventType = iota
    EventSources
    EventChunk
    EventDone
    EventError
)
```

### `AskOptions` 增加思考开关与采集器

```go
type AskOptions struct {
    KBID        string
    KBStrategy  *config.StrategyConfig
    ReqStrategy *config.StrategyConfig
    CfgSnapshot *config.Config
    Thinking    bool            // 本次请求是否启用思考链路（F9）
    Sink        TraceSink       // 非流式采集器（nil 时 Ask 自己建 slice sink）
}

func WithThinking(on bool) AskOption      // 由 handler 根据三级策略合并结果设置
```

### `retriever` 包埋点（无循环依赖）

```go
// retriever 包内定义，不 import rag
type RetrieveTrace struct {
    Query    string
    Method   string          // vector / bm25 / hybrid / multi_fusion / hyde_vector / hyde_orig
    Recalled int
    PerQuery []PerQueryTrace // 多路时逐路
    RerankBefore []RankedItem
    RerankAfter  []RankedItem
}

// RetrieveRequest 增加可选回调；nil 表示不采集（N2）
type RetrieveRequest struct {
    Query  string
    TopK   int
    Filter map[string]any
    Trace  func(t RetrieveTrace)  // nil=关闭
}
```

- `Search`：向量/BM25 并行执行完 → `Trace{Method: vector/bm25/hybrid, Recalled: 融合前}`；rerank 后 → `Trace{RerankBefore, RerankAfter}`。
- `SearchMulti`：每路内部 `Search` 已各自回调；融合后 → `Trace{Method: multi_fusion, PerQuery, Recalled: 融合结果数}`（N7：wg.Wait 后按路顺序回调）。
- `SearchByVector`（HyDE 路）：`Trace{Method: hyde_vector/…}`。

### 配置：`StrategyConfig` 增加字段（三级覆盖，F9）

```go
type StrategyConfig struct {
    // ...现有字段
    Thinking string `yaml:"thinking" json:"thinking,omitempty"` // off / on，空=继承
}
```

- `ResolveStrategy` 合并 `eff.Thinking`（默认 `off`，保持现状）；`ValidateStrategy` 校验 `off/on`。
- 前端 `StrategyConfig` 类型同步加 `thinking?: 'off' | 'on'`。

### SSE 协议（handler_chat.go）

- 事件序列变为：`thinking×N → sources → chunk×N → done`。
- `ChatStream` 中 `case EventThinking: c.SSEvent("thinking", ev.Thinking)`；`Chat`（非流式）直接返回 `RAGResult`（已含 Thinking）。
- 流式 sink 实现：`sink := func(step ThinkingStep) { sendEvent(ctx, out, StreamEvent{Type: EventThinking, Thinking: &step}) }`——每步完成立即发出（G2）。

## 模块设计与埋点位置

### sink 注入与开关判定（engine 内部）

```go
// engine 内部：开关判定复用 effective() 三级合并（F9）
func (e *RAGEngine) sinkFor(o *AskOptions, streamSink TraceSink) TraceSink {
    if o.Thinking && streamSink != nil {
        return streamSink          // 流式：handler 注入的转发 sink
    }
    if o.Thinking {
        return &sliceSink{}        // 非流式：engine 自建，结束时附到 RAGResult
    }
    return nil                     // 关闭：零开销（N2）
}

// 统一埋点辅助
func recordStep(sink TraceSink, step ThinkingStep) {
    if sink != nil { sink.Record(step) }
}
```

- **开关判定在 engine**（`eff.Thinking == "on"`），handler 无需复制策略合并逻辑。
- **sink 注入在 handler**（流式转发闭包持有 out 通道），非流式由 engine 自建 sliceSink。

### 埋点位置清单（每个环节一个同步点）

| 环节 | 位置 | Record 内容 |
|------|------|-------------|
| 路由判定 | `Ask`/`StreamAsk` 的 `eff.Routing=="auto"` 分支，`routeQuery` 成功后 | `StepRouting`：复杂度/策略/reasoning；失败不 Record（F2） |
| 单查询改写 | `prepare` 内 `rewriteQuery` 调用后 | `StepRewrite`：original/rewritten；失败降级 → `fallback:true`（F3） |
| 多查询变体 | `prepare`/`tryMultiQuery` 内 `multiQuery` 成功后 | `StepMultiQuery`：variants（F3） |
| 单路检索 | retriever `Search` 内部（向量/BM25 汇合后） | `StepRetrieval`：Method（vector/bm25/hybrid）+ Recalled |
| 多路检索 | retriever `SearchMulti` 内部（`wg.Wait` 后） | `StepRetrieval`：PerQuery 逐路 + 融合 Recalled（N7 顺序稳定） |
| rerank 对比 | retriever `Search` 内 rerank 成功后 | `StepRerank`：Before/After（EnableReranker 且成功时才有，F5） |
| 目标 chunks | 各路径 `buildContext` 后 | `StepChunks`：与 sources 同集合 + 内容预览（F6/AC7） |
| 分解判定+子问题 | `tryDecompose` 判定成功后 | `StepDecompose`：should_decompose + sub_questions（F7） |
| 子问题检索 | `tryDecompose` 每子问题 `searchSubQuery` 后 | `StepRetrieval`：query=子问题（逐子问题一条，F7） |
| 回退问题 | `tryStepBack` 判定成功后 | `StepStepBack`：step_back_query（F7） |
| HyDE 假设文档 | `hydeSearch` 生成后 | `StepHyDE`：hypo_doc 服务端截断 500 字符（F7/N3） |
| HyDE 双路 | `hydeSearch` 向量路+原查询路 | `StepRetrieval`：Method=hyde_vector / hyde_orig（F7） |

### 各路径构造 RAGResult 时统一附加 Thinking

```go
// 所有 return &RAGResult{...} 处经此辅助
func withThinking(o *AskOptions, res *RAGResult) *RAGResult {
    if ss, ok := o.Sink.(*sliceSink); ok && len(ss.steps) > 0 {
        res.Thinking = ss.steps
    }
    return res
}
```

覆盖：`Ask` 常规路径、`tryDecompose`、`tryStepBack`、`tryMultiQuery`（F8/AC2）。

### handler 层（handler_chat.go）

```go
// ChatStream：注入流式转发 sink，每步完成立即发 thinking 事件（G2）
sink := TraceSinkFunc(func(step ThinkingStep) {
    sendEvent(ctx, out, StreamEvent{Type: EventThinking, Thinking: &step})
})
events, err := h.engine().StreamAsk(ctx, ..., rag.WithThinking(true), rag.WithSink(sink))
// 事件分发：case EventThinking → c.SSEvent("thinking", ev.Thinking)

// Chat（非流式）：不传 sink，engine 自建 sliceSink 附到 RAGResult.Thinking
result, err := eng.Ask(ctx, ..., rag.WithThinking(true))
```

## 模块交互

```
handler ChatStream
  └─ engine.StreamAsk(WithThinking(true), WithSink(转发sink))
       ├─ effective() → eff.Thinking 判定开关
       ├─ [Routing] routeQuery → sink.Record(StepRouting)
       ├─ [Decompose/StepBack/HyDE 路径] 各自环节 → sink.Record(...)
       └─ prepare(sink)
            ├─ [Rewrite/MultiQuery] → sink.Record(改写/变体)
            ├─ retriever.Search / SearchMulti(带 Trace 回调)
            │    ├─ 向量+BM25 汇合 → Trace → 翻译 Record(StepRetrieval)
            │    └─ rerank 成功 → Trace → Record(StepRerank)
            └─ buildContext → Record(StepChunks)
       └─ llm.StreamGenerate → chunk×N
       事件序：thinking×N → sources → chunk×N → done

handler Chat（非流式）
  └─ engine.Ask(WithThinking(true))  → engine 自建 sliceSink
       └─ 各环节 Record → withThinking() 附到 RAGResult.Thinking → JSON 返回
```

数据流要点：
- retriever 回调 → rag 层**翻译**为 ThinkingStep（`retriever.RetrieveTrace` → `StepRetrieval/StepRerank` 的 Data），解耦两个包。
- 事件顺序 = Record 调用顺序 = 真实执行顺序（同步点埋点保证，N7）。

## 文件组织

```
project/
├── docs/21-thinking-middleware/
│   ├── spec.md                  — 已批准
│   └── plan.md                  — 本文档
├── internal/rag/
│   ├── thinking.go              — 新建：ThinkingStep/TraceSink/各环节 Data/sliceSink/recordStep
│   ├── engine.go                — 修改：RAGResult+Thinking、StreamEvent+Thinking、EventThinking、
│   │                               AskOptions+Thinking/Sink、sinkFor/withThinking、各路径埋点
│   ├── decompose.go             — 修改：tryDecompose/tryStepBack 埋点
│   └── routing.go               — 修改：tryMultiQuery/hydeSearch 埋点
├── internal/retriever/
│   ├── retriever.go             — 修改：Search/SearchMulti/SearchByVector 内 Trace 回调
│   ├── types.go                 — 修改：RetrieveRequest+Trace、RetrieveTrace/PerQueryTrace/RankedItem
│   └── rrf.go                   — 修改：FuseRRF/FuseMultiQuery 调用点透传 Trace（或保持纯函数）
├── internal/config/
│   └── config.go                — 修改：StrategyConfig+Thinking 字段
├── internal/rag/strategy.go     — 修改：ResolveStrategy 合并 Thinking、ValidateStrategy 校验
├── internal/api/
│   └── handler_chat.go          — 修改：ChatStream thinking 事件分发 + WithThinking/WithSink
├── internal/api/response.go     — 修改：OK 序列化透传（无需改，RAGResult 自带 json tag）
├── frontend/src/api/
│   ├── chat.ts                  — 修改：解析 thinking 事件
│   └── types.ts                 — 修改：SSEEvent 增加 thinking、StrategyConfig+thinking、ThinkingStep 类型
├── frontend/src/stores/chat.ts  — 修改：LocalMessage+thinking、事件累积
├── frontend/src/components/
│   └── ThinkingPanel.vue        — 新建：可折叠思考面板（分步渲染各环节）
├── frontend/src/components/ChatMessage.vue — 修改：接入 ThinkingPanel
└── 测试
    ├── internal/rag/thinking_test.go   — 新建
    ├── internal/rag/engine_test.go     — 修改（含新埋点断言）
    ├── internal/retriever/retriever_test.go — 修改（Trace 回调断言）
    └── frontend/src/api/chat.test.ts   — 修改（thinking 事件解析）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 思考链路开关接入点 | 复用 `StrategyConfig` 三级覆盖（`thinking: off/on`） | 与现有 query/fusion/routing 同一套体系，全局/知识库/请求都可控；无需另建配置通道 |
| 采集器形态 | 接口 `TraceSink` + nil 表示关闭 | 开关关闭时仅一个 nil 判断（N2 零开销）；接口可替换（将来演进企业级 trace 后端） |
| 埋点粒度 | 环节级同步点，而非子步骤级 | 每环节完成后 Record 一次；并行子步骤在 wg.Wait 后按路顺序回调，天然满足 N7 顺序稳定 |
| retriever 与 rag 解耦 | retriever 定义自己的 `RetrieveTrace`，经 `RetrieveRequest.Trace` 回调上抛，rag 层翻译为 ThinkingStep | 避免 retriever import rag 造成循环依赖 |
| 流式 vs 非流式双出口 | 同一套埋点，handler 流式注入转发 sink、engine 非流式自建 sliceSink | 一次实现两处可用，AC2 保证内容一致 |
| 事件协议 | SSE 新增 `thinking` 事件，位于 sources 之前 | 与 OpenAI `ResponseStreamEvent` 的增量事件模式一致；向后兼容（既有事件语义不变，N6） |
| thinking 持久化 | 不持久化（N4） | 与企业级一致（DeepSeek reasoning 不进上下文）；历史表结构不动 |
| chunk 内容展示 | 完整 content 交给前端，CSS 截断 + 展开 | 数量已被 MaxChunks 限制（默认 5 条），总量可控，无需服务端二次截断逻辑 |
| HyDE 假设文档 | 服务端截断 500 字符 | 假设文档可能很长，且非核心证据，截断足够展示（N3） |
| FuseRRF/FuseMultiQuery | 保持纯函数，Trace 由调用方（Search/SearchMulti）负责 | 纯函数不掺副作用，可测性保持 |
