package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/chunker"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

type mockEmbedder struct {
	dim       int
	shouldErr bool
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.shouldErr {
		return nil, errors.New("embed error")
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = make([]float32, m.dim)
	}
	return vectors, nil
}

type mockVectorStore struct {
	records   []vectorstore.VectorRecord
	shouldErr bool
}

func (m *mockVectorStore) Upsert(ctx context.Context, records []vectorstore.VectorRecord) error {
	if m.shouldErr {
		return errors.New("upsert error")
	}
	m.records = append(m.records, records...)
	return nil
}

func (m *mockVectorStore) Search(ctx context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(ctx context.Context, ids []string) error {
	return nil
}

func (m *mockVectorStore) EnsureCollection(ctx context.Context) error {
	return nil
}

func (m *mockVectorStore) Close() error {
	return nil
}

func TestIngestSuccess(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{
			Strategy:  chunker.StrategyRecursive,
			ChunkSize: 500,
		},
		nil,
	)

	input := "# 标题\n\n这是正文段落。\n\n## 二级标题\n\n这是第二段内容。"
	reader := strings.NewReader(input)
	info := loader.FileInfo{Filename: "test.md", Size: int64(len(input))}

	_, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     reader,
		Info:       info,
	})
	if err != nil {
		t.Fatalf("Ingest 失败: %v", err)
	}

	if len(vs.records) == 0 {
		t.Fatal("期望至少有 1 条 record 入库")
	}

	for _, r := range vs.records {
		if r.Payload["filename"] != "test.md" {
			t.Errorf("filename 期望 test.md，实际 %v", r.Payload["filename"])
		}
		if r.Payload["kb_id"] != "kb-1" {
			t.Errorf("kb_id 期望 kb-1，实际 %v", r.Payload["kb_id"])
		}
		if r.Payload["document_id"] != "doc-1" {
			t.Errorf("document_id 期望 doc-1，实际 %v", r.Payload["document_id"])
		}
		if _, ok := r.Payload["chunk_id"]; !ok {
			t.Error("record 缺少 chunk_id 字段")
		}
		if _, ok := r.Payload["heading_context"]; !ok {
			t.Error("record 缺少 heading_context 字段")
		}
		if _, ok := r.Payload["chunk_index"]; !ok {
			t.Error("record 缺少 chunk_index 字段")
		}
		if _, ok := r.Payload["content"]; !ok {
			t.Error("record 缺少 content 字段")
		}
		if len(r.Vector) != 4 {
			t.Errorf("向量维度期望 4，实际 %d", len(r.Vector))
		}
		if r.ID == "" {
			t.Error("record ID 不应为空")
		}
	}
}

func TestIngestEmbedError(t *testing.T) {
	emb := &mockEmbedder{dim: 4, shouldErr: true}
	vs := &mockVectorStore{}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 500},
		nil,
	)

	input := "一些文本内容"
	_, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader(input),
		Info:       loader.FileInfo{Filename: "test.txt"},
	})
	if err == nil {
		t.Fatal("Embed 失败时 Ingest 应返回 error")
	}
}

func TestIngestUpsertError(t *testing.T) {
	emb := &mockEmbedder{dim: 4}
	vs := &mockVectorStore{shouldErr: true}

	p := NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{Strategy: chunker.StrategyRecursive, ChunkSize: 500},
		nil,
	)

	input := "一些文本内容"
	_, err := p.Ingest(context.Background(), IngestRequest{
		KBID:       "kb-1",
		DocumentID: "doc-1",
		Reader:     strings.NewReader(input),
		Info:       loader.FileInfo{Filename: "test.txt"},
	})
	if err == nil {
		t.Fatal("Upsert 失败时 Ingest 应返回 error")
	}
}

// 确保 mockVectorStore 实现了 io.Closer（如果有）
var _ io.Closer = (*mockVectorStore)(nil)
