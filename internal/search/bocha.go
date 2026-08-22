package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"golang.org/x/time/rate"
)

// defaultBochaBaseURL 博查 AI 搜索官方端点
const defaultBochaBaseURL = "https://api.bochaai.com/v1/web-search"

// bochaProvider 博查 AI 搜索实现（第一版 Web Search 后端）
type bochaProvider struct {
	baseURL string
	apiKey  string
	count   int
	client  *http.Client
	limiter *rate.Limiter // QPS 限流（接线 web_search.qps）
}

// NewBochaProvider 创建博查搜索提供者
func NewBochaProvider(cfg config.WebSearchConfig) Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBochaBaseURL
	}
	count := cfg.Count
	if count <= 0 {
		count = 5
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	qps := cfg.QPS
	if qps <= 0 {
		qps = 1 // 兜底防 QPS=0 时限流器永久阻塞
	}
	return &bochaProvider{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		count:   count,
		client:  &http.Client{Timeout: time.Duration(timeout) * time.Second},
		limiter: rate.NewLimiter(rate.Limit(qps), qps),
	}
}

func (p *bochaProvider) Name() string    { return "bocha" }
func (p *bochaProvider) Available() bool { return p.apiKey != "" }

type bochaRequest struct {
	Query     string `json:"query"`
	Summary   bool   `json:"summary"`
	Count     int    `json:"count"`
	Freshness string `json:"freshness"`
}

type bochaResponse struct {
	Code int `json:"code"`
	Msg  any `json:"msg"`
	Data struct {
		// 博查实际返回：data.webPages.value（数组）；字段无 content，正文需另行抓取
		WebPages struct {
			Value []struct {
				Name     string `json:"name"`
				URL      string `json:"url"`
				Snippet  string `json:"snippet"`
				Summary  string `json:"summary"`
				SiteName string `json:"siteName"`
			} `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

// Search 调用博查 /v1/web-search 接口并解析结果
func (p *bochaProvider) Search(ctx context.Context, query string, opts Options) ([]Result, error) {
	if !p.Available() {
		return nil, fmt.Errorf("博查搜索未配置 api_key")
	}

	if err := p.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("限流等待失败: %w", err)
	}

	count := opts.Count
	if count <= 0 {
		count = p.count
	}
	freshness := opts.Freshness
	if freshness == "" {
		freshness = "noLimit"
	}

	reqBody := bochaRequest{Query: query, Summary: true, Count: count, Freshness: freshness}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("博查 API 错误 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var br bochaResponse
	if err := json.Unmarshal(respBody, &br); err != nil {
		return nil, fmt.Errorf("解析博查响应失败: %w", err)
	}
	if br.Code != 0 && br.Code != 200 {
		return nil, fmt.Errorf("博查 API 业务错误 code=%d msg=%v", br.Code, br.Msg)
	}

	pages := br.Data.WebPages.Value
	results := make([]Result, 0, len(pages))
	for _, wp := range pages {
		snippet := wp.Summary
		if snippet == "" {
			snippet = wp.Snippet
		}
		results = append(results, Result{
			Title:   wp.Name,
			URL:     wp.URL,
			Snippet: snippet,
			Site:    wp.SiteName,
			// 博查不返回正文 content；如需正文可后续加模式 B 自主抓取
		})
	}
	return results, nil
}
