package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// decomposeResult 分解判定与子问题
type decomposeResult struct {
	ShouldDecompose bool
	SubQuestions    []string
}

// stepBackResult 回退判定与回退问题
type stepBackResult struct {
	ShouldStepBack bool
	StepBackQuery  string
}

// judgeDecompose 调用 LLM 判定问题是否需要分解
func (e *RAGEngine) judgeDecompose(ctx context.Context, question string) (bool, error) {
	prompt, err := renderDecomposeJudge(question, e.templates.decomposeJudge)
	if err != nil {
		return false, fmt.Errorf("渲染分解判定提示失败: %w", err)
	}
	out, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.0),
	)
	if err != nil {
		return false, fmt.Errorf("分解判定调用失败: %w", err)
	}
	var parsed struct {
		Decompose bool `json:"decompose"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return false, fmt.Errorf("解析分解判定失败（非 JSON）: %w", err)
	}
	return parsed.Decompose, nil
}

// listSubQuestions 调用 LLM 生成子问题列表（JSON 数组），去重/限长
func (e *RAGEngine) listSubQuestions(ctx context.Context, question string) ([]string, error) {
	maxSub := e.cfg.DecompositionMaxSub
	if maxSub <= 0 {
		maxSub = 5
	}
	prompt, err := renderDecomposeList(question, maxSub, e.templates.decomposeList)
	if err != nil {
		return nil, fmt.Errorf("渲染子问题提示失败: %w", err)
	}
	out, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.0),
	)
	if err != nil {
		return nil, fmt.Errorf("子问题生成调用失败: %w", err)
	}
	var subs []string
	if err := json.Unmarshal([]byte(out), &subs); err != nil {
		return nil, fmt.Errorf("解析子问题失败（非 JSON 数组）: %w", err)
	}
	// 去重、过滤空串、限长
	seen := make(map[string]bool, len(subs))
	result := make([]string, 0, len(subs))
	for _, s := range subs {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		result = append(result, s)
		if len(result) >= maxSub {
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("子问题列表为空")
	}
	return result, nil
}

// judgeStepBack 调用 LLM 判定是否需要回退并生成回退问题
func (e *RAGEngine) judgeStepBack(ctx context.Context, question string) (stepBackResult, error) {
	prompt, err := renderStepBackJudge(question, e.templates.stepBackJudge)
	if err != nil {
		return stepBackResult{}, fmt.Errorf("渲染回退判定提示失败: %w", err)
	}
	out, err := e.llm.Generate(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: prompt}},
		llm.WithTemperature(0.0),
	)
	if err != nil {
		return stepBackResult{}, fmt.Errorf("回退判定调用失败: %w", err)
	}
	var parsed struct {
		StepBack bool   `json:"step_back"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return stepBackResult{}, fmt.Errorf("解析回退判定失败（非 JSON）: %w", err)
	}
	return stepBackResult{ShouldStepBack: parsed.StepBack, StepBackQuery: strings.TrimSpace(parsed.Question)}, nil
}

// searchSubQuery 检索单个查询（Multi-Query 启用时用 SearchMulti），返回检索结果
func (e *RAGEngine) searchSubQuery(ctx context.Context, query string, kbID string) ([]retriever.RetrieveResult, error) {
	req := retriever.RetrieveRequest{
		Query:      query,
		TopK:       e.cfg.TopK,
		Filter:     kbFilter(kbID),
		SkipRerank: true, // 路内不 rerank：由 tryDecompose/tryStepBack 汇总后统一整体重排（F3/F4）
	}
	if e.cfg.MultiQueryOn() {
		return e.retriever.SearchMulti(ctx, req, []string{query})
	}
	return e.retriever.Search(ctx, req)
}

// tryDecompose 问题分解流程：
// 判定为复杂 → 生成子问题 → 逐子问题检索（parallel 并发 / sequential 前序拼入）→ 一次综合生成。
// 返回 ok=false 表示判定为不适用（调用方落回常规路径）；err 非 nil 表示内部失败（调用方静默降级）。
func (e *RAGEngine) tryDecompose(ctx context.Context, sessionID string, question string, o AskOptions) (*RAGResult, bool, error) {
	start := time.Now()
	sink := o.Sink // 思考链路采集器（由 Ask/StreamAsk 预先设置）
	// 1. 判定
	should, err := e.judgeDecompose(ctx, question)
	if err != nil {
		slog.Warn("分解判定失败，降级常规路径", "err", err)
		return nil, false, err
	}
	if !should {
		slog.Info("分解判定：不需要分解，走常规路径")
		return nil, false, nil
	}
	slog.Info("分解判定：需要分解", "耗时ms", time.Since(start).Milliseconds())

	// 2. 生成子问题
	subs, err := e.listSubQuestions(ctx, question)
	if err != nil {
		slog.Warn("子问题生成失败，降级常规路径", "err", err)
		return nil, false, err
	}
	slog.Info("分解为子问题", "数量", len(subs), "子问题", subs)
	// 思考链路：分解判定 + 子问题（F7）
	recordStep(sink, ThinkingStep{
		Type:  StepDecompose,
		Label: "问题分解",
		Data:  DecomposeData{ShouldDecompose: true, SubQuestions: subs},
	})

	// 3. 逐子问题检索
	mode := e.cfg.DecompositionMode
	if mode == "" {
		mode = "parallel"
	}
	var subChunks [][]retriever.RetrieveResult
	if mode == "sequential" {
		// 顺序：前序子问题的检索上下文拼入后续 prompt 作参考（简化：顺序执行，逐个子问题独立检索）
		subChunks = make([][]retriever.RetrieveResult, len(subs))
		for i, s := range subs {
			chunks, serr := e.searchSubQuery(ctx, s, o.KBID)
			if serr != nil {
				slog.Warn("子问题检索失败，忽略该子问题", "sub", s, "err", serr)
				continue
			}
			subChunks[i] = chunks
		}
	} else {
		// 并行：errgroup 风格并发（信号量限流）
		subChunks = make([][]retriever.RetrieveResult, len(subs))
		concurrency := e.cfg.MultiQueryConcurrency
		if concurrency <= 0 {
			concurrency = 3
		}
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for i, s := range subs {
			i, s := i, s
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				chunks, serr := e.searchSubQuery(ctx, s, o.KBID)
				if serr != nil {
					slog.Warn("子问题检索失败，忽略该子问题", "sub", s, "err", serr)
					return
				}
				subChunks[i] = chunks
			}()
		}
		wg.Wait()
	}

	// 思考链路：逐子问题检索（F7；主 goroutine 顺序 Record，N7 无竞争）
	for i, s := range subs {
		recordStep(sink, ThinkingStep{
			Type:  StepRetrieval,
			Label: "子问题检索",
			Data:  RetrievalData{Query: s, Method: "sub_query", Recalled: len(subChunks[i])},
		})
	}

	// 4. 汇总检索结果与来源
	var allChunks []retriever.RetrieveResult
	for _, chunks := range subChunks {
		allChunks = append(allChunks, chunks...)
	}
	if len(allChunks) == 0 {
		slog.Warn("分解后无任何检索结果，走兜底")
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), true, nil
	}
	// 汇总后整体重排一次（F3/AC4；rerank 关闭或失败时降级返回原结果）
	allChunks, _ = e.retriever.Rerank(ctx, question, allChunks, e.cfg.MaxChunks, traceSinkForRequest(sink, question))
	if len(allChunks) == 0 {
		slog.Warn("分解后重排结果为空，走兜底")
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), true, nil
	}

	items, sources := buildContext(allChunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, false, fmt.Errorf("渲染上下文失败: %w", err)
	}
	slog.Info("分解综合", "上下文token", estimateTokens(contextText), "引用数", len(sources), "耗时ms", time.Since(start).Milliseconds())
	// 思考链路：目标 chunks（F6）
	recordStep(sink, ThinkingStep{
		Type:  StepChunks,
		Label: "目标片段",
		Data:  chunksDataFrom(items, sources),
	})

	// 5. 综合生成
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: e.templates.system},
		{Role: llm.RoleUser, Content: contextText + "\n\n用户问题：" + question + "\n\n请综合以上资料，全面回答该问题。"},
	}
	answer, err := e.llm.Generate(ctx, messages)
	if err != nil {
		return nil, false, fmt.Errorf("综合生成失败: %w", err)
	}

	e.appendHistory(sessionID, llm.RoleUser, question, "")
	e.appendHistory(sessionID, llm.RoleAssistant, answer, marshalSources(sources))
	return withThinking(&o, &RAGResult{Answer: answer, Sources: sources}), true, nil
}

// tryStepBack 回退查询流程：判定需要 → 回退问题检索 + 原问题检索 → 合并上下文生成。
func (e *RAGEngine) tryStepBack(ctx context.Context, sessionID string, question string, o AskOptions) (*RAGResult, bool, error) {
	start := time.Now()
	sink := o.Sink // 思考链路采集器（由 Ask/StreamAsk 预先设置）
	// 1. 判定
	sb, err := e.judgeStepBack(ctx, question)
	if err != nil {
		slog.Warn("回退判定失败，降级常规路径", "err", err)
		return nil, false, err
	}
	if !sb.ShouldStepBack || sb.StepBackQuery == "" {
		slog.Info("回退判定：不需要回退，走常规路径")
		return nil, false, nil
	}
	slog.Info("回退判定：需要回退", "回退问题", sb.StepBackQuery, "耗时ms", time.Since(start).Milliseconds())
	// 思考链路：回退问题（F7）
	recordStep(sink, ThinkingStep{
		Type:  StepStepBack,
		Label: "回退查询",
		Data:  StepBackData{StepBackQuery: sb.StepBackQuery},
	})

	// 2. 回退问题检索 + 原问题检索
	backChunks, err := e.searchSubQuery(ctx, sb.StepBackQuery, o.KBID)
	if err != nil {
		slog.Warn("回退问题检索失败，降级常规路径", "err", err)
		return nil, false, err
	}
	origChunks, err := e.searchSubQuery(ctx, question, o.KBID)
	if err != nil {
		slog.Warn("原问题检索失败，降级常规路径", "err", err)
		return nil, false, err
	}
	// 思考链路：回退/原问题两次检索（F7）
	recordStep(sink, ThinkingStep{
		Type:  StepRetrieval,
		Label: "回退问题检索",
		Data:  RetrievalData{Query: sb.StepBackQuery, Method: "sub_query", Recalled: len(backChunks)},
	})
	recordStep(sink, ThinkingStep{
		Type:  StepRetrieval,
		Label: "原问题检索",
		Data:  RetrievalData{Query: question, Method: "sub_query", Recalled: len(origChunks)},
	})

	allChunks := append(backChunks, origChunks...)
	if len(allChunks) == 0 {
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), true, nil
	}
	// 合并后整体重排一次（F4/AC4；rerank 关闭或失败时降级返回原结果）
	allChunks, _ = e.retriever.Rerank(ctx, question, allChunks, e.cfg.MaxChunks, traceSinkForRequest(sink, question))
	if len(allChunks) == 0 {
		return withThinking(&o, &RAGResult{Answer: noAnswerText}), true, nil
	}

	items, sources := buildContext(allChunks, e.cfg.MaxContextTokens, e.cfg.MaxChunks)
	contextText, err := renderContext(items, e.templates.context)
	if err != nil {
		return nil, false, fmt.Errorf("渲染上下文失败: %w", err)
	}
	slog.Info("回退综合", "上下文token", estimateTokens(contextText), "引用数", len(sources), "耗时ms", time.Since(start).Milliseconds())
	// 思考链路：目标 chunks（F6）
	recordStep(sink, ThinkingStep{
		Type:  StepChunks,
		Label: "目标片段",
		Data:  chunksDataFrom(items, sources),
	})

	// 3. 生成
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: e.templates.system},
		{Role: llm.RoleUser, Content: contextText + "\n\n用户问题：" + question},
	}
	answer, err := e.llm.Generate(ctx, messages)
	if err != nil {
		return nil, false, fmt.Errorf("回退生成失败: %w", err)
	}

	e.appendHistory(sessionID, llm.RoleUser, question, "")
	e.appendHistory(sessionID, llm.RoleAssistant, answer, marshalSources(sources))
	return withThinking(&o, &RAGResult{Answer: answer, Sources: sources}), true, nil
}
