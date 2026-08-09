package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
)

func TestBochaSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization 头错误: %q", r.Header.Get("Authorization"))
		}
		var req bochaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("解析请求失败: %v", err)
		}
		if req.Query != "RAG 召回" || !req.Summary || req.Count != 5 || req.Freshness != "noLimit" {
			t.Errorf("请求体错误: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		// 博查真实结构：data.webPages.value（数组）
		fmt.Fprint(w, `{"code":200,"msg":null,"data":{"webPages":{"value":[
			{"name":"RAG召回概述","url":"https://example.com/1","snippet":"短摘要","summary":"完整摘要1","siteName":"示例站"},
			{"name":"检索增强生成","url":"https://example.com/2","snippet":"摘要2","summary":"","siteName":""}
		]}}}`)
	}))
	defer server.Close()

	p := NewBochaProvider(config.WebSearchConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Count:   5,
		Timeout: 10,
	})
	if !p.Available() {
		t.Errorf("配置 api_key 后 Available() 应为 true")
	}

	results, err := p.Search(context.Background(), "RAG 召回", Options{})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	r := results[0]
	// summary 优先作为摘要；siteName 映射站点名；博查无 content
	if r.Title != "RAG召回概述" || r.URL != "https://example.com/1" || r.Snippet != "完整摘要1" || r.Site != "示例站" {
		t.Errorf("结果解析错误: %+v", r)
	}
	if results[1].Snippet != "摘要2" {
		t.Errorf("summary 为空时应回退 snippet: %+v", results[1])
	}
	if results[1].Site != "" {
		t.Errorf("空 siteName 应为空串: %+v", results[1])
	}
}

func TestBochaSearchNoAPIKey(t *testing.T) {
	p := NewBochaProvider(config.WebSearchConfig{Provider: "bocha"})
	if p.Available() {
		t.Errorf("未配置 api_key 时 Available() 应为 false")
	}
	if _, err := p.Search(context.Background(), "q", Options{}); err == nil {
		t.Errorf("未配置 api_key 应返回错误")
	}
}

func TestBochaSearchHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	p := NewBochaProvider(config.WebSearchConfig{BaseURL: server.URL, APIKey: "k", Timeout: 10})
	if _, err := p.Search(context.Background(), "q", Options{}); err == nil {
		t.Errorf("HTTP 400 应返回错误")
	}
}

func TestNewFactory(t *testing.T) {
	// 空/bocha → bochaProvider
	if _, ok := New(config.WebSearchConfig{}).(*bochaProvider); !ok {
		t.Errorf("provider 空应创建 bochaProvider")
	}
	if _, ok := New(config.WebSearchConfig{Provider: "BOCHA"}).(*bochaProvider); !ok {
		t.Errorf("provider=BOCHA（大小写不敏感）应创建 bochaProvider")
	}
	// 未知 → unavailableProvider（占位不可用）
	p := New(config.WebSearchConfig{Provider: "unknown"})
	if p.Available() {
		t.Errorf("未知提供者应不可用")
	}
	if _, err := p.Search(context.Background(), "q", Options{}); err == nil {
		t.Errorf("未知提供者 Search 应返回错误")
	}
}

func TestBochaOptionsOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req bochaRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Count != 10 || req.Freshness != "week" {
			t.Errorf("Options 未覆盖: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"webPages":{"value":[]}}}`)
	}))
	defer server.Close()

	p := NewBochaProvider(config.WebSearchConfig{BaseURL: server.URL, APIKey: "k", Count: 5, Timeout: 10})
	if _, err := p.Search(context.Background(), "q", Options{Count: 10, Freshness: "week"}); err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
}
