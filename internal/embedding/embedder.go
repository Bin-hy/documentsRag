package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"golang.org/x/time/rate"
)

// Embedder 向量生成接口
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type openaiEmbedder struct {
	config  config.EmbedderConfig
	client  *http.Client
	limiter *rate.Limiter
}

// NewEmbedder 创建 Embedder（按 provider 分发，当前仅支持 OpenAI 兼容接口）
func NewEmbedder(cfg config.EmbedderConfig) (Embedder, error) {
	switch strings.ToLower(cfg.Provider) {
	case "", "openai":
		// 默认 / openai 兼容实现
	default:
		return nil, fmt.Errorf("未知 embedding provider: %s", cfg.Provider)
	}
	return &openaiEmbedder{
		config:  cfg,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(cfg.QPS), cfg.QPS),
	}, nil
}

func (e *openaiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var allVectors [][]float32

	for i := 0; i < len(texts); i += e.config.BatchSize {
		end := i + e.config.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		if err := e.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("限流等待失败: %w", err)
		}

		vectors, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("第 %d-%d 条 Embedding 失败: %w", i, end, err)
		}

		allVectors = append(allVectors, vectors...)
	}

	return allVectors, nil
}

func (e *openaiEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error

	for attempt := 0; attempt <= e.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		vectors, err := e.doRequest(ctx, texts)
		if err == nil {
			return vectors, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", e.config.MaxRetries, lastErr)
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (e *openaiEmbedder) doRequest(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model: e.config.Model,
		Input: texts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(e.config.BaseURL, "/") + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if e.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.config.APIKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &RetryableError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, &RetryableError{
			Cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Embedding API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	vectors := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index < len(vectors) {
			vectors[d.Index] = d.Embedding
		}
	}

	return vectors, nil
}

// RetryableError 可重试错误
type RetryableError struct {
	Cause error
}

func (e *RetryableError) Error() string {
	return e.Cause.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Cause
}

func isRetryable(err error) bool {
	_, ok := err.(*RetryableError)
	return ok
}
