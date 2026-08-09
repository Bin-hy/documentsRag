package datasource

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// mockRetriever 最小可用的 retriever.Retriever 实现（仅 Search 生效）
type mockRetriever struct {
	results []retriever.RetrieveResult
	err     error
	lastReq retriever.RetrieveRequest
}

func (m *mockRetriever) Search(ctx context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
	m.lastReq = req
	return m.results, m.err
}

func (m *mockRetriever) SearchMulti(ctx context.Context, req retriever.RetrieveRequest, queries []string) ([]retriever.RetrieveResult, error) {
	return nil, nil
}

func (m *mockRetriever) SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]retriever.RetrieveResult, error) {
	return nil, nil
}

func (m *mockRetriever) Rerank(ctx context.Context, query string, results []retriever.RetrieveResult, topN int, trace func(retriever.RetrieveTrace)) ([]retriever.RetrieveResult, error) {
	return results, nil
}

// 测试用数据源
type testSource struct {
	name      string
	available bool
}

func (s *testSource) Name() string    { return s.name }
func (s *testSource) Available() bool { return s.available }
func (s *testSource) Search(ctx context.Context, req SearchRequest) ([]retriever.RetrieveResult, error) {
	return nil, nil
}

func TestRegistryRegisterGetListNames(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&testSource{name: "a"})
	reg.Register(&testSource{name: "b", available: true})

	if got, ok := reg.Get("a"); !ok || got.Name() != "a" {
		t.Errorf("Get(a) = %v, %v; want ok=true Name=a", got, ok)
	}
	if _, ok := reg.Get("missing"); ok {
		t.Errorf("Get(missing) 应返回 ok=false")
	}
	if got := reg.Names(); !slices.Contains(got, "a") || !slices.Contains(got, "b") || len(got) != 2 {
		t.Errorf("Names() = %v, want [a b]", got)
	}
	if got := reg.List(); len(got) != 2 {
		t.Errorf("List() 长度 = %d, want 2", len(got))
	}
}

func TestRegistryOverwrite(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&testSource{name: "a"})
	reg.Register(&testSource{name: "a", available: true}) // 重名覆盖

	got, _ := reg.Get("a")
	if !got.Available() {
		t.Errorf("重名注册后 Get(a).Available() = false, want true（应覆盖）")
	}
}

func TestRegistryRegisterNil(t *testing.T) {
	reg := NewRegistry()
	reg.Register(nil) // 不应 panic
	if got := reg.Names(); len(got) != 0 {
		t.Errorf("注册 nil 后 Names() = %v, want 空", got)
	}
}

func TestVectorStoreSourceSearch(t *testing.T) {
	mock := &mockRetriever{
		results: []retriever.RetrieveResult{
			{ID: "d1", Content: "内容1", Metadata: map[string]any{"filename": "f1.md"}},
			{ID: "d2", Content: "内容2"}, // 无 Metadata
		},
	}
	src := NewVectorStoreSource(mock)

	results, err := src.Search(context.Background(), SearchRequest{Query: "q", TopK: 5, Filter: map[string]any{"kb_id": "kb1"}})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}
	// 透传请求
	if mock.lastReq.Query != "q" || mock.lastReq.TopK != 5 || mock.lastReq.Filter["kb_id"] != "kb1" {
		t.Errorf("请求未透传: %+v", mock.lastReq)
	}
	// source_type 标记
	for _, r := range results {
		if got := r.Metadata["source_type"]; got != SourceVectorStore {
			t.Errorf("Metadata[source_type] = %v, want %s", got, SourceVectorStore)
		}
	}
	// 原有元数据保留
	if results[0].Metadata["filename"] != "f1.md" {
		t.Errorf("原有 Metadata 丢失: %+v", results[0].Metadata)
	}
}

func TestVectorStoreSourceSearchError(t *testing.T) {
	wantErr := errors.New("boom")
	src := NewVectorStoreSource(&mockRetriever{err: wantErr})
	if _, err := src.Search(context.Background(), SearchRequest{Query: "q"}); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestWebSearchSource(t *testing.T) {
	src := NewWebSearchSource()
	if src.Name() != SourceWebSearch {
		t.Errorf("Name() = %s, want %s", src.Name(), SourceWebSearch)
	}
	if src.Available() {
		t.Errorf("占位源 Available() = true, want false")
	}
	if _, err := src.Search(context.Background(), SearchRequest{Query: "q"}); err == nil {
		t.Errorf("占位源 Search 应返回错误")
	}
}
