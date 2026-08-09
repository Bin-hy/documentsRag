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
	RoleTool      = "tool" // function calling 工具结果消息
)

// ToolCall 模型请求的工具调用（assistant 消息携带）
type ToolCall struct {
	ID        string `json:"id"`        // 工具调用 ID（tool 结果消息回引用）
	Name      string `json:"name"`      // 工具名
	Arguments string `json:"arguments"` // 参数 JSON 字符串
}

// Message 对话消息
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Sources    string     `json:"sources,omitempty"`      // 引用来源 JSON 数组字符串（历史持久化用，空 = 无引用）
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant 消息的工具调用列表
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool 结果消息回引的工具调用 ID
}

// FunctionTool OpenAI 风格 function tool 定义（tools 请求参数）
type FunctionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// toOpenAIFormat 转为 OpenAI tools 请求格式
func (f FunctionTool) toOpenAIFormat() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        f.Name,
			"description": f.Description,
			"parameters":  f.Parameters,
		},
	}
}

// StreamChunk 流式增量片段
type StreamChunk struct {
	Content   string     // 增量文本（不含前一片段内容）
	ToolCalls []ToolCall // 流式聚合后的工具调用（仅在流结束的 Done 片段携带，空 = 无）
	Done      bool       // 最后一个片段标记
	Err       error      // 流中段出错时设置（Err != nil 即终止）
}

// ChatOptions 生成选项（未指定的字段使用配置默认值）
type ChatOptions struct {
	Model       string
	Temperature *float32 // nil 使用配置默认温度
	MaxTokens   int
	Tools       []FunctionTool // function calling 工具定义（空 = 不启用工具）
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

// WithTools 注入 function calling 工具定义
func WithTools(tools []FunctionTool) ChatOption {
	return func(o *ChatOptions) { o.Tools = tools }
}

// LLM 统一生成接口（OpenAI 兼容）
type LLM interface {
	Generate(ctx context.Context, messages []Message, opts ...ChatOption) (string, error)
	StreamGenerate(ctx context.Context, messages []Message, opts ...ChatOption) (<-chan StreamChunk, error)
	// GenerateTool 带工具定义的生成：返回正文与模型请求的工具调用（无工具调用时 ToolCalls 为空）
	GenerateTool(ctx context.Context, messages []Message, tools []FunctionTool, opts ...ChatOption) (*ToolResponse, error)
}

// ToolResponse 带工具调用的生成结果
type ToolResponse struct {
	Content   string
	ToolCalls []ToolCall // 模型请求的工具调用（空 = 未请求工具，Content 为最终回答）
}

type openaiLLM struct {
	config  config.LLMConfig
	client  *http.Client
	limiter *rate.Limiter
}

// NewLLM 创建 LLM 客户端
func NewLLM(cfg config.LLMConfig) LLM {
	return &openaiLLM{
		config:  cfg,
		client:  &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second},
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

// GenerateTool 带工具定义的生成：返回正文与模型请求的工具调用（含重试与限流）
func (l *openaiLLM) GenerateTool(ctx context.Context, messages []Message, tools []FunctionTool, opts ...ChatOption) (*ToolResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= l.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		if err := l.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("限流等待失败: %w", err)
		}

		resp, err := l.doGenerateTool(ctx, messages, tools, opts)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", l.config.MaxRetries, lastErr)
}

// doGenerateTool 执行一次带工具定义的 chat/completions 请求并解析 tool_calls
func (l *openaiLLM) doGenerateTool(ctx context.Context, messages []Message, tools []FunctionTool, opts []ChatOption) (*ToolResponse, error) {
	allOpts := append([]ChatOption(nil), opts...)
	allOpts = append(allOpts, WithTools(tools))

	body, err := l.buildRequestBody(messages, allOpts, false)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(l.config.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	l.setHeaders(req)

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, &RetryableError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, &RetryableError{Cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LLM API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM API 返回空 choices")
	}

	msg := chatResp.Choices[0].Message
	out := &ToolResponse{Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
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
		Messages:    toOpenAIMessages(messages),
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      stream,
	}

	if len(o.Tools) > 0 {
		tools := make([]map[string]any, 0, len(o.Tools))
		for _, t := range o.Tools {
			tools = append(tools, t.toOpenAIFormat())
		}
		reqBody.Tools = tools
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
	Model       string           `json:"model"`
	Messages    []map[string]any `json:"messages"` // OpenAI 协议消息（toOpenAIMessages 转换）
	Temperature float32          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	Stream      bool             `json:"stream"`
	Tools       []map[string]any `json:"tools,omitempty"` // OpenAI function tools（空 = 不传）
}

// toOpenAIMessages 把内部 Message 转为 OpenAI 协议消息：
//   - assistant 的 ToolCalls 转为嵌套格式 [{id, type:"function", function:{name, arguments}}]
//   - role=tool 消息携带 tool_call_id 回引
//   - 内部字段（Sources 引用 JSON）不下发
func toOpenAIMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		om := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}
			om["tool_calls"] = tcs
		}
		if m.ToolCallID != "" {
			om["tool_call_id"] = m.ToolCallID
		}
		out = append(out, om)
	}
	return out
}

// chatToolCall 响应中的工具调用（function 嵌套结构）
type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatCompletionResponse OpenAI 兼容响应体（含 message 与流式 delta 的工具调用）
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content   string             `json:"content"`
			ToolCalls []chatToolCallPart `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// chatToolCallPart 流式 delta 中的工具调用分片（按 index 聚合）
type chatToolCallPart struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
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
