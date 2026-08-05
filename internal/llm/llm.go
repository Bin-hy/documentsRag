// Package llm 提供统一的 LLM 客户端，支持普通生成与流式生成。
// 使用 OpenAI 兼容接口（/v1/chat/completions），兼容 GPT、豆包、DeepSeek、vLLM 等后端。
package llm

import (
	"bytes"
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

// 消息角色
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message 对话消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk 流式增量片段
type StreamChunk struct {
	Content string // 增量文本（不含前一片段内容）
	Done    bool   // 最后一个片段标记
	Err     error  // 流中段出错时设置（Err != nil 即终止）
}

// ChatOptions 生成选项（未指定的字段使用配置默认值）
type ChatOptions struct {
	Model       string
	Temperature *float32 // nil 使用配置默认温度
	MaxTokens   int
}

// ChatOption 函数式生成选项
type ChatOption func(*ChatOptions)

// WithModel 覆盖模型名称
func WithModel(model string) ChatOption {
	return func(o *ChatOptions) { o.Model = model }
}

// WithTemperature 覆盖生成温度
func WithTemperature(t float32) ChatOption {
	return func(o *ChatOptions) { o.Temperature = &t }
}

// WithMaxTokens 覆盖最大生成 token 数
func WithMaxTokens(n int) ChatOption {
	return func(o *ChatOptions) { o.MaxTokens = n }
}

// LLM 统一生成接口（OpenAI 兼容）
type LLM interface {
	Generate(ctx context.Context, messages []Message, opts ...ChatOption) (string, error)
	StreamGenerate(ctx context.Context, messages []Message, opts ...ChatOption) (<-chan StreamChunk, error)
}

type openaiLLM struct {
	config  config.LLMConfig
	client  *http.Client
	limiter *rate.Limiter
}

// NewLLM 创建 LLM 客户端
func NewLLM(cfg config.LLMConfig) LLM {
	return &openaiLLM{
		config: cfg,
		client: &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second},
		limiter: rate.NewLimiter(rate.Limit(cfg.QPS), cfg.QPS),
	}
}

// Generate 普通生成，返回完整文本
func (l *openaiLLM) Generate(ctx context.Context, messages []Message, opts ...ChatOption) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= l.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		// 每次请求尝试前限流（与流式路径一致）
		if err := l.limiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("限流等待失败: %w", err)
		}

		content, err := l.doGenerate(ctx, messages, opts)
		if err == nil {
			return content, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return "", err
		}
	}

	return "", fmt.Errorf("重试 %d 次后仍失败: %w", l.config.MaxRetries, lastErr)
}

func (l *openaiLLM) doGenerate(ctx context.Context, messages []Message, opts []ChatOption) (string, error) {
	body, err := l.buildRequestBody(messages, opts, false)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(l.config.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	l.setHeaders(req)

	resp, err := l.client.Do(req)
	if err != nil {
		return "", &RetryableError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return "", &RetryableError{
			Cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM API 返回空 choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// buildRequestBody 构造 chat/completions 请求体
func (l *openaiLLM) buildRequestBody(messages []Message, opts []ChatOption, stream bool) ([]byte, error) {
	o := ChatOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	model := o.Model
	if model == "" {
		model = l.config.Model
	}

	temperature := l.config.Temperature
	if o.Temperature != nil {
		temperature = *o.Temperature
	}

	maxTokens := o.MaxTokens
	if maxTokens <= 0 {
		maxTokens = l.config.MaxTokens
	}

	reqBody := chatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      stream,
	}

	return json.Marshal(reqBody)
}

func (l *openaiLLM) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if l.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.config.APIKey)
	}
}

// chatCompletionRequest OpenAI 兼容请求体
type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream"`
}

// chatCompletionResponse OpenAI 兼容响应体
type chatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
		Delta   struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
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
