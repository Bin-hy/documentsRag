package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// noAnswerText 检索结果为空时的兜底回答
const noAnswerText = "未找到相关资料。"

// RAGResult 回答结果
type RAGResult struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
}

// EventType 流式事件类型
type EventType int

const (
	EventSources EventType = iota // 引用来源，先发
	EventChunk                    // 文本增量
	EventDone                     // 正常结束
	EventError                    // 出错终止
)

// StreamEvent 流式 RAG 事件
type StreamEvent struct {
	Type    EventType
	Content string   // EventChunk 时有效
	Sources []Source // EventSources 时有效
	Err     error    // EventError 时有效
}

// Engine RAG 编排接口
type Engine interface {
	Ask(ctx context.Context, sessionID string, question string, opts ...AskOption) (*RAGResult, error)
	StreamAsk(ctx context.Context, sessionID string, question string, opts ...AskOption) (<-chan StreamEvent, error)
}

// AskOptions 问答选项
type AskOptions struct {
	KBID string // 知识库范围，空表示不限定
}

// AskOption 函数式问答选项
type AskOption func(*AskOptions)

// WithKBID 限定知识库范围（检索时按 kb_id 过滤）
func WithKBID(kbID string) AskOption {
	return func(o *AskOptions) { o.KBID = kbID }
}

// RAGEngine 编排实现：历史 → 改写 → 检索 → 组装 → 生成 → 落历史
type RAGEngine struct {
	cfg       config.RAGConfig
	llm       llm.LLM
	retriever retriever.Retriever
	history   HistoryStore
	templates promptTemplates
	embedder  embedding.Embedder // HyDE 用（nil 时 HyDE 禁用）
}

// NewEngine 创建 RAG 编排器
func NewEngine(cfg config.RAGConfig, l llm.LLM, rt retriever.Retriever, hs HistoryStore, emb embedding.Embedder) Engine {
	return &RAGEngine{
		cfg:       cfg,
		llm:       l,
		retriever: rt,
		history:   hs,
		templates: loadPromptTemplates(cfg),
		embedder:  emb,
	}
}

// Ask 单轮问答：历史 → 改写 → 检索 → 组装 → 生成 → 落历史
func (e *RAGEngine) Ask(ctx context.Context, sessionID string, question string, opts ...AskOption) (*RAGResult, error) {
	// 策略分支：Decomposition 优先（互斥），其次 Step-Back；不适用/失败落回常规
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	// RAG 路由：判定复杂度 → 按 strategy 分流
	if e.cfg.RoutingOn() {
		route, ok, err := e.routeQuery(ctx, question)
		strategy := ""
		if err != nil || !ok {
			strategy = e.cfg.RoutingFallback
			slog.Warn("路由判定失败，回退默认策略", "fallback", strategy, "err", err)
		} else {
			strategy = route.Strategy
			slog.Info("路由判定完成", "complexity", route.Complexity, "strategy", strategy, "reasoning", route.Reasoning)
		}
		// 按策略分流：decomposition → 分解；multi_query → 多查询；direct → 常规（含现有策略分支）
		if strategy == "decomposition" {
			if res, ok2, err2 := e.tryDecompose(ctx, sessionID, question, o); err2 == nil && ok2 {
				return res, nil
			}
		} else if strategy == "multi_query" {
			if res, ok2, err2 := e.tryMultiQuery(ctx, sessionID, question, o); err2 == nil && ok2 {
				return res, nil
			}
		}
		// direct 或策略失败 → 落常规
	}

	if e.cfg.DecompositionOn() {
		if res, ok, err := e.tryDecompose(ctx, sessionID, question, o); err == nil && ok {
			return res, nil
		}
	} else if e.cfg.StepBackOn() {
		if res, ok, err := e.tryStepBack(ctx, sessionID, question, o); err == nil && ok {
			return res, nil
		}
	}

	messages, sources, err := e.prepare(ctx, sessionID, question, opts...)
	if err != nil {
		return nil, err
	}

	// 检索结果为空：跳过 LLM 生成，直接兜底回答
	if len(sources) == 0 {
		e.appendHistory(sessionID, llm.RoleUser, question)
		e.appendHistory(sessionID, llm.RoleAssistant, noAnswerText)
		return &RAGResult{Answer: noAnswerText}, nil
	}

	start := time.Now()
	answer, err := e.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}
	slog.Info("RAG 生成完成", "session", sessionID, "生成耗时ms", time.Since(start).Milliseconds())

	e.appendHistory(sessionID, llm.RoleUser, question)
	e.appendHistory(sessionID, llm.RoleAssistant, answer)

	return &RAGResult{Answer: answer, Sources: sources}, nil
}

// StreamAsk 流式问答：事件序列 EventSources → EventChunk×N → EventDone；出错发 EventError
func (e *RAGEngine) StreamAsk(ctx context.Context, sessionID string, question string, opts ...AskOption) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent)

	// 策略分支：Decomposition 优先（互斥），其次 Step-Back；综合结果以流式事件发出
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	var strategyRes *RAGResult
	// RAG 路由：判定复杂度 → 按 strategy 分流
	if e.cfg.RoutingOn() {
		route, ok, err := e.routeQuery(ctx, question)
		strategy := ""
		if err != nil || !ok {
			strategy = e.cfg.RoutingFallback
			slog.Warn("路由判定失败，回退默认策略", "fallback", strategy, "err", err)
		} else {
			strategy = route.Strategy
			slog.Info("路由判定完成", "complexity", route.Complexity, "strategy", strategy, "reasoning", route.Reasoning)
		}
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
	if strategyRes == nil && e.cfg.DecompositionOn() {
		if res, ok, err := e.tryDecompose(ctx, sessionID, question, o); err == nil && ok {
			strategyRes = res
		}
	} else if strategyRes == nil && e.cfg.StepBackOn() {
		if res, ok, err := e.tryStepBack(ctx, sessionID, question, o); err == nil && ok {
			strategyRes = res
		}
	}

	go func() {
		defer close(out)

		// 策略路径已生成完整回答：直接流式发出
		if strategyRes != nil {
			sendEvent(ctx, out, StreamEvent{Type: EventSources, Sources: strategyRes.Sources})
			sendEvent(ctx, out, StreamEvent{Type: EventChunk, Content: strategyRes.Answer})
			sendEvent(ctx, out, StreamEvent{Type: EventDone})
			return
		}

		messages, sources, err := e.prepare(ctx, sessionID, question, opts...)
		if err != nil {
			sendEvent(ctx, out, StreamEvent{Type: EventError, Err: err})
			return
		}

		sendEvent(ctx, out, StreamEvent{Type: EventSources, Sources: sources})

		// 检索结果为空：直接兜底回答
		if len(sources) == 0 {
			sendEvent(ctx, out, StreamEvent{Type: EventChunk, Content: noAnswerText})
			e.appendHistory(sessionID, llm.RoleUser, question)
			e.appendHistory(sessionID, llm.RoleAssistant, noAnswerText)
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

		e.appendHistory(sessionID, llm.RoleUser, question)
		e.appendHistory(sessionID, llm.RoleAssistant, sb.String())
		sendEvent(ctx, out, StreamEvent{Type: EventDone})
	}()

	return out, nil
}

// appendHistory 写入对话历史，失败仅告警不阻断主流程
func (e *RAGEngine) appendHistory(sessionID string, role string, content string) {
	if err := e.history.Append(sessionID, role, content); err != nil {
		slog.Warn("写入对话历史失败", "session", sessionID, "err", err)
	}
}

// prepare 执行 历史读取 → 改写 → 检索 → 组装，返回可注入的 messages 与引用来源
func (e *RAGEngine) prepare(ctx context.Context, sessionID string, question string, opts ...AskOption) ([]llm.Message, []Source, error) {
	o := AskOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	history, err := e.history.Get(sessionID, e.cfg.HistoryLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("读取对话历史失败: %w", err)
	}

	// Query 改写 / 多查询（多查询启用时替代单查询改写）
	query := question
	if e.cfg.MultiQueryOn() {
		// 多查询路径：生成变体 → 多路检索
		multiStart := time.Now()
		queries, err := e.multiQuery(ctx, history, question)
		if err != nil {
			slog.Warn("多查询生成失败，降级单查询", "err", err)
			// 降级：走现有单查询改写路径
			if e.cfg.RewriteEnabled() {
				rewritten, rerr := e.rewriteQuery(ctx, history, question)
				if rerr != nil {
					slog.Warn("Query 改写失败，降级使用原问题", "err", rerr)
				} else {
					query = rewritten
					slog.Info("Query 改写完成", "原问题", question, "改写后", query,
						"耗时ms", time.Since(multiStart).Milliseconds())
				}
			}
		} else {
			slog.Info("多查询生成完成", "变体数", len(queries), "变体", queries,
				"耗时ms", time.Since(multiStart).Milliseconds())
			// 多路检索
			retrieveStart := time.Now()
			chunks, err := e.retriever.SearchMulti(ctx, retriever.RetrieveRequest{
				Query:  query,
				TopK:   e.cfg.TopK,
				Filter: kbFilter(o.KBID),
			}, queries)
			if err != nil {
				return nil, nil, fmt.Errorf("多路检索失败: %w", err)
			}
			slog.Info("检索完成", "检索耗时ms", time.Since(retrieveStart).Milliseconds(), "召回数", len(chunks))

			// 上下文组装
			items, sources := buildContext(chunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
			contextText, err := renderContext(items, e.templates.context)
			if err != nil {
				return nil, nil, fmt.Errorf("渲染上下文失败: %w", err)
			}
			slog.Info("上下文组装完成", "上下文token", estimateTokens(contextText), "引用数", len(sources))

			// 组装 messages：system + 历史 + user（上下文 + 原始问题）
			messages := make([]llm.Message, 0, 2+len(history))
			messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: e.templates.system})
			messages = append(messages, history...)
			userContent := contextText + "\n\n用户问题：" + question
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})

			return messages, sources, nil
		}
	} else if e.cfg.RewriteEnabled() {
		rewriteStart := time.Now()
		rewritten, err := e.rewriteQuery(ctx, history, question)
		if err != nil {
			slog.Warn("Query 改写失败，降级使用原问题", "err", err)
		} else {
			query = rewritten
			slog.Info("Query 改写完成", "原问题", question, "改写后", query,
				"耗时ms", time.Since(rewriteStart).Milliseconds())
		}
	}

	// 检索（携带知识库范围）；HyDE 启用且非多查询路径时用 hydeSearch 增强
	retrieveStart := time.Now()
	var chunks []retriever.RetrieveResult
	if e.cfg.HyDEOn() && e.embedder != nil && !e.cfg.MultiQueryOn() {
		chunks, err = e.hydeSearch(ctx, query, o)
	} else {
		chunks, err = e.retriever.Search(ctx, retriever.RetrieveRequest{
			Query:  query,
			TopK:   e.cfg.TopK,
			Filter: kbFilter(o.KBID),
		})
	}
	if err != nil {
		return nil, nil, fmt.Errorf("检索失败: %w", err)
	}
	slog.Info("检索完成", "检索耗时ms", time.Since(retrieveStart).Milliseconds(), "召回数", len(chunks))

	// 上下文组装
	items, sources := buildContext(chunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, nil, fmt.Errorf("渲染上下文失败: %w", err)
	}
	slog.Info("上下文组装完成", "上下文token", estimateTokens(contextText), "引用数", len(sources))

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
