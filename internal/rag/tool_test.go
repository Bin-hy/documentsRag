package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/search"
)

// stubSearchProvider 测试用搜索 Provider（独立于 engine_enhanced_test.go 的 fakeSearchProvider）
type stubSearchProvider struct {
	results []search.Result
	err     error
}

func (s *stubSearchProvider) Name() string    { return "stub-search" }
func (s *stubSearchProvider) Available() bool { return true }
func (s *stubSearchProvider) Search(ctx context.Context, query string, opts search.Options) ([]search.Result, error) {
	return s.results, s.err
}

// webSearchTool 结构化执行：文本结果 + 思考链条目（标题/链接/摘要）
func TestWebSearchToolExecuteStructured(t *testing.T) {
	tool := NewWebSearchTool(&stubSearchProvider{results: []search.Result{
		{Title: "量子纠缠", URL: "https://example.com/1", Snippet: "量子纠缠是量子力学现象，粒子相互作用后无法单独描述。"},
		{Title: "量子通信", URL: "https://example.com/2", Snippet: "量子纠缠在量子通信中的应用。"},
	}})

	st, ok := tool.(StructuredTool)
	if !ok {
		t.Fatal("webSearchTool 应实现 StructuredTool")
	}

	text, items, err := st.ExecuteStructured(context.Background(), `{"query":"量子纠缠"}`)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	// 文本结果含标题/链接/摘要（回传 LLM 用）
	if !strings.Contains(text, "[1] 量子纠缠") || !strings.Contains(text, "https://example.com/1") ||
		!strings.Contains(text, "量子纠缠是量子力学现象") {
		t.Errorf("文本结果内容不完整: %q", text)
	}
	// 结构化条目完整（思考链展示用）
	if len(items) != 2 {
		t.Fatalf("条目数 = %d, want 2", len(items))
	}
	if items[0].Title != "量子纠缠" || items[0].URL != "https://example.com/1" ||
		items[0].Snippet != "量子纠缠是量子力学现象，粒子相互作用后无法单独描述。" {
		t.Errorf("条目[0]错误: %+v", items[0])
	}
}

// webSearchTool 参数缺失应报错
func TestWebSearchToolMissingQuery(t *testing.T) {
	tool := NewWebSearchTool(&stubSearchProvider{})
	_, err := tool.Execute(context.Background(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Errorf("缺少 query 应报错: %v", err)
	}
}

// webSearchTool 无结果时返回明确提示且 items 为空
func TestWebSearchToolNoResults(t *testing.T) {
	tool := NewWebSearchTool(&stubSearchProvider{results: nil})
	text, items, err := tool.(StructuredTool).ExecuteStructured(context.Background(), `{"query":"不存在的内容"}`)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !strings.Contains(text, "未搜索到") {
		t.Errorf("无结果提示错误: %q", text)
	}
	if len(items) != 0 {
		t.Errorf("无结果时 items 应为空: %+v", items)
	}
}
