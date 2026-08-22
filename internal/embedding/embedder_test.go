package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
)

func TestEmbedNormal(t *testing.T) {
	dim := 4
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := embeddingResponse{}
		for i := range req.Input {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i+1) * 0.1
			}
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: vec, Index: i})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	emb, err := NewEmbedder(config.EmbedderConfig{
		BaseURL:    server.URL,
		Model:      "test-model",
		Dimension:  dim,
		BatchSize:  100,
		MaxRetries: 3,
		QPS:        100,
	})
	if err != nil {
		t.Fatalf("NewEmbedder 失败: %v", err)
	}

	texts := make([]string, 10)
	for i := range texts {
		texts[i] = "test text"
	}

	vectors, err := emb.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed 失败: %v", err)
	}

	if len(vectors) != 10 {
		t.Fatalf("期望 10 个向量，实际 %d", len(vectors))
	}

	for i, v := range vectors {
		if len(v) != dim {
			t.Errorf("向量[%d] 维度 %d，期望 %d", i, len(v), dim)
		}
	}
}

func TestEmbedBatchSplit(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := embeddingResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float32{0.1, 0.2}, Index: i})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	emb, err := NewEmbedder(config.EmbedderConfig{
		BaseURL:    server.URL,
		Model:      "test",
		BatchSize:  20,
		MaxRetries: 3,
		QPS:        100,
	})
	if err != nil {
		t.Fatalf("NewEmbedder 失败: %v", err)
	}

	texts := make([]string, 100)
	for i := range texts {
		texts[i] = "text"
	}

	_, err = emb.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed 失败: %v", err)
	}

	if requestCount.Load() != 5 {
		t.Errorf("期望 5 次请求（100/20），实际 %d", requestCount.Load())
	}
}

func TestEmbedRetry(t *testing.T) {
	var attempt atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempt.Add(1)
		if n <= 2 {
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}

		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := embeddingResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: []float32{0.1}, Index: i})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	emb, err := NewEmbedder(config.EmbedderConfig{
		BaseURL:    server.URL,
		Model:      "test",
		BatchSize:  100,
		MaxRetries: 3,
		QPS:        100,
	})
	if err != nil {
		t.Fatalf("NewEmbedder 失败: %v", err)
	}

	vectors, err := emb.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("重试后应成功，实际错误: %v", err)
	}

	if len(vectors) != 1 {
		t.Errorf("期望 1 个向量，实际 %d", len(vectors))
	}

	if attempt.Load() != 3 {
		t.Errorf("期望 3 次尝试（2 次 429 + 1 次成功），实际 %d", attempt.Load())
	}
}

func TestEmbedTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()

	emb, err := NewEmbedder(config.EmbedderConfig{
		BaseURL:    server.URL,
		Model:      "test",
		BatchSize:  100,
		MaxRetries: 0,
		QPS:        100,
	})
	if err != nil {
		t.Fatalf("NewEmbedder 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = emb.Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("超时应返回错误")
	}
}
