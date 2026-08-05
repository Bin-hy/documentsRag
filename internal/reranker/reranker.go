package reranker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// NewReranker 创建 Reranker（兼容 /v1/rerank 接口）
func NewReranker(cfg config.RerankerConfig) Reranker {
	return &apiReranker{
		config:  cfg,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(cfg.QPS), cfg.QPS),
	}
}

func (r *apiReranker) Rerank(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
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
