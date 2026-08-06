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
	Reasoning  string
}

// routeQuery 调用 LLM 判定查询复杂度与策略。解析失败返回 (zero, false, err)（外层回退默认策略）。
func (e *RAGEngine) routeQuery(ctx context.Context, question string) (routeResult, bool, error) {
	prompt, err := renderRouting(question, e.templates.routing)
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
		Reasoning  string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return routeResult{}, false, fmt.Errorf("解析路由判定失败（非 JSON）: %w", err)
	}
	return routeResult{Complexity: parsed.Complexity, Strategy: parsed.Strategy, Reasoning: parsed.Reasoning}, true, nil
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

	// 2. 假设文档向量化
	vectors, err := e.embedder.Embed(ctx, []string{hypoDoc})
	if err != nil || len(vectors) == 0 {
		slog.Warn("HyDE Embedding 失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}

	// 3. HyDE 向量路 + 原查询路
	hydeResults, err := e.retriever.SearchByVector(ctx, vectors[0], e.cfg.TopK, kbFilter(o.KBID))
	if err != nil {
		slog.Warn("HyDE 向量检索失败，降级原查询", "err", err)
		return e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	}
	origResults, err := e.retriever.Search(ctx, retriever.RetrieveRequest{Query: query, TopK: e.cfg.TopK, Filter: kbFilter(o.KBID)})
	if err != nil {
		slog.Warn("HyDE 原查询检索失败，使用 HyDE 结果", "err", err)
		return hydeResults, nil
	}

	// 4. 融合
	fused := retriever.FuseMultiQuery([][]retriever.RetrieveResult{hydeResults, origResults}, 60, e.cfg.TopK)
	slog.Info("HyDE 检索完成", "HyDE路", len(hydeResults), "原查询路", len(origResults), "融合数", len(fused),
		"耗时ms", time.Since(start).Milliseconds())
	return fused, nil
}

// tryMultiQuery 多查询路径（路由 strategy=multi_query 时调用）：
// 生成变体 → SearchMulti 多路检索 → 综合生成。复用阶段一逻辑。
func (e *RAGEngine) tryMultiQuery(ctx context.Context, sessionID string, question string, o AskOptions) (*RAGResult, bool, error) {
	// 生成变体（与阶段一 multiQuery 一致）
	queries, err := e.multiQuery(ctx, nil, question)
	if err != nil {
		slog.Warn("多查询变体生成失败，落回常规路径", "err", err)
		return nil, false, err
	}
	slog.Info("多查询生成完成", "变体数", len(queries), "变体", queries)

	chunks, err := e.retriever.SearchMulti(ctx, retriever.RetrieveRequest{
		Query:  question,
		TopK:   e.cfg.TopK,
		Filter: kbFilter(o.KBID),
	}, queries)
	if err != nil {
		slog.Warn("多路检索失败，落回常规路径", "err", err)
		return nil, false, err
	}
	if len(chunks) == 0 {
		return &RAGResult{Answer: noAnswerText}, true, nil
	}

	items, sources := buildContext(chunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, false, fmt.Errorf("渲染上下文失败: %w", err)
	}

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
	return &RAGResult{Answer: answer, Sources: sources}, true, nil
}
