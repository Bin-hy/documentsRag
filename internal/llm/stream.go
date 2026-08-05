package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamGenerate 流式生成，返回增量片段通道。
//
// 注意：调用方必须持续消费通道直至关闭，否则内部生产方会阻塞；建议用 for range 消费。
// 重试策略：收到首个 delta 之前失败可重试（限流 + 重建连接）；
// 收到首个 delta 之后失败不再重试，直接发送带 Err 的终止片段，避免重复输出。
func (l *openaiLLM) StreamGenerate(ctx context.Context, messages []Message, opts ...ChatOption) (<-chan StreamChunk, error) {
	out := make(chan StreamChunk)

	go func() {
		defer close(out)

		var lastErr error
		for attempt := 0; attempt <= l.config.MaxRetries; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(1<<(attempt-1)) * time.Second
				select {
				case <-ctx.Done():
					sendErr(ctx, out, ctx.Err())
					return
				case <-time.After(backoff):
				}
			}

			received := false
			err := l.doStream(ctx, messages, opts, out, &received)
			if err == nil {
				return
			}

			if received {
				// 已输出内容，不重试，透传错误
				sendErr(ctx, out, err)
				return
			}

			lastErr = err
			if !isRetryable(err) {
				sendErr(ctx, out, err)
				return
			}
		}

		sendErr(ctx, out, fmt.Errorf("重试 %d 次后仍失败: %w", l.config.MaxRetries, lastErr))
	}()

	return out, nil
}

// doStream 执行单次流式请求并解析 SSE 响应
func (l *openaiLLM) doStream(ctx context.Context, messages []Message, opts []ChatOption, out chan<- StreamChunk, received *bool) error {
	if err := l.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("限流等待失败: %w", err)
	}

	body, err := l.buildRequestBody(messages, opts, true)
	if err != nil {
		return err
	}

	url := strings.TrimRight(l.config.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	l.setHeaders(req)

	resp, err := l.client.Do(req)
	if err != nil {
		return &RetryableError{Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		return &RetryableError{Cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LLM API 错误 HTTP %d: %s", resp.StatusCode, string(body))
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/event-stream") {
		return fmt.Errorf("流式响应 Content-Type 异常: %s", ct)
	}

	return parseSSE(ctx, resp.Body, out, received)
}

// parseSSE 解析 text/event-stream 响应，将增量文本发到 out
func parseSSE(ctx context.Context, r io.Reader, out chan<- StreamChunk, received *bool) error {
	scanner := bufio.NewScanner(r)
	// 单个事件行可能很长，扩大缓冲区
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return sendChunk(ctx, out, StreamChunk{Done: true})
		}

		var resp chatCompletionResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return fmt.Errorf("解析 SSE 数据失败: %w", err)
		}

		if len(resp.Choices) > 0 && resp.Choices[0].Delta.Content != "" {
			*received = true
			if err := sendChunk(ctx, out, StreamChunk{Content: resp.Choices[0].Delta.Content}); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// 流正常结束但未收到 [DONE]（部分服务端行为），视为正常结束
	_ = sendChunk(ctx, out, StreamChunk{Done: true})
	return nil
}

// sendChunk 发送片段；ctx 取消时返回 ctx.Err()
func sendChunk(ctx context.Context, out chan<- StreamChunk, chunk StreamChunk) error {
	select {
	case out <- chunk:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sendErr 发送错误终止片段（调用方已取消时静默丢弃）
func sendErr(ctx context.Context, out chan<- StreamChunk, err error) {
	select {
	case out <- StreamChunk{Err: err}:
	case <-ctx.Done():
	}
}
