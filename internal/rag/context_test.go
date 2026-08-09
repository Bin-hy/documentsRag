package rag

import (
	"testing"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// 构造测试用检索结果
func testChunk(id string, content string, filename string, heading string) retriever.RetrieveResult {
	return retriever.RetrieveResult{
		ID:      id,
		Content: content,
		Score:   0.9,
		Metadata: map[string]any{
			"filename":        filename,
			"heading_context": heading,
		},
	}
}

// AC8: maxChunks 生效
func TestBuildContext_MaxChunks(t *testing.T) {
	chunks := []retriever.RetrieveResult{
		testChunk("1", "内容一", "a.md", "标题A"),
		testChunk("2", "内容二", "a.md", "标题A"),
		testChunk("3", "内容三", "b.md", "标题B"),
	}

	items, sources := buildContext(chunks, 100000, 2)
	if len(items) != 2 || len(sources) != 2 {
		t.Fatalf("maxChunks 未生效: items=%d sources=%d", len(items), len(sources))
	}
	if items[0].Index != 1 || items[1].Index != 2 {
		t.Errorf("编号错误: %+v", items)
	}
}

// AC8: token 预算截断
func TestBuildContext_TokenBudget(t *testing.T) {
	chunks := []retriever.RetrieveResult{
		testChunk("1", "这是一个比较长的中文内容片段用于测试预算", "a.md", ""),
		testChunk("2", "第二段", "b.md", ""),
		testChunk("3", "第三段", "c.md", ""),
	}

	// 预算只够放第一条（第一条带来源标注约 54 token，加上第二条约 74 超预算）
	items, _ := buildContext(chunks, 60, 10)
	if len(items) != 1 {
		t.Fatalf("预算截断未生效: got %d want 1", len(items))
	}
	if items[0].Content != chunks[0].Content {
		t.Errorf("第一条内容错误: %+v", items[0])
	}
}

// AC6 辅助: 引用来源元数据提取正确
func TestBuildContext_SourceExtraction(t *testing.T) {
	chunks := []retriever.RetrieveResult{
		testChunk("id-1", "内容", "report.pdf", "第二章"),
	}

	_, sources := buildContext(chunks, 100000, 10)
	if len(sources) != 1 {
		t.Fatalf("sources 数量错误: %d", len(sources))
	}
	s := sources[0]
	if s.ID != "id-1" || s.Filename != "report.pdf" || s.Heading != "第二章" || s.Score != 0.9 {
		t.Errorf("Source 提取错误: %+v", s)
	}
}

// N5: 元数据缺失时 Source 字段为空串不 panic
func TestBuildContext_MissingMetadata(t *testing.T) {
	chunks := []retriever.RetrieveResult{
		{ID: "x", Content: "无元数据", Score: 0.5},
	}

	items, sources := buildContext(chunks, 100000, 10)
	if len(items) != 1 || len(sources) != 1 {
		t.Fatalf("缺失元数据场景失败: items=%d sources=%d", len(items), len(sources))
	}
	if sources[0].Filename != "" || sources[0].Heading != "" {
		t.Errorf("空元数据应输出空串: %+v", sources[0])
	}
}

// 估算函数基础行为
func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 0 {
		t.Error("空串应为 0")
	}
	// 中文每个 2 token
	if estimateTokens("中文") != 4 {
		t.Errorf("中文估算错误: %d", estimateTokens("中文"))
	}
	// 英文按词
	if estimateTokens("hello world") != 2 {
		t.Errorf("英文估算错误: %d", estimateTokens("hello world"))
	}
}

// F8: 引用来源携带 source_type（来自 Metadata["source_type"]）
func TestBuildContext_SourceType(t *testing.T) {
	chunks := []retriever.RetrieveResult{
		{ID: "v1", Content: "向量内容", Score: 0.9, Metadata: map[string]any{"source_type": "vector_store"}},
		{ID: "w1", Content: "web内容", Score: 0.8, Metadata: map[string]any{"source_type": "web_search"}},
		{ID: "n1", Content: "无来源标记", Score: 0.7},
	}

	_, sources := buildContext(chunks, 100000, 10)
	if len(sources) != 3 {
		t.Fatalf("sources 数量 = %d, want 3", len(sources))
	}
	if sources[0].SourceType != "vector_store" {
		t.Errorf("sources[0].SourceType = %q, want vector_store", sources[0].SourceType)
	}
	if sources[1].SourceType != "web_search" {
		t.Errorf("sources[1].SourceType = %q, want web_search", sources[1].SourceType)
	}
	if sources[2].SourceType != "" {
		t.Errorf("无标记时 SourceType = %q, want 空", sources[2].SourceType)
	}
}
