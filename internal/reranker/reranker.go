package reranker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"golang.org/x/time/rate"
)

// RerankCandidate 重排序候选项
type RerankCandidate struct {
	ID       string
	Content  string
	Score    float32
	Metadata map[string]any
}

// RerankResult 重排序结果
type RerankResult struct {
	ID       string
	Content  string
	Score    float32
	Metadata map[string]any
}

// Reranker 重排序接口
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error)
}

type apiReranker struct {
	config  config.RerankerConfig
	client  *http.Client
	limiter *rate.Limiter
}

// newLimiter 创建 QPS 限流器；qps<=0（配置缺省）时使用默认值 10，
// 避免 rate.NewLimiter(0,0) 导致 Wait 无限阻塞
func newLimiter(qps int) *rate.Limiter {
	if qps <= 0 {
		qps = 10
	}
	return rate.NewLimiter(rate.Limit(qps), qps)
}

// NewReranker 创建 Reranker
// mode=api（默认）：调用 /v1/rerank 专用重排接口（Jina/Cohere/vLLM 等）
// mode=llm / ollama：使用通用大模型 chat/completions 打分重排（无专用 rerank 端点的场景）
func NewReranker(cfg config.RerankerConfig) Reranker {
	switch strings.ToLower(cfg.Mode) {
	case "llm", "ollama":
		return NewLLMReranker(cfg)
	case "", "api":
		// 默认 api 模式
	default:
		slog.Warn("未知重排模式，回退 api 模式", "mode", cfg.Mode)
	}
	return &apiReranker{
		config:  cfg,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: newLimiter(cfg.QPS),
	}
}

func (r *apiReranker) Rerank(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// topN 未显式指定时回退到配置值（接线 reranker.top_n）
	if topN <= 0 {
		topN = r.config.TopN
	}

	if err := r.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("限流等待失败: %w", err)
	}

	return r.doRerank(ctx, query, candidates, topN)
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func (r *apiReranker) doRerank(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error) {
	var lastErr error

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		results, err := r.doRequest(ctx, query, candidates, topN)
		if err == nil {
			return results, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", r.config.MaxRetries, lastErr)
}

func (r *apiReranker) doRequest(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error) {
	documents := make([]string, len(candidates))
	for i, c := range candidates {
		documents[i] = c.Content
	}

	reqBody := rerankRequest{
		Model:     r.config.Model,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(r.config.BaseURL, "/") + "/v1/rerank"
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if r.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.config.APIKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, &retryableError{cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, &retryableError{
			cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Reranker API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var rrResp rerankResponse
	if err := json.Unmarshal(respBody, &rrResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	sort.Slice(rrResp.Results, func(i, j int) bool {
		return rrResp.Results[i].RelevanceScore > rrResp.Results[j].RelevanceScore
	})

	results := make([]RerankResult, 0, len(rrResp.Results))
	for _, rr := range rrResp.Results {
		if rr.Index < len(candidates) {
			c := candidates[rr.Index]
			results = append(results, RerankResult{
				ID:       c.ID,
				Content:  c.Content,
				Score:    float32(rr.RelevanceScore),
				Metadata: c.Metadata,
			})
		}
	}

	return results, nil
}

type retryableError struct {
	cause error
}

func (e *retryableError) Error() string {
	return e.cause.Error()
}

func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}
