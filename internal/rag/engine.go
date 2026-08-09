package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/datasource"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// noAnswerText 检索结果为空时的兜底回答
const noAnswerText = "未找到相关资料。"

// RAGResult 回答结果
type RAGResult struct {
	Answer   string         `json:"answer"`
	Sources  []Source       `json:"sources"`
	Thinking []ThinkingStep `json:"thinking,omitempty"` // 思考链路（F8，关闭时省略）
}

// EventType 流式事件类型
// 顺序即发送顺序：thinking×N → sources → chunk×N → done（或 error 终止）
type EventType int

const (
	EventThinking EventType = iota // 思考链路环节（每步完成立即发）
	EventSources                   // 引用来源，先发
	EventChunk                     // 文本增量
	EventDone                      // 正常结束
	EventError                     // 出错终止
)

// StreamEvent 流式 RAG 事件
type StreamEvent struct {
	Type     EventType
	Content  string        // EventChunk 时有效
	Sources  []Source      // EventSources 时有效
	Thinking *ThinkingStep // EventThinking 时有效
	Err      error         // EventError 时有效
}

// Engine RAG 编排接口
type Engine interface {
	Ask(ctx context.Context, sessionID string, question string, opts ...AskOption) (*RAGResult, error)
	StreamAsk(ctx context.Context, sessionID string, question string, opts ...AskOption) (<-chan StreamEvent, error)
}

// AskOptions 问答选项
type AskOptions struct {
	KBID        string                 // 知识库范围，空表示不限定
	KBStrategy  *config.StrategyConfig // 知识库级策略（nil = 用全局）
	ReqStrategy *config.StrategyConfig // 请求级策略（nil = 未覆盖）
	CfgSnapshot *config.Config         // 请求级配置快照（nil = 用引擎构建时配置）
	Thinking    bool                   // 本次请求是否请求思考链路（最终以 effective 三级合并为准，F9）
	Sink        TraceSink              // 思考链路采集器（流式由 handler 注入；非流式 engine 自建 sliceSink）
	ForceSingle bool                   // routing 判定 direct 时置位：本次查询强制 single（不对外配置）
	// 内部字段（engine 内部设置，不对外配置）：
	DataSource         string   // 路由判定后选中的数据源（vector_store / web_search），prepare 读取
	AllowedDataSources []string // 允许的数据源集合（三层合并后），路由约束用
}

// AskOption 函数式问答选项
type AskOption func(*AskOptions)

// WithKBID 限定知识库范围（检索时按 kb_id 过滤）
func WithKBID(kbID string) AskOption {
	return func(o *AskOptions) { o.KBID = kbID }
}

// WithStrategy 设置知识库级与请求级策略（三级覆盖：请求 > 知识库 > 全局）
func WithStrategy(kbStrategy, reqStrategy *config.StrategyConfig) AskOption {
	return func(o *AskOptions) {
		o.KBStrategy = kbStrategy
		o.ReqStrategy = reqStrategy
	}
}

// WithConfigSnapshot 设置请求级配置快照（配置热重载时保证单次请求一致性）
func WithConfigSnapshot(cfg *config.Config) AskOption {
	return func(o *AskOptions) { o.CfgSnapshot = cfg }
}

// WithThinking 请求本次问答启用思考链路（最终开关 = 三级合并策略的 thinking=on 且此值为 true）
func WithThinking(on bool) AskOption {
	return func(o *AskOptions) { o.Thinking = on }
}

// withForceSingle 仅供 engine 内部使用：routing 判定 direct 时强制本次查询 single（F-A1）
func withForceSingle() AskOption {
	return func(o *AskOptions) { o.ForceSingle = true }
}

// withForceSingleIf 按条件追加强制单查询选项（供 Ask/StreamAsk 调 prepare 时使用）
func withForceSingleIf(o *AskOptions) []AskOption {
	if o.ForceSingle {
		return []AskOption{withForceSingle()}
	}
	return nil
}

// withDataSource 设置本次检索数据源（内部：Ask/StreamAsk 路由判定后传给 prepare）
func withDataSource(name string) AskOption {
	return func(o *AskOptions) { o.DataSource = name }
}

// withAllowedDataSources 设置允许的数据源集合（内部：供 prepare 白名单兜底校验）
func withAllowedDataSources(names []string) AskOption {
	return func(o *AskOptions) { o.AllowedDataSources = names }
}

// WithSink 注入思考链路采集器（流式：转发到 SSE 通道；非流式：传 nil 由 engine 自建）
func WithSink(sink TraceSink) AskOption {
	return func(o *AskOptions) { o.Sink = sink }
}

// sinkFor 计算本次请求的思考链路采集器（N2：关闭时返回 nil，零开销）
// 最终开关 = 三级合并策略 thinking=on 且请求要求（o.Thinking）
func (e *RAGEngine) sinkFor(o *AskOptions, streamSink TraceSink) TraceSink {
	eff := e.effective(*o)
	if eff.Thinking != "on" || !o.Thinking {
		return nil
	}
	if streamSink != nil {
		return streamSink
	}
	return &sliceSink{}
}

// withThinking 非流式：把 sliceSink 收集的步骤附到 RAGResult.Thinking（F8）
func withThinking(o *AskOptions, res *RAGResult) *RAGResult {
	if ss, ok := o.Sink.(*sliceSink); ok && len(ss.steps) > 0 {
		res.Thinking = ss.steps
	}
	return res
}

// RAGEngine 编排实现：历史 → 改写 → 检索 → 组装 → 生成 → 落历史
type RAGEngine struct {
	cfg       config.RAGConfig
	llm       llm.LLM
	retriever retriever.Retriever
	history   HistoryStore
	templates promptTemplates
	embedder  embedding.Embedder  // HyDE 用（nil 时 HyDE 禁用）
	sources   datasource.Registry // 数据源注册中心（nil 时构建默认：vector_store + web 占位）
}

// EngineOption 引擎构造选项
type EngineOption func(*RAGEngine)

// WithSources 注入数据源注册中心（可动态注册自定义数据源；nil 用默认）
func WithSources(reg datasource.Registry) EngineOption {
	return func(e *RAGEngine) { e.sources = reg }
}

// ragCfgFor 返回请求级 RAG 配置（快照优先）
func (e *RAGEngine) ragCfgFor(o AskOptions) config.RAGConfig {
	if o.CfgSnapshot != nil {
		return o.CfgSnapshot.RAG
	}
	return e.cfg
}

// effective 解析合并策略（请求 > 知识库 > 全局），失败返回全局默认降级
func (e *RAGEngine) effective(o AskOptions) EffectiveStrategy {
	// 全局默认：请求级快照优先（热重载一致性），否则引擎构建时配置
	ragCfg := e.cfg
	if o.CfgSnapshot != nil {
		ragCfg = o.CfgSnapshot.RAG
	}
	global := ragCfg.Strategy
	if global.Query == "" && global.Fusion == "" && global.Decomposition == "" &&
		global.StepBack == "" && global.HyDE == "" && global.Routing == "" {
		// 旧开关兜底：从 *On() 推导；thinking 单独保留（不参与兜底判断，
		// 否则仅配置 thinking 会阻断旧开关映射其他策略）
		thinking := global.Thinking
		dataSources := global.DataSources // 数据源允许集合独立于策略单值，兜底时保留
		global = config.StrategyConfig{}
		if ragCfg.MultiQueryOn() {
			global.Query = "multi"
		} else {
			global.Query = "single" // 显式单查询（防止默认 multi）
			global.Fusion = "none"  // single 无多路可融合，必须 none
		}
		if ragCfg.DecompositionOn() {
			global.Decomposition = ragCfg.DecompositionMode
			if global.Decomposition == "" {
				global.Decomposition = "parallel"
			}
		}
		if ragCfg.StepBackOn() {
			global.StepBack = "on"
		}
		if ragCfg.RoutingOn() {
			global.Routing = "auto"
		}
		if ragCfg.HyDEOn() {
			global.HyDE = "on"
		}
		global.Thinking = thinking
		global.DataSources = dataSources
	}
	var kb, req config.StrategyConfig
	if o.KBStrategy != nil {
		kb = *o.KBStrategy
	}
	if o.ReqStrategy != nil {
		req = *o.ReqStrategy
	}
	eff, err := ResolveStrategy(global, kb, req)
	if err != nil {
		slog.Warn("策略合并校验失败，降级全局默认", "err", err)
		return DefaultEffectiveStrategy()
	}
	return eff
}

// NewEngine 创建 RAG 编排器
func NewEngine(cfg config.RAGConfig, l llm.LLM, rt retriever.Retriever, hs HistoryStore, emb embedding.Embedder, opts ...EngineOption) Engine {
	e := &RAGEngine{
		cfg:       cfg,
		llm:       l,
		retriever: rt,
		history:   hs,
		templates: loadPromptTemplates(cfg),
		embedder:  emb,
	}
	for _, opt := range opts {
		opt(e)
	}
	// 默认注册中心：向量库（可用）+ web 搜索（占位不可用）；外部可用 WithSources 注入自定义源
	if e.sources == nil {
		reg := datasource.NewRegistry()
		reg.Register(datasource.NewVectorStoreSource(rt))
		reg.Register(datasource.NewWebSearchSource())
		e.sources = reg
	}
	return e
}

// resolveDataSource 解析本次查询实际使用的数据源：
//   - candidate（LLM 路由输出）为空/未知 → vector_store
//   - allowed（允许集合）非空且 candidate 不在其中 → allowed 首项（私有性强制，AC4）
//   - 选中源未注册或不可用 → 降级：优先 allowed 内的可用源（含 vector_store）；allowed 空或全不可用 → vector_store（AC5，不中断请求）
//
// 返回 (源名, 源实例)。
func (e *RAGEngine) resolveDataSource(allowed []string, candidate string) (string, datasource.Source) {
	name := candidate
	if name == "" || name == "auto" {
		name = datasource.SourceVectorStore
	}
	if len(allowed) > 0 && !slices.Contains(allowed, name) {
		name = allowed[0]
	}
	if src, ok := e.sources.Get(name); ok && src.Available() {
		return name, src
	}
	// 候选不可用：降级目标优先在 allowed 集合内找可用源（保证不落到未授权源）
	if len(allowed) > 0 {
		for _, n := range allowed {
			if s, ok2 := e.sources.Get(n); ok2 && s.Available() {
				return n, s
			}
		}
	}
	// allowed 为空或 allowed 内全不可用 → 降级默认向量库（本地内部源，不涉及外部数据外泄；
	// 输出显式告警暴露配置问题，服务可用性优先）
	if _, ok := e.sources.Get(datasource.SourceVectorStore); !ok {
		slog.Warn("数据源降级失败：默认向量库未注册", "allowed", allowed, "candidate", candidate)
		return datasource.SourceVectorStore, nil
	}
	slog.Warn("允许的数据源均不可用，降级默认向量库", "allowed", allowed, "candidate", candidate)
	def, _ := e.sources.Get(datasource.SourceVectorStore)
	return datasource.SourceVectorStore, def
}

// allowedText 渲染允许的数据源说明文本（供路由模板约束 LLM 输出，F6）
func allowedText(allowed []string) string {
	if len(allowed) == 0 {
		return datasource.SourceVectorStore + "（默认）"
	}
	return strings.Join(allowed, " / ")
}

// Ask 单轮问答：历史 → 改写 → 检索 → 组装 → 生成 → 落历史
func (e *RAGEngine) Ask(ctx context.Context, sessionID string, question string, opts ...AskOption) (*RAGResult, error) {
	// 策略分支：Decomposition 优先（互斥），其次 Step-Back；不适用/失败落回常规
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	// 思考链路：计算采集器（关闭时 nil），非流式由 engine 自建 sliceSink
	sink := e.sinkFor(&o, o.Sink)
	o.Sink = sink
	eff := e.effective(o)
	slog.Info("生效策略", "query", eff.Query, "fusion", eff.Fusion, "decomposition", eff.Decomposition,
		"step_back", eff.StepBack, "hyde", eff.HyDE, "routing", eff.Routing, "thinking", eff.Thinking)

	// RAG 路由：判定复杂度、策略与数据源 → 按 strategy 分流
	if eff.Routing == "auto" {
		route, ok, err := e.routeQuery(ctx, question, allowedText(eff.DataSources))
		strategy := ""
		if err != nil || !ok {
			strategy = e.ragCfgFor(o).RoutingFallback
			slog.Warn("路由判定失败，回退默认策略", "fallback", strategy, "err", err)
		} else {
			strategy = route.Strategy
			// 数据源解析：受限 allowed 约束（AC4）+ 不可用降级（AC5）
			o.AllowedDataSources = eff.DataSources
			dsName, _ := e.resolveDataSource(eff.DataSources, route.DataSource)
			o.DataSource = dsName
			if route.DataSource != "" && route.DataSource != dsName {
				slog.Warn("数据源被约束或不可用，改用默认", "candidate", route.DataSource,
					"allowed", eff.DataSources, "used", dsName)
			}
			slog.Info("路由判定完成", "complexity", route.Complexity, "strategy", strategy,
				"data_source", dsName, "reasoning", route.Reasoning)
			// 思考链路：路由判定（F2，判定失败不 Record）
			recordStep(sink, ThinkingStep{
				Type:  StepRouting,
				Label: "路由判定",
				Data:  RoutingData{Complexity: route.Complexity, Strategy: route.Strategy, DataSource: dsName, Reasoning: route.Reasoning},
			})
			// 路由 direct：简单问题直接检索——本次查询强制 single，跳过多查询改写（F-A1）
			if strategy == "direct" {
				o.ForceSingle = true
			}
		}
		// 按策略分流：decomposition → 分解；multi_query → 多查询；direct → 常规（含现有策略分支）。
		// 非向量数据源（如 web_search）时跳过策略增强，走常规单次数据源检索（技术决策：搜哪与怎么搜不叠加）
		if o.DataSource == "" || o.DataSource == datasource.SourceVectorStore {
			if strategy == "decomposition" {
				if res, ok2, err2 := e.tryDecompose(ctx, sessionID, question, o); err2 == nil && ok2 {
					return res, nil
				}
			} else if strategy == "multi_query" {
				if res, ok2, err2 := e.tryMultiQuery(ctx, sessionID, question, o); err2 == nil && ok2 {
					return res, nil
				}
			}
		}
		// direct 或策略失败 → 落常规
	}

	if o.DataSource == "" || o.DataSource == datasource.SourceVectorStore {
		if eff.Decomposition != "off" {
			if res, ok, err := e.tryDecompose(ctx, sessionID, question, o); err == nil && ok {
				return res, nil
			}
		} else if eff.StepBack == "on" {
			if res, ok, err := e.tryStepBack(ctx, sessionID, question, o); err == nil && ok {
				return res, nil
			}
		}
	}

	prepareOpts := append(opts, WithSink(sink))
	prepareOpts = append(prepareOpts, withForceSingleIf(&o)...)
	prepareOpts = append(prepareOpts, withDataSource(o.DataSource))
	prepareOpts = append(prepareOpts, withAllowedDataSources(o.AllowedDataSources))
	messages, sources, err := e.prepare(ctx, sessionID, question, prepareOpts...)
	if err != nil {
		return nil, err
	}

	// 检索结果为空：跳过 LLM 生成，直接兜底回答
	if len(sources) == 0 {
		e.appendHistory(sessionID, llm.RoleUser, question, "")
		e.appendHistory(sessionID, llm.RoleAssistant, noAnswerText, "")
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), nil
	}

	start := time.Now()
	answer, err := e.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}
	slog.Info("RAG 生成完成", "session", sessionID, "生成耗时ms", time.Since(start).Milliseconds())

	e.appendHistory(sessionID, llm.RoleUser, question, "")
	e.appendHistory(sessionID, llm.RoleAssistant, answer, marshalSources(sources))

	return withThinking(&o, &RAGResult{Answer: answer, Sources: sources}), nil
}

// StreamAsk 流式问答：事件序列 EventSources → EventChunk×N → EventDone；出错发 EventError
func (e *RAGEngine) StreamAsk(ctx context.Context, sessionID string, question string, opts ...AskOption) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent)

	// 策略分支：Decomposition 优先（互斥），其次 Step-Back；综合结果以流式事件发出
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	go func() {
		defer close(out)

		// 思考链路：计算采集器。优先用外部注入（测试/特殊场景）；否则 engine 内部
		// 转发到事件通道——thinking 事件与 sources/chunk 同通道同顺序，天然满足
		// 「thinking 全在 sources 前」（prepare 先完成全部埋点再发 sources）。
		var sink TraceSink
		if o.Sink != nil {
			sink = o.Sink
		} else if o.Thinking && e.effective(o).Thinking == "on" {
			sink = TraceSinkFunc(func(step ThinkingStep) {
				sendEvent(ctx, out, StreamEvent{Type: EventThinking, Thinking: &step})
			})
		}
		o.Sink = sink
		eff := e.effective(o)
		slog.Info("生效策略", "query", eff.Query, "fusion", eff.Fusion, "decomposition", eff.Decomposition,
			"step_back", eff.StepBack, "hyde", eff.HyDE, "routing", eff.Routing, "thinking", eff.Thinking)

		// 策略路径（含思考链路事件，均在 goroutine 内发送保证接收者就绪）
		var strategyRes *RAGResult
		// RAG 路由：判定复杂度、策略与数据源 → 按 strategy 分流
		if eff.Routing == "auto" {
			route, ok, err := e.routeQuery(ctx, question, allowedText(eff.DataSources))
			strategy := ""
			if err != nil || !ok {
				strategy = e.ragCfgFor(o).RoutingFallback
				slog.Warn("路由判定失败，回退默认策略", "fallback", strategy, "err", err)
			} else {
				strategy = route.Strategy
				// 数据源解析：受限 allowed 约束（AC4）+ 不可用降级（AC5）
				o.AllowedDataSources = eff.DataSources
				dsName, _ := e.resolveDataSource(eff.DataSources, route.DataSource)
				o.DataSource = dsName
				if route.DataSource != "" && route.DataSource != dsName {
					slog.Warn("数据源被约束或不可用，改用默认", "candidate", route.DataSource,
						"allowed", eff.DataSources, "used", dsName)
				}
				slog.Info("路由判定完成", "complexity", route.Complexity, "strategy", strategy,
					"data_source", dsName, "reasoning", route.Reasoning)
				// 思考链路：路由判定（F2，判定失败不 Record）
				recordStep(sink, ThinkingStep{
					Type:  StepRouting,
					Label: "路由判定",
					Data:  RoutingData{Complexity: route.Complexity, Strategy: route.Strategy, DataSource: dsName, Reasoning: route.Reasoning},
				})
				// 路由 direct：简单问题直接检索——本次查询强制 single，跳过多查询改写（F-A1）
				if strategy == "direct" {
					o.ForceSingle = true
				}
			}
			// 非向量数据源时跳过策略分流，走常规单次数据源检索
			if o.DataSource == "" || o.DataSource == datasource.SourceVectorStore {
				if strategy == "decomposition" {
					if res, ok2, err2 := e.tryDecompose(ctx, sessionID, question, o); err2 == nil && ok2 {
						strategyRes = res
					}
				} else if strategy == "multi_query" {
					if res, ok2, err2 := e.tryMultiQuery(ctx, sessionID, question, o); err2 == nil && ok2 {
						strategyRes = res
					}
				}
			}
		}
		if strategyRes == nil && (o.DataSource == "" || o.DataSource == datasource.SourceVectorStore) {
			if eff.Decomposition != "off" {
				if res, ok, err := e.tryDecompose(ctx, sessionID, question, o); err == nil && ok {
					strategyRes = res
				}
			} else if eff.StepBack == "on" {
				if res, ok, err := e.tryStepBack(ctx, sessionID, question, o); err == nil && ok {
					strategyRes = res
				}
			}
		}

		// 策略路径已生成完整回答：直接流式发出
		if strategyRes != nil {
			sendEvent(ctx, out, StreamEvent{Type: EventSources, Sources: strategyRes.Sources})
			sendEvent(ctx, out, StreamEvent{Type: EventChunk, Content: strategyRes.Answer})
			sendEvent(ctx, out, StreamEvent{Type: EventDone})
			return
		}

		prepareOpts := append(opts, WithSink(sink))
		prepareOpts = append(prepareOpts, withForceSingleIf(&o)...)
		prepareOpts = append(prepareOpts, withDataSource(o.DataSource))
		prepareOpts = append(prepareOpts, withAllowedDataSources(o.AllowedDataSources))
		messages, sources, err := e.prepare(ctx, sessionID, question, prepareOpts...)
		if err != nil {
			sendEvent(ctx, out, StreamEvent{Type: EventError, Err: err})
			return
		}

		sendEvent(ctx, out, StreamEvent{Type: EventSources, Sources: sources})

		// 检索结果为空：直接兜底回答
		if len(sources) == 0 {
			sendEvent(ctx, out, StreamEvent{Type: EventChunk, Content: noAnswerText})
			e.appendHistory(sessionID, llm.RoleUser, question, "")
			e.appendHistory(sessionID, llm.RoleAssistant, noAnswerText, "")
			sendEvent(ctx, out, StreamEvent{Type: EventDone})
			return
		}

		ch, err := e.llm.StreamGenerate(ctx, messages)
		if err != nil {
			sendEvent(ctx, out, StreamEvent{Type: EventError, Err: err})
			return
		}

		var sb strings.Builder
		for chunk := range ch {
			if chunk.Err != nil {
				sendEvent(ctx, out, StreamEvent{Type: EventError, Err: chunk.Err})
				return
			}
			if chunk.Done {
				break
			}
			sb.WriteString(chunk.Content)
			if !sendEvent(ctx, out, StreamEvent{Type: EventChunk, Content: chunk.Content}) {
				return
			}
		}

		// ctx 取消：丢弃截断结果，不落历史、不发 Done
		if ctx.Err() != nil {
			return
		}

		e.appendHistory(sessionID, llm.RoleUser, question, "")
		e.appendHistory(sessionID, llm.RoleAssistant, sb.String(), marshalSources(sources))
		sendEvent(ctx, out, StreamEvent{Type: EventDone})
	}()

	return out, nil
}

// appendHistory 写入对话历史，失败仅告警不阻断主流程
func (e *RAGEngine) appendHistory(sessionID string, role string, content string, sourcesJSON string) {
	if err := e.history.Append(sessionID, role, content, sourcesJSON); err != nil {
		slog.Warn("写入对话历史失败", "session", sessionID, "err", err)
	}
}

// prepare 执行 历史读取 → 改写 → 检索 → 组装，返回可注入的 messages 与引用来源
func (e *RAGEngine) prepare(ctx context.Context, sessionID string, question string, opts ...AskOption) ([]llm.Message, []Source, error) {
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	eff := e.effective(o)
	// routing 判定 direct：本次查询强制 single，跳过多查询改写与多路检索（F-A1）
	if o.ForceSingle {
		eff.Query = "single"
	}
	// 非向量数据源：强制单查询（不叠加多查询增强），由下方数据源检索统一处理
	if o.DataSource != "" && o.DataSource != datasource.SourceVectorStore {
		eff.Query = "single"
	}
	// 白名单兜底（私有性防御）：若调用方指定了允许集合，数据源不得超出集合
	if o.DataSource != "" && o.DataSource != datasource.SourceVectorStore &&
		len(o.AllowedDataSources) > 0 && !slices.Contains(o.AllowedDataSources, o.DataSource) {
		slog.Warn("数据源超出允许集合，降级向量库", "data_source", o.DataSource, "allowed", o.AllowedDataSources)
		o.DataSource = datasource.SourceVectorStore
	}
	sink := o.Sink // 思考链路采集器（由 Ask/StreamAsk 预先计算设置）
	// 请求级配置快照（热重载一致性）：prepare 内 RAG 参数用快照
	ragCfg := e.cfg
	if o.CfgSnapshot != nil {
		ragCfg = o.CfgSnapshot.RAG
	}

	history, err := e.history.Get(sessionID, ragCfg.HistoryLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("读取对话历史失败: %w", err)
	}

	// Query 改写 / 多查询（多查询启用时替代单查询改写）
	query := question
	if eff.Query == "multi" {
		// 多查询路径：生成变体 → 多路检索
		multiStart := time.Now()
		queries, err := e.multiQuery(ctx, history, question)
		if err != nil {
			slog.Warn("多查询生成失败，降级单查询", "err", err)
			// 降级：走现有单查询改写路径
			if ragCfg.RewriteEnabled() {
				rewritten, rerr := e.rewriteQuery(ctx, history, question)
				if rerr != nil {
					slog.Warn("Query 改写失败，降级使用原问题", "err", rerr)
					// 思考链路：改写失败降级（F3）
					recordStep(sink, ThinkingStep{
						Type:  StepRewrite,
						Label: "查询改写",
						Data:  RewriteData{Original: question, Rewritten: question, Fallback: true},
					})
				} else {
					query = rewritten
					slog.Info("Query 改写完成", "原问题", question, "改写后", query,
						"耗时ms", time.Since(multiStart).Milliseconds())
					// 思考链路：单查询改写（F3）
					recordStep(sink, ThinkingStep{
						Type:  StepRewrite,
						Label: "查询改写",
						Data:  RewriteData{Original: question, Rewritten: rewritten},
					})
				}
			}
		} else {
			slog.Info("多查询生成完成", "变体数", len(queries), "变体", queries,
				"耗时ms", time.Since(multiStart).Milliseconds())
			// 思考链路：多查询变体（F3）
			recordStep(sink, ThinkingStep{
				Type:  StepMultiQuery,
				Label: "多查询改写",
				Data:  MultiQueryData{Variants: queries},
			})
			// 多路检索
			retrieveStart := time.Now()
			chunks, err := e.retriever.SearchMulti(ctx, retriever.RetrieveRequest{
				Query:  query,
				TopK:   ragCfg.TopK,
				Filter: kbFilter(o.KBID),
				Trace:  traceSinkForRequest(sink, query),
			}, queries)
			if err != nil {
				return nil, nil, fmt.Errorf("多路检索失败: %w", err)
			}
			slog.Info("检索完成", "检索耗时ms", time.Since(retrieveStart).Milliseconds(), "召回数", len(chunks))

			// 上下文组装
			items, sources := buildContext(chunks, ragCfg.MaxContextTokens, ragCfg.MaxChunks)
			contextText, err := renderContext(items, e.templates.context)
			if err != nil {
				return nil, nil, fmt.Errorf("渲染上下文失败: %w", err)
			}
			slog.Info("上下文组装完成", "上下文token", estimateTokens(contextText), "引用数", len(sources))
			// 思考链路：目标 chunks（F6）
			recordStep(sink, ThinkingStep{
				Type:  StepChunks,
				Label: "目标片段",
				Data:  chunksDataFrom(items, sources),
			})

			// 组装 messages：system + 历史 + user（上下文 + 原始问题）
			messages := make([]llm.Message, 0, 2+len(history))
			messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: e.templates.system})
			messages = append(messages, history...)
			userContent := contextText + "\n\n用户问题：" + question
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})

			return messages, sources, nil
		}
	} else if ragCfg.RewriteEnabled() {
		rewriteStart := time.Now()
		rewritten, err := e.rewriteQuery(ctx, history, question)
		if err != nil {
			slog.Warn("Query 改写失败，降级使用原问题", "err", err)
			// 思考链路：改写失败降级（F3）
			recordStep(sink, ThinkingStep{
				Type:  StepRewrite,
				Label: "查询改写",
				Data:  RewriteData{Original: question, Rewritten: question, Fallback: true},
			})
		} else {
			query = rewritten
			slog.Info("Query 改写完成", "原问题", question, "改写后", query,
				"耗时ms", time.Since(rewriteStart).Milliseconds())
			// 思考链路：单查询改写（F3）
			recordStep(sink, ThinkingStep{
				Type:  StepRewrite,
				Label: "查询改写",
				Data:  RewriteData{Original: question, Rewritten: rewritten},
			})
		}
	}

	// 检索（携带知识库范围）；HyDE 启用且非多查询路径时用 hydeSearch 增强；
	// 路由到非向量数据源时（o.DataSource 由 Ask/StreamAsk 设置）走数据源注册中心检索
	retrieveStart := time.Now()
	var chunks []retriever.RetrieveResult
	if o.DataSource != "" && o.DataSource != datasource.SourceVectorStore {
		src, ok := e.sources.Get(o.DataSource)
		if !ok || !src.Available() {
			// 源缺失/不可用：降级向量库（防御，正常已由路由段处理）
			slog.Warn("数据源不可用，降级向量库", "data_source", o.DataSource)
			chunks, err = e.retriever.Search(ctx, retriever.RetrieveRequest{
				Query:  query,
				TopK:   ragCfg.TopK,
				Filter: kbFilter(o.KBID),
				Trace:  traceSinkForRequest(sink, query),
			})
		} else {
			chunks, err = src.Search(ctx, datasource.SearchRequest{
				Query:  query,
				TopK:   ragCfg.TopK,
				Filter: kbFilter(o.KBID),
			})
			slog.Info("数据源检索完成", "data_source", o.DataSource,
				"检索耗时ms", time.Since(retrieveStart).Milliseconds(), "召回数", len(chunks))
		}
	} else if eff.HyDE == "on" && e.embedder != nil && eff.Query != "multi" {
		chunks, err = e.hydeSearch(ctx, query, o)
	} else {
		chunks, err = e.retriever.Search(ctx, retriever.RetrieveRequest{
			Query:  query,
			TopK:   ragCfg.TopK,
			Filter: kbFilter(o.KBID),
			Trace:  traceSinkForRequest(sink, query),
		})
	}
	if err != nil {
		return nil, nil, fmt.Errorf("检索失败: %w", err)
	}
	slog.Info("检索完成", "检索耗时ms", time.Since(retrieveStart).Milliseconds(), "召回数", len(chunks))

	// 上下文组装
	items, sources := buildContext(chunks, ragCfg.MaxContextTokens, ragCfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, nil, fmt.Errorf("渲染上下文失败: %w", err)
	}
	slog.Info("上下文组装完成", "上下文token", estimateTokens(contextText), "引用数", len(sources))
	// 思考链路：目标 chunks（F6）
	recordStep(sink, ThinkingStep{
		Type:  StepChunks,
		Label: "目标片段",
		Data:  chunksDataFrom(items, sources),
	})

	// 组装 messages：system + 历史 + user（上下文 + 原始问题）
	messages := make([]llm.Message, 0, 2+len(history))
	messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: e.templates.system})
	messages = append(messages, history...)
	userContent := contextText + "\n\n用户问题：" + question
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})

	return messages, sources, nil
}

// rewriteQuery 调用 LLM 改写查询（固定低温，仅输出改写结果）
func (e *RAGEngine) rewriteQuery(ctx context.Context, history []llm.Message, question string) (string, error) {
	prompt, err := renderRewrite(history, question, e.templates.rewrite)
	if err != nil {
		return "", fmt.Errorf("渲染改写提示失败: %w", err)
	}

	rewritten, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.1),
	)
	if err != nil {
		return "", fmt.Errorf("改写调用失败: %w", err)
	}

	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" {
		return "", fmt.Errorf("改写结果为空")
	}
	return rewritten, nil
}

// kbFilter 构造知识库过滤条件（空 kbID 返回 nil 表示不过滤）
func kbFilter(kbID string) map[string]any {
	if kbID == "" {
		return nil
	}
	return map[string]any{"kb_id": kbID}
}

// multiQuery 调用 LLM 生成多查询变体（JSON 数组），返回 [原问题] + 变体。
// 解析失败返回 error（调用方降级单查询）。
func (e *RAGEngine) multiQuery(ctx context.Context, history []llm.Message, question string) ([]string, error) {
	count := e.cfg.MultiQueryCount
	if count <= 0 {
		count = 3
	}
	prompt, err := renderMultiQuery(history, question, count, e.templates.multiQuery)
	if err != nil {
		return nil, fmt.Errorf("渲染多查询提示失败: %w", err)
	}

	out, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.1),
	)
	if err != nil {
		return nil, fmt.Errorf("生成多查询变体失败: %w", err)
	}

	var variants []string
	if err := json.Unmarshal([]byte(out), &variants); err != nil {
		return nil, fmt.Errorf("解析多查询变体失败（非 JSON 数组）: %w", err)
	}
	// 过滤空串，去重，限制数量
	seen := make(map[string]bool, len(variants)+1)
	queries := make([]string, 0, len(variants)+1)
	queries = append(queries, question)
	seen[question] = true
	for _, v := range variants {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		queries = append(queries, v)
	}
	if len(queries) <= 1 {
		return nil, fmt.Errorf("多查询变体为空")
	}
	return queries, nil
}

// sendEvent 发送事件；ctx 取消时返回 false
func sendEvent(ctx context.Context, out chan<- StreamEvent, ev StreamEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// marshalSources 序列化引用来源为 JSON 字符串（历史持久化用）；空/失败返回空串
func marshalSources(sources []Source) string {
	if len(sources) == 0 {
		return ""
	}
	b, err := json.Marshal(sources)
	if err != nil {
		return ""
	}
	return string(b)
}
