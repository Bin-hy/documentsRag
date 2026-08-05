package reranker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
)

func TestRerankNormal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rerankRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := rerankResponse{
			Results: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 1, RelevanceScore: 0.95},
				{Index: 0, RelevanceScore: 0.80},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		TopN:       2,
		MaxRetries: 3,
		QPS:        100,
	}

	rk := NewReranker(cfg)
	candidates := []RerankCandidate{
		{ID: "doc1", Content: "第一篇文档", Score: 0.8},
		{ID: "doc2", Content: "第二篇文档", Score: 0.7},
	}

	results, err := rk.Rerank(context.Background(), "查询", candidates, 2)
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(results))
	}

	// 按 relevance_score 排序，doc2(0.95) 应排在 doc1(0.80) 前面
	if results[0].ID != "doc2" {
		t.Errorf("期望第一个结果为 doc2，实际 %s", results[0].ID)
	}
	if results[0].Score != 0.95 {
		t.Errorf("期望分数 0.95，实际 %f", results[0].Score)
	}
}

func TestRerankRetry(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 2 {
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}

		resp := rerankResponse{
			Results: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.9},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		TopN:       1,
		MaxRetries: 3,
		QPS:        100,
	}

	rk := NewReranker(cfg)
	candidates := []RerankCandidate{
		{ID: "doc1", Content: "内容"},
	}

	results, err := rk.Rerank(context.Background(), "查询", candidates, 1)
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}

	if attempt != 3 {
		t.Errorf("期望 3 次请求（2次429+1次成功），实际 %d 次", attempt)
	}
}

func TestRerankTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		TopN:       1,
		MaxRetries: 0,
		QPS:        100,
	}

	rk := NewReranker(cfg)
	candidates := []RerankCandidate{
		{ID: "doc1", Content: "内容"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := rk.Rerank(ctx, "查询", candidates, 1)
	if err == nil {
		t.Fatal("超时后应返回错误")
	}
}
