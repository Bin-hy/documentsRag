package reranker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// scoreByContent 模拟 LLM 打分：含 "召回" 得 9 分，含 "架构" 得 2 分，其余 0 分
func scoreByContent(content string) float32 {
	switch {
	case strings.Contains(content, "召回"):
		return 9
	case strings.Contains(content, "架构"):
		return 2
	}
	return 0
}

func TestLLMRerankerRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("请求路径 = %s, want /v1/chat/completions", r.URL.Path)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("解析请求失败: %v", err)
		}
		if req.Model != "qwen2.5:3b" {
			t.Errorf("model = %s, want qwen2.5:3b", req.Model)
		}
		if req.Temperature != 0 {
			t.Errorf("temperature = %v, want 0", req.Temperature)
		}
		if len(req.Messages) != 1 {
			t.Fatalf("messages 长度 = %d, want 1", len(req.Messages))
		}
		// 从 prompt 中还原被替换的文档内容（模板说明文字中也含"【文档】"，需取最后一个出现位置）
		content := req.Messages[0].Content
		doc := content[strings.LastIndex(content, "【文档】")+len("【文档】"):]
		doc = strings.TrimSpace(doc)

		score := scoreByContent(doc)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"%d"}}]}`, int(score))
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		APIKey:     "ollama",
		Model:      "qwen2.5:3b",
		TopN:       2,
		MaxRetries: 3,
		QPS:        100,
		Mode:       "llm",
	}

	rk := NewReranker(cfg)
	candidates := []RerankCandidate{
		{ID: "doc1", Content: "这篇文档讲了向量检索与召回流程"},
		{ID: "doc2", Content: "这篇文档介绍系统架构"},
		{ID: "doc3", Content: "这篇文档详细说明 RAG 召回策略"},
	}

	results, err := rk.Rerank(context.Background(), "RAG 如何做召回？", candidates, 2)
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, want 2", len(results))
	}
	// top2 应为含 "召回" 的 doc1/doc3（各 9 分），doc2（2 分）不应出现
	for _, r := range results {
		if r.ID == "doc2" {
			t.Errorf("doc2 不应出现在 top2")
		}
		if r.Score != 9 {
			t.Errorf("ID=%s Score=%v, want 9", r.ID, r.Score)
		}
	}
}

func TestLLMRerankerCustomPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Messages[0].Content, "自定义指令") {
			t.Errorf("prompt 未包含自定义指令")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"8"}}]}`)
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:           server.URL,
		Model:             "test-model",
		TopN:              5,
		MaxRetries:        3,
		QPS:               100,
		Mode:              "llm",
		LLMPromptTemplate: "自定义指令 {query} {document}",
	}

	rk := NewReranker(cfg)
	results, err := rk.Rerank(context.Background(), "q", []RerankCandidate{{ID: "a", Content: "d"}}, 5)
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	if len(results) != 1 || results[0].Score != 8 {
		t.Errorf("结果 = %+v, want 1 条且 Score=8", results)
	}
}

func TestLLMRerankerHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		TopN:       5,
		MaxRetries: 1,
		QPS:        100,
		Mode:       "llm",
	}

	rk := NewReranker(cfg)
	_, err := rk.Rerank(context.Background(), "q", []RerankCandidate{{ID: "a", Content: "d"}}, 5)
	if err == nil {
		t.Fatalf("期望报错, 实际为 nil")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("错误信息 = %v, want 包含 HTTP 404", err)
	}
}

func TestLLMRerankerZeroQPSSkipsLimiter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"5"}}]}`)
	}))
	defer server.Close()

	// QPS 缺省为 0 时不应让限流器无限阻塞
	cfg := config.RerankerConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		TopN:       5,
		MaxRetries: 1,
		Mode:       "llm",
	}

	rk := NewReranker(cfg)
	results, err := rk.Rerank(context.Background(), "q", []RerankCandidate{{ID: "a", Content: "d"}}, 5)
	if err != nil {
		t.Fatalf("Rerank 失败: %v", err)
	}
	if len(results) != 1 || results[0].Score != 5 {
		t.Errorf("结果 = %+v, want 1 条且 Score=5", results)
	}
}

func TestNewRerankerMode(t *testing.T) {
	if _, ok := NewReranker(config.RerankerConfig{Mode: "llm"}).(*llmReranker); !ok {
		t.Errorf("mode=llm 应返回 *llmReranker")
	}
	if _, ok := NewReranker(config.RerankerConfig{Mode: "OLLAMA"}).(*llmReranker); !ok {
		t.Errorf("mode=OLLAMA（大小写不敏感）应返回 *llmReranker")
	}
	if _, ok := NewReranker(config.RerankerConfig{}).(*apiReranker); !ok {
		t.Errorf("mode 为空应返回 *apiReranker")
	}
	if _, ok := NewReranker(config.RerankerConfig{Mode: "api"}).(*apiReranker); !ok {
		t.Errorf("mode=api 应返回 *apiReranker")
	}
}

func TestParseScore(t *testing.T) {
	cases := []struct {
		input string
		want  float32
	}{
		{"8", 8},
		{"9分", 9},
		{"7.5", 7.5},
		{"分数：6", 6},
		{"得分: 9.5", 9.5},
		{"10", 10},
		{"11", 10},              // 超上限收敛
		{"-1", 0},               // 负数收敛
		{"在 0-10 分制中我给 8 分", 8}, // 说明文字中的 0/10 不应被误取
		{"我给 7 分，满分 10", 7},     // 应取实际打的 7 而非末尾的 10
		{"无数字", 0},              // 无法解析
		{"", 0},
	}
	for _, c := range cases {
		if got := parseScore(c.input); got != c.want {
			t.Errorf("parseScore(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}
