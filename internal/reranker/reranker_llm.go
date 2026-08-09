package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"golang.org/x/time/rate"
)

// defaultLLMRerankPrompt 内置默认打分 prompt 模板，{query}、{document} 会被替换为实际内容
const defaultLLMRerankPrompt = `你是一个文档相关性评分器。请判断下面的【查询】与【文档】内容的相关程度，只输出一个 0 到 10 之间的数字分数（10 表示高度相关，0 表示完全不相关），不要输出任何其他文字、解释或标点符号。

【查询】
{query}

【文档】
{document}

分数：`

// llmReranker 使用通用大模型（OpenAI 兼容 /v1/chat/completions 接口，如 Ollama）
// 对每个候选文档单独打分（0-10），再按分数降序重排。
// 适用于没有专用 rerank 端点（/v1/rerank）的本地小模型场景。
type llmReranker struct {
	config  config.RerankerConfig
	client  *http.Client
	limiter *rate.Limiter
	prompt  string
}

// NewLLMReranker 创建 LLM 打分重排器；cfg.LLMPromptTemplate 留空时使用内置默认模板
func NewLLMReranker(cfg config.RerankerConfig) Reranker {
	prompt := cfg.LLMPromptTemplate
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultLLMRerankPrompt
	}
	return &llmReranker{
		config:  cfg,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: newLimiter(cfg.QPS),
		prompt:  prompt,
	}
}

// chatRequest OpenAI 兼容 /v1/chat/completions 请求体
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// scorePattern 匹配 0-10 的整数或小数分数（含负数，便于后续收敛到合法区间）
var (
	// scoreAfterLabelPattern 匹配 "分数：8" 这类带标签的分数
	scoreAfterLabelPattern = regexp.MustCompile(`分数[：:]\s*(-?\d+(?:\.\d+)?)`)
	// scoreWithUnitPattern 匹配 "8分" 这类带单位结尾的分数（无负号，避免 "0-10" 中的 "-10" 被误捕获）
	scoreWithUnitPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*分`)
	// scoreTrailingPattern 匹配文本末尾的数字（避免命中 "0-10 分制" 等说明文字中的数字）
	scoreTrailingPattern = regexp.MustCompile(`(-?\d+(?:\.\d+)?)[^0-9]*$`)
)

func (r *llmReranker) Rerank(ctx context.Context, query string, candidates []RerankCandidate, topN int) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	results := make([]RerankResult, 0, len(candidates))
	for i, c := range candidates {
		if err := r.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("限流等待失败: %w", err)
		}

		prompt := strings.ReplaceAll(r.prompt, "{query}", query)
		prompt = strings.ReplaceAll(prompt, "{document}", c.Content)

		score, err := r.scoreDocument(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("文档打分失败(候选 %d): %w", i, err)
		}

		results = append(results, RerankResult{
			ID:       c.ID,
			Content:  c.Content,
			Score:    score,
			Metadata: c.Metadata,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topN > 0 && len(results) > topN {
		results = results[:topN]
	}

	return results, nil
}

// scoreDocument 对单个 prompt 打分，带重试
func (r *llmReranker) scoreDocument(ctx context.Context, prompt string) (float32, error) {
	var lastErr error

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(backoff):
			}
		}

		score, err := r.scoreOnce(ctx, prompt)
		if err == nil {
			return score, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return 0, err
		}
	}

	return 0, fmt.Errorf("重试 %d 次后仍失败: %w", r.config.MaxRetries, lastErr)
}

func (r *llmReranker) scoreOnce(ctx context.Context, prompt string) (float32, error) {
	reqBody := chatRequest{
		Model: r.config.Model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: r.config.LLMTemperature,
		MaxTokens:   32,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	url := strings.TrimRight(r.config.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	if r.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.config.APIKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, &retryableError{cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return 0, &retryableError{
			cause: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("Reranker API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return 0, fmt.Errorf("解析响应失败: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return 0, fmt.Errorf("响应缺少 choices")
	}

	return parseScore(chatResp.Choices[0].Message.Content), nil
}

// parseScore 从模型输出中提取 0-10 分数并收敛到合法区间；无法解析时返回 0
// 按优先级依次尝试："分数："标签 → "N分"（取最后一个，避免说明文字如 "10 分制" 干扰实际打分）→ 文本末尾数字
func parseScore(content string) float32 {
	if m := scoreAfterLabelPattern.FindStringSubmatch(content); len(m) > 1 && m[1] != "" {
		return clampScore(m[1])
	}

	if ms := scoreWithUnitPattern.FindAllStringSubmatch(content, -1); len(ms) > 0 {
		if last := ms[len(ms)-1]; len(last) > 1 && last[1] != "" {
			return clampScore(last[1])
		}
	}

	if m := scoreTrailingPattern.FindStringSubmatch(content); len(m) > 1 && m[1] != "" {
		return clampScore(m[1])
	}

	return 0
}

// clampScore 解析数字并收敛到 [0,10] 区间
func clampScore(s string) float32 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > 10 {
		return 10
	}
	return float32(f)
}
