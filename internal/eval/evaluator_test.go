package eval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// fakeRetriever 返回固定检索结果，可配置错误
type fakeRetriever struct {
	mu      sync.Mutex
	results []retriever.RetrieveResult
	err     error
	callLog []string
}

func (f *fakeRetriever) Search(ctx context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callLog = append(f.callLog, req.Query)
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

// fakeEngine 返回固定回答与来源
type fakeEngine struct {
	answer  string
	sources []rag.Source
	err     error
}

func (f *fakeEngine) Ask(ctx context.Context, sessionID, question string, opts ...rag.AskOption) (*rag.RAGResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &rag.RAGResult{Answer: f.answer, Sources: f.sources}, nil
}

func (f *fakeEngine) StreamAsk(ctx context.Context, sessionID, question string, opts ...rag.AskOption) (<-chan rag.StreamEvent, error) {
	ch := make(chan rag.StreamEvent, 2)
	ch <- rag.StreamEvent{Type: rag.EventSources, Sources: f.sources}
	ch <- rag.StreamEvent{Type: rag.EventDone}
	close(ch)
	return ch, nil
}

// fakeLLM 返回可配置的 JSON 或错误
type fakeLLM struct {
	mu        sync.Mutex
	accuracy  string
	faithful  string
	err       error
	callCount int
}

func (f *fakeLLM) Generate(ctx context.Context, messages []llm.Message, opts ...llm.ChatOption) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.err != nil {
		return "", f.err
	}
	// 根据 system 提示词区分：accuracyPrompt 含「准确性」，faithfulnessPrompt 含「忠实度」
	if len(messages) > 0 && containsAny(messages[0].Content, "准确性") {
		return f.accuracy, nil
	}
	return f.faithful, nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (f *fakeLLM) StreamGenerate(ctx context.Context, messages []llm.Message, opts ...llm.ChatOption) (<-chan llm.StreamChunk, error) {
	return nil, nil
}

func testDataset() *Dataset {
	return &Dataset{Name: "测试集", Samples: []EvalSample{
		{Question: "Q1", Answer: "A1", ExpectedIDs: []string{"c1"}},
		{Question: "Q2", ExpectedIDs: []string{"c2", "c3"}},
	}}
}

// 检索结果：c1 命中第一条样本期望，c9 为干扰项
func testResults() []retriever.RetrieveResult {
	return []retriever.RetrieveResult{
		{ID: "c1", Content: "内容1"},
		{ID: "c9", Content: "干扰"},
	}
}

func TestRunRetrieveMode(t *testing.T) {
	rt := &fakeRetriever{results: testResults()}
	ds := testDataset()
	cfg := EvalConfig{Mode: "retrieve", KValues: []int{1, 3, 5}}

	results := runRetrieve(context.Background(), ds, cfg, rt)
	if len(results) != 2 {
		t.Fatalf("结果数错误: %d", len(results))
	}
	// 样本1 期望 c1，前1命中 → Recall@1 true
	if !results[0].Recall[1] {
		t.Errorf("样本1 Recall@1 应为 true，实际 %v", results[0].Recall)
	}
	// 样本2 期望 c2/c3，检索结果不含 → 全部 false
	if results[1].Recall[1] || results[1].Recall[3] || results[1].Recall[5] {
		t.Errorf("样本2 Recall 应全 false，实际 %v", results[1].Recall)
	}
	// LLM 不应被调用
	if results[0].Accuracy != nil || results[0].Faithful != nil {
		t.Errorf("retrieve 模式不应有 LLM 指标")
	}
}

func TestRunQAMode(t *testing.T) {
	rt := &fakeRetriever{results: testResults()}
	eng := &fakeEngine{answer: "回答", sources: []rag.Source{{Filename: "a.md"}}}
	ds := testDataset()
	cfg := EvalConfig{Mode: "qa", KValues: []int{1}}

	results := runQA(context.Background(), ds, cfg, rt, eng)
	if results[0].Answer != "回答" {
		t.Errorf("回答错误: %q", results[0].Answer)
	}
	if len(results[0].Sources) != 1 || results[0].Sources[0] != "a.md" {
		t.Errorf("来源错误: %+v", results[0].Sources)
	}
	if results[0].Accuracy != nil || results[0].Faithful != nil {
		t.Errorf("qa 模式不应有 LLM 指标")
	}
}

func TestRunFullMode(t *testing.T) {
	rt := &fakeRetriever{results: testResults()}
	eng := &fakeEngine{answer: "回答", sources: []rag.Source{{Filename: "a.md"}}}
	judge := &fakeLLM{accuracy: `{"score": 8}`, faithful: `{"faithful": true}`}
	ds := testDataset()
	cfg := EvalConfig{Mode: "full", KValues: []int{1}}

	results := runFull(context.Background(), ds, cfg, rt, eng, judge)
	if results[0].Accuracy == nil || *results[0].Accuracy != 8 {
		t.Errorf("准确性错误: %v", results[0].Accuracy)
	}
	if results[0].Faithful == nil || !*results[0].Faithful {
		t.Errorf("忠实判定错误: %v", results[0].Faithful)
	}
	// 样本2 无标准答案，只评忠实度
	if results[1].Accuracy != nil {
		t.Errorf("无标准答案的样本不应有准确性评分")
	}
	if results[1].Faithful == nil {
		t.Errorf("样本2 应有忠实度判定")
	}
}

func TestRunSingleSampleError(t *testing.T) {
	rt := &fakeRetriever{err: errors.New("检索挂了")}
	ds := testDataset()
	cfg := EvalConfig{Mode: "retrieve", KValues: []int{1}}

	results := runRetrieve(context.Background(), ds, cfg, rt)
	rep := ComputeMetrics(results, cfg.KValues)
	if rep.ErrorCount != 2 {
		t.Errorf("ErrorCount 应为 2，实际 %d", rep.ErrorCount)
	}
	if rep.RecallByK[1] != 0 {
		t.Errorf("全部失败时 Recall 应为 0，实际 %v", rep.RecallByK)
	}
}

func TestRunUnknownMode(t *testing.T) {
	cfg := EvalConfig{Mode: "xxx", KValues: []int{1}}
	_, err := Run(context.Background(), cfg, nil, nil, nil)
	if err == nil {
		t.Fatal("未知模式应报错")
	}
}

func TestConcurrencyLimit(t *testing.T) {
	rt := &fakeRetriever{results: testResults()}
	ds := &Dataset{Samples: make([]EvalSample, 4)}
	for i := range ds.Samples {
		ds.Samples[i] = EvalSample{Question: "q", ExpectedIDs: []string{}}
	}
	cfg := EvalConfig{Mode: "retrieve", KValues: []int{1}, Concurrency: 1}

	results := runRetrieve(context.Background(), ds, cfg, rt)
	if len(results) != 4 {
		t.Fatalf("结果数错误: %d", len(results))
	}
	if len(rt.callLog) != 4 {
		t.Errorf("检索调用次数错误: %d", len(rt.callLog))
	}
}

func TestComputeMetricsRecall(t *testing.T) {
	results := []EvalResult{
		{Sample: EvalSample{ExpectedIDs: []string{"a"}}, Recall: map[int]bool{1: true, 3: true}},
		{Sample: EvalSample{ExpectedIDs: []string{"a"}}, Recall: map[int]bool{1: false, 3: true}},
		{Sample: EvalSample{ExpectedIDs: []string{"a"}}, Recall: map[int]bool{1: false, 3: false}, Error: "失败"},
	}
	rep := ComputeMetrics(results, []int{1, 3})
	if rep.TotalSamples != 3 || rep.ErrorCount != 1 {
		t.Errorf("总数/错误数错误: %+v", rep)
	}
	// 有效样本 2：Recall@1 = 0.5，Recall@3 = 1.0
	if rep.RecallByK[1] != 0.5 {
		t.Errorf("Recall@1 应为 0.5，实际 %v", rep.RecallByK[1])
	}
	if rep.RecallByK[3] != 1.0 {
		t.Errorf("Recall@3 应为 1.0，实际 %v", rep.RecallByK[3])
	}
}
