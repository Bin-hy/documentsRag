package rag

import (
	"testing"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// sliceSink 按 Record 顺序追加
func TestSliceSink_AppendOrder(t *testing.T) {
	s := &sliceSink{}
	s.Record(ThinkingStep{Type: StepRewrite, Label: "改写"})
	s.Record(ThinkingStep{Type: StepRetrieval, Label: "检索"})
	s.Record(ThinkingStep{Type: StepChunks, Label: "目标片段"})
	if len(s.steps) != 3 {
		t.Fatalf("期望 3 步，实际 %d", len(s.steps))
	}
	for i, want := range []ThinkingStepType{StepRewrite, StepRetrieval, StepChunks} {
		if s.steps[i].Type != want {
			t.Errorf("第 %d 步类型错误: got %q want %q", i, s.steps[i].Type, want)
		}
	}
}

// recordStep 对 nil sink 直接跳过，不 panic（N2）
func TestRecordStep_NilSink(t *testing.T) {
	recordStep(nil, ThinkingStep{Type: StepRouting})
	recordStep(nil, ThinkingStep{Type: StepChunks, Data: ChunksData{}})
}

// retriever.RetrieveTrace → RetrievalData 翻译：单路与多路（含 PerQuery）
func TestRetrievalDataFrom(t *testing.T) {
	// 单路
	single := retrievalDataFrom(retriever.RetrieveTrace{
		Query: "q1", Method: "hybrid", Recalled: 3,
	})
	if single.Query != "q1" || single.Method != "hybrid" || single.Recalled != 3 {
		t.Errorf("单路翻译错误: %+v", single)
	}
	if len(single.PerQuery) != 0 {
		t.Errorf("单路不应有 PerQuery: %+v", single.PerQuery)
	}

	// 多路（顺序与传入 queries 一致，N7）
	multi := retrievalDataFrom(retriever.RetrieveTrace{
		Query: "q0", Method: "multi_fusion", Recalled: 4,
		PerQuery: []retriever.PerQueryTrace{
			{Query: "q0", Recalled: 2},
			{Query: "v1", Recalled: 1},
			{Query: "v2", Recalled: 1},
		},
	})
	if len(multi.PerQuery) != 3 || multi.PerQuery[1].Query != "v1" || multi.PerQuery[2].Recalled != 1 {
		t.Errorf("多路翻译错误: %+v", multi.PerQuery)
	}
}

// rerank 前后对比翻译
func TestRerankDataFrom(t *testing.T) {
	d := rerankDataFrom(retriever.RetrieveTrace{
		Query: "q",
		RerankBefore: []retriever.RankedItem{
			{ID: "a", Filename: "f.md", Score: 0.9, Rank: 1},
			{ID: "b", Filename: "g.md", Score: 0.8, Rank: 2},
		},
		RerankAfter: []retriever.RankedItem{
			{ID: "b", Filename: "g.md", Score: 0.99, Rank: 1},
			{ID: "a", Filename: "f.md", Score: 0.5, Rank: 2},
		},
	})
	if len(d.Before) != 2 || len(d.After) != 2 {
		t.Fatalf("前后对比翻译错误: %+v", d)
	}
	if d.Before[0].ID != "a" || d.After[0].ID != "b" {
		t.Errorf("rerank 前后顺序未正确保留: before=%+v after=%+v", d.Before, d.After)
	}
	if d.After[0].Score != 0.99 {
		t.Errorf("rerank 分数翻译错误: %+v", d.After[0])
	}
}

// chunksDataFrom 与 sources 同集合构造（AC7）
func TestChunksDataFrom(t *testing.T) {
	items := []ContextItem{
		{Index: 1, Filename: "a.md", Heading: "标题", Content: "内容一"},
	}
	sources := []Source{
		{ID: "r1", Filename: "a.md", Heading: "标题", Score: 0.9},
	}
	d := chunksDataFrom(items, sources)
	if len(d.Chunks) != 1 {
		t.Fatalf("期望 1 条 chunk，实际 %d", len(d.Chunks))
	}
	c := d.Chunks[0]
	if c.ID != "r1" || c.Filename != "a.md" || c.Heading != "标题" || c.Score != 0.9 || c.Content != "内容一" {
		t.Errorf("chunk 构造错误: %+v", c)
	}
}

// truncateRunes 按 rune 截断，超长追加省略号，多字节字符不截断
func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("短文本", 10); got != "短文本" {
		t.Errorf("短文本不应截断: %q", got)
	}
	got := truncateRunes("一二三四五六七八九十", 5)
	if got != "一二三四五…" {
		t.Errorf("截断错误: %q", got)
	}
	if len([]rune(got)) != 6 { // 5 个字符 + 省略号
		t.Errorf("截断后 rune 数错误: %d", len([]rune(got)))
	}
}
