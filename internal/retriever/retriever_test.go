package retriever

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/reranker"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

// --- Tokenizer 测试 ---

func TestSimpleTokenizerEnglish(t *testing.T) {
	tk := NewSimpleTokenizer()
	tokens := tk.Tokenize("Hello World")
	expected := []string{"hello", "world"}
	if len(tokens) != len(expected) {
		t.Fatalf("期望 %d 个 token，实际 %d: %v", len(expected), len(tokens), tokens)
	}
	for i, e := range expected {
		if tokens[i] != e {
			t.Errorf("第 %d 个 token 期望 %q，实际 %q", i, e, tokens[i])
		}
	}
}

func TestSimpleTokenizerChinese(t *testing.T) {
	tk := NewSimpleTokenizer()
	tokens := tk.Tokenize("向量数据库")
	expected := []string{"向量", "量数", "数据", "据库"}
	if len(tokens) != len(expected) {
		t.Fatalf("期望 %d 个 token，实际 %d: %v", len(expected), len(tokens), tokens)
	}
	for i, e := range expected {
		if tokens[i] != e {
			t.Errorf("第 %d 个 token 期望 %q，实际 %q", i, e, tokens[i])
		}
	}
}

func TestSimpleTokenizerMixed(t *testing.T) {
	tk := NewSimpleTokenizer()
	tokens := tk.Tokenize("使用Qdrant存储")
	// "使用" -> bigram: "使用"
	// "Qdrant" -> "qdrant"
	// "存储" -> bigram: "存储"
	expected := []string{"使用", "qdrant", "存储"}
	if len(tokens) != len(expected) {
		t.Fatalf("期望 %d 个 token，实际 %d: %v", len(expected), len(tokens), tokens)
	}
	for i, e := range expected {
		if tokens[i] != e {
			t.Errorf("第 %d 个 token 期望 %q，实际 %q", i, e, tokens[i])
		}
	}
}

// --- BM25 测试 ---

func TestBM25Index(t *testing.T) {
	idx := NewBM25Index(NewSimpleTokenizer())
	idx.Add("doc1", "Qdrant 是一个向量数据库", "")
	idx.Add("doc2", "Redis 是一个内存数据库", "")
	idx.Add("doc3", "Qdrant 支持向量检索和过滤 Qdrant", "")

	if idx.DocCount() != 3 {
		t.Fatalf("DocCount 期望 3，实际 %d", idx.DocCount())
	}

	results := idx.Search("Qdrant", 10)
	if len(results) == 0 {
		t.Fatal("期望搜索到结果")
	}

	// doc3 包含两次 Qdrant，分数应更高
	if results[0].ID != "doc3" {
		t.Errorf("期望 doc3 排第一，实际 %s", results[0].ID)
	}

	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("分数应 > 0，实际 %f", r.Score)
		}
	}
}

func TestBM25Remove(t *testing.T) {
	idx := NewBM25Index(NewSimpleTokenizer())
	idx.Add("doc1", "Qdrant 向量数据库", "")
	idx.Add("doc2", "Redis 缓存", "")

	idx.Remove("doc1")

	if idx.DocCount() != 1 {
		t.Fatalf("DocCount 期望 1，实际 %d", idx.DocCount())
	}

	results := idx.Search("Qdrant", 10)
	if len(results) != 0 {
		t.Errorf("Remove 后不应搜索到 doc1，实际结果: %v", results)
	}
}

func TestBM25Rebuild(t *testing.T) {
	idx := NewBM25Index(NewSimpleTokenizer())
	idx.Add("old1", "旧版本遗留信息", "")

	docs := []BM25Doc{
		{ID: "new1", Content: "新的向量数据库"},
		{ID: "new2", Content: "另一篇新内容"},
	}
	idx.Rebuild(docs)

	if idx.DocCount() != 2 {
		t.Fatalf("Rebuild 后 DocCount 期望 2，实际 %d", idx.DocCount())
	}

	results := idx.Search("遗留", 10)
	if len(results) != 0 {
		t.Error("Rebuild 后不应搜索到旧文档")
	}

	results = idx.Search("向量", 10)
	if len(results) == 0 {
		t.Error("Rebuild 后应能搜索到新文档")
	}
}

// 知识库隔离：SearchFiltered 只返回指定 kb 的结果
func TestBM25SearchFilteredByKB(t *testing.T) {
	idx := NewBM25Index(NewSimpleTokenizer())
	idx.Add("doc1", "Qdrant 向量数据库", "kb-a")
	idx.Add("doc2", "Qdrant 配置指南", "kb-b")
	idx.Add("doc3", "Qdrant 部署", "kb-a")

	// 不带 kb 过滤：三篇都命中
	all := idx.Search("Qdrant", 10)
	if len(all) != 3 {
		t.Fatalf("不过滤时期望 3 条，实际 %d", len(all))
	}

	// 指定 kb-a：只返回 doc1/doc3
	onlyA := idx.SearchFiltered("Qdrant", 10, "kb-a")
	if len(onlyA) != 2 {
		t.Fatalf("kb-a 过滤后期望 2 条，实际 %d", len(onlyA))
	}
	for _, r := range onlyA {
		if r.ID == "doc2" {
			t.Error("kb-a 过滤不应包含 kb-b 的 doc2")
		}
	}

	// 指定 kb-b：只返回 doc2
	onlyB := idx.SearchFiltered("Qdrant", 10, "kb-b")
	if len(onlyB) != 1 || onlyB[0].ID != "doc2" {
		t.Errorf("kb-b 过滤期望仅 doc2，实际 %v", onlyB)
	}

	// 不存在的 kb：无结果
	none := idx.SearchFiltered("Qdrant", 10, "kb-zzz")
	if len(none) != 0 {
		t.Errorf("不存在的 kb 应无结果，实际 %v", none)
	}
}

// --- RRF 测试 ---

func TestFuseRRF(t *testing.T) {
	vectorResults := []RetrieveResult{
		{ID: "doc1", Content: "content1", Score: 0.9, Metadata: map[string]any{"k": "v1"}},
		{ID: "doc2", Content: "content2", Score: 0.8, Metadata: map[string]any{"k": "v2"}},
	}
	bm25Results := []BM25Result{
		{ID: "doc2", Score: 2.5},
		{ID: "doc3", Score: 1.8},
	}
	allDocs := map[string]RetrieveResult{
		"doc1": vectorResults[0],
		"doc2": vectorResults[1],
		"doc3": {ID: "doc3", Content: "content3", Metadata: map[string]any{"k": "v3"}},
	}

	cfg := RRFConfig{K: 60, VectorWeight: 0.7, BM25Weight: 0.3}
	results := FuseRRF(vectorResults, bm25Results, allDocs, cfg)

	if len(results) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(results))
	}

	// doc2 出现在两路，分数应最高
	if results[0].ID != "doc2" {
		t.Errorf("期望 doc2 排第一（两路都命中），实际 %s", results[0].ID)
	}

	// 验证分数降序
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("结果应按分数降序，第 %d 个(%f) > 第 %d 个(%f)", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestFuseRRFWeightChange(t *testing.T) {
	vectorResults := []RetrieveResult{
		{ID: "doc1", Content: "c1", Score: 0.9},
	}
	bm25Results := []BM25Result{
		{ID: "doc2", Score: 2.0},
	}
	allDocs := map[string]RetrieveResult{
		"doc1": vectorResults[0],
		"doc2": {ID: "doc2", Content: "c2"},
	}

	// 高向量权重
	cfg1 := RRFConfig{K: 60, VectorWeight: 0.9, BM25Weight: 0.1}
	results1 := FuseRRF(vectorResults, bm25Results, allDocs, cfg1)

	// 高 BM25 权重
	cfg2 := RRFConfig{K: 60, VectorWeight: 0.1, BM25Weight: 0.9}
	results2 := FuseRRF(vectorResults, bm25Results, allDocs, cfg2)

	if results1[0].ID == results2[0].ID {
		t.Error("改变权重后排序应发生变化")
	}
}

// --- Retriever 编排测试 ---

type mockEmbedder struct {
	vectors [][]float32
	err     error
	delay   time.Duration
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.vectors, nil
}

type mockVectorStore struct {
	results []vectorstore.SearchResult
	err     error
}

func (m *mockVectorStore) Search(ctx context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func (m *mockVectorStore) Upsert(ctx context.Context, records []vectorstore.VectorRecord) error { return nil }
func (m *mockVectorStore) Delete(ctx context.Context, ids []string) error                       { return nil }
func (m *mockVectorStore) EnsureCollection(ctx context.Context) error                           { return nil }
func (m *mockVectorStore) Close() error                                                         { return nil }

type mockReranker struct {
	err error
}

func (m *mockReranker) Rerank(ctx context.Context, query string, candidates []reranker.RerankCandidate, topN int) ([]reranker.RerankResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	results := make([]reranker.RerankResult, len(candidates))
	for i, c := range candidates {
		results[i] = reranker.RerankResult{
			ID:       c.ID,
			Content:  c.Content,
			Score:    c.Score + 0.1,
			Metadata: c.Metadata,
		}
	}
	return results, nil
}

func TestRetrieverSearch(t *testing.T) {
	emb := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
	vs := &mockVectorStore{results: []vectorstore.SearchResult{
		{ID: "v1", Score: 0.9, Payload: map[string]any{"content": "向量结果1"}},
		{ID: "v2", Score: 0.8, Payload: map[string]any{"content": "向量结果2"}},
	}}

	bm25 := NewBM25Index(NewSimpleTokenizer())
	bm25.Add("v1", "向量结果1 关键词匹配", "")
	bm25.Add("b1", "BM25 独有结果", "")

	rk := &mockReranker{}

	cfg := config.RetrieverConfig{
		TopK:           10,
		RRFK:           60,
		VectorWeight:   0.7,
		BM25Weight:     0.3,
		EnableBM25:     true,
		EnableReranker: true,
	}

	ret := NewRetriever(cfg, emb, vs, bm25, rk)
	results, err := ret.Search(context.Background(), RetrieveRequest{Query: "向量"})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("期望返回结果")
	}
}

func TestRetrieverDegradeNoReranker(t *testing.T) {
	emb := &mockEmbedder{vectors: [][]float32{{0.1, 0.2}}}
	vs := &mockVectorStore{results: []vectorstore.SearchResult{
		{ID: "v1", Score: 0.9, Payload: map[string]any{"content": "结果"}},
	}}

	cfg := config.RetrieverConfig{
		TopK:           10,
		RRFK:           60,
		VectorWeight:   0.7,
		BM25Weight:     0.3,
		EnableBM25:     false,
		EnableReranker: false,
	}

	ret := NewRetriever(cfg, emb, vs, nil, nil)
	results, err := ret.Search(context.Background(), RetrieveRequest{Query: "测试"})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	if results[0].ID != "v1" {
		t.Errorf("期望 ID=v1，实际 %s", results[0].ID)
	}
}

func TestRetrieverDegradeEmptyBM25(t *testing.T) {
	emb := &mockEmbedder{vectors: [][]float32{{0.1, 0.2}}}
	vs := &mockVectorStore{results: []vectorstore.SearchResult{
		{ID: "v1", Score: 0.9, Payload: map[string]any{"content": "纯向量结果"}},
	}}

	bm25 := NewBM25Index(NewSimpleTokenizer()) // 空索引

	cfg := config.RetrieverConfig{
		TopK:           10,
		RRFK:           60,
		VectorWeight:   0.7,
		BM25Weight:     0.3,
		EnableBM25:     true,
		EnableReranker: false,
	}

	ret := NewRetriever(cfg, emb, vs, bm25, nil)
	results, err := ret.Search(context.Background(), RetrieveRequest{Query: "测试"})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("BM25 为空时应退化为纯向量结果，期望 1 个结果，实际 %d", len(results))
	}
}

func TestRetrieverRerankFailDegrades(t *testing.T) {
	emb := &mockEmbedder{vectors: [][]float32{{0.1, 0.2}}}
	vs := &mockVectorStore{results: []vectorstore.SearchResult{
		{ID: "v1", Score: 0.9, Payload: map[string]any{"content": "内容"}},
	}}

	rk := &mockReranker{err: errors.New("reranker api down")}

	cfg := config.RetrieverConfig{
		TopK:           10,
		RRFK:           60,
		VectorWeight:   0.7,
		BM25Weight:     0.3,
		EnableBM25:     false,
		EnableReranker: true,
	}

	ret := NewRetriever(cfg, emb, vs, nil, rk)
	results, err := ret.Search(context.Background(), RetrieveRequest{Query: "测试"})
	if err != nil {
		t.Fatalf("Reranker 失败时 Search 应降级而非报错: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("降级后应返回融合结果")
	}
}
