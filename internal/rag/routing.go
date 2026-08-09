package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// routeResult 路由判定结果
type routeResult struct {
	Complexity string // simple / medium / complex
	Strategy   string // direct / multi_query / decomposition
	DataSource string // vector_store / web_search（空=默认 vector_store）
	Reasoning  string
}

// routeQuery 调用 LLM 判定查询复杂度、策略与数据源。解析失败返回 (zero, false, err)（外层回退默认策略）。
// allowedText 为允许的数据源说明（约束 LLM 只在允许范围内选择数据源）。
func (e *RAGEngine) routeQuery(ctx context.Context, question, allowedText string) (routeResult, bool, error) {
	prompt, err := renderRouting(question, allowedText, e.templates.routing)
	if err != nil {
		return routeResult{}, false, fmt.Errorf("渲染路由提示失败: %w", err)
	}
	out, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.0),
	)
	if err != nil {
		return routeResult{}, false, fmt.Errorf("路由判定调用失败: %w", err)
	}
	var parsed struct {
		Complexity string `json:"complexity"`
		Strategy   string `json:"strategy"`
		DataSource string `json:"data_source"`
		Reasoning  string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return routeResult{}, false, fmt.Errorf("解析路由判定失败（非 JSON）: %w", err)
	}
	return routeResult{Complexity: parsed.Complexity, Strategy: parsed.Strategy, DataSource: parsed.DataSource, Reasoning: parsed.Reasoning}, true, nil
}

// shouldHyde 判断当前查询是否应用 HyDE（skip_simple 且 simple 查询时跳过）
func (e *RAGEngine) shouldHyde(route routeResult) bool {
	if !e.cfg.HyDEOn() || e.embedder == nil {
		return false
	}
	if e.cfg.HyDESkipSimpleOn() && route.Complexity == "simple" {
		return false
	}
	return true
}

// hydeSearch 假设文档检索：生成假设文档 → embed → SearchByVector（HyDE 路）
// + Search（原查询路）→ FuseMultiQuery 融合。Embedding 失败降级原查询。
func (e *RAGEngine) hydeSearch(ctx context.Context, query string, o AskOptions) ([]retriever.RetrieveResult, error) {
	start := time.Now()
	sink := o.Sink // 思考链路采集器（由 Ask/StreamAsk 预先设置）

	// 1. 生成假设文档
	prompt, err := renderHyDE(query, e.templates.hyde)
	if err != nil {
		slog.Warn("HyDE 提示渲染失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}
	hypoDoc, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.3),
	)
	if err != nil {
		slog.Warn("HyDE 假设文档生成失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}
	slog.Info("HyDE 假设文档生成", "耗时ms", time.Since(start).Milliseconds())
	// 思考链路：假设文档（F7/N3，服务端截断）
	recordStep(sink, ThinkingStep{
		Type:  StepHyDE,
		Label: "HyDE 假设文档",
		Data:  HyDEData{HypoDoc: truncateRunes(hypoDoc, maxHypoDocPreviewLen)},
	})

	// 2. 假设文档向量化
	vectors, err := e.embedder.Embed(ctx, []string{hypoDoc})
	if err != nil || len(vectors) == 0 {
		slog.Warn("HyDE Embedding 失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}

	// 3. HyDE 向量路 + 原查询路（思考链路：HyDE 双路，F7）
	hydeResults, err := e.retriever.SearchByVector(ctx, vectors[0], e.cfg.TopK, kbFilter(o.KBID))
	if err != nil {
		slog.Warn("HyDE 向量检索失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}
	recordStep(sink, ThinkingStep{
		Type:  StepRetrieval,
		Label: "HyDE 向量检索",
		Data:  RetrievalData{Query: query, Method: "hyde_vector", Recalled: len(hydeResults)},
	})
	origResults, err := e.retriever.Search(ctx, retriever.RetrieveRequest{
		Query:      query,
		TopK:       e.cfg.TopK,
		Filter:     kbFilter(o.KBID),
		SkipRerank: true, // 融合后统一整体重排（F2）
	})
	if err != nil {
		slog.Warn("HyDE 原查询检索失败，使用 HyDE 结果", "err", err)
		return hydeResults, nil
	}
	recordStep(sink, ThinkingStep{
		Type:  StepRetrieval,
		Label: "HyDE 原查询检索",
		Data:  RetrievalData{Query: query, Method: "hyde_orig", Recalled: len(origResults)},
	})

	// 4. 融合
	fused := retriever.FuseMultiQuery([][]retriever.RetrieveResult{hydeResults, origResults}, 60, e.cfg.TopK)
	slog.Info("HyDE 检索完成", "HyDE路", len(hydeResults), "原查询路", len(origResults), "融合数", len(fused),
		"耗时ms", time.Since(start).Milliseconds())

	// 5. 融合后整体重排一次（F2/AC3；rerank 关闭或失败时降级返回融合结果）
	fused, _ = e.retriever.Rerank(ctx, query, fused, e.cfg.TopK, traceSinkForRequest(sink, query))
	return fused, nil
}

// tryMultiQuery 多查询路径（路由 strategy=multi_query 时调用）：
// 生成变体 → SearchMulti 多路检索 → 综合生成。复用阶段一逻辑。
func (e *RAGEngine) tryMultiQuery(ctx context.Context, sessionID string, question string, o AskOptions) (*RAGResult, bool, error) {
	sink := o.Sink // 思考链路采集器（由 Ask/StreamAsk 预先设置）
	// 生成变体（与阶段一 multiQuery 一致）
	queries, err := e.multiQuery(ctx, nil, question)
	if err != nil {
		slog.Warn("多查询变体生成失败，落回常规路径", "err", err)
		return nil, false, err
	}
	slog.Info("多查询生成完成", "变体数", len(queries), "变体", queries)
	// 思考链路：多查询变体（F3）
	recordStep(sink, ThinkingStep{
		Type:  StepMultiQuery,
		Label: "多查询改写",
		Data:  MultiQueryData{Variants: queries},
	})

	chunks, err := e.retriever.SearchMulti(ctx, retriever.RetrieveRequest{
		Query:  question,
		TopK:   e.cfg.TopK,
		Filter: kbFilter(o.KBID),
		Trace:  traceSinkForRequest(sink, question),
	}, queries)
	if err != nil {
		slog.Warn("多路检索失败，落回常规路径", "err", err)
		return nil, false, err
	}
	if len(chunks) == 0 {
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), true, nil
	}

	items, sources := buildContext(chunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, false, fmt.Errorf("渲染上下文失败: %w", err)
	}
	// 思考链路：目标 chunks（F6）
	recordStep(sink, ThinkingStep{
		Type:  StepChunks,
		Label: "目标片段",
		Data:  chunksDataFrom(items, sources),
	})

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: e.templates.system},
		{Role: llm.RoleUser, Content: contextText + "\n\n用户问题：" + question},
	}
	answer, err := e.llm.Generate(ctx, messages)
	if err != nil {
		return nil, false, fmt.Errorf("生成失败: %w", err)
	}

	e.appendHistory(sessionID, llm.RoleUser, question, "")
	e.appendHistory(sessionID, llm.RoleAssistant, answer, marshalSources(sources))
	return withThinking(&o, &RAGResult{Answer: answer, Sources: sources}), true, nil
}
