package vectorstore

import "context"

// VectorRecord 入库记录
type VectorRecord struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

// SearchRequest 搜索请求
type SearchRequest struct {
	Vector []float32
	TopK   int
	Filter map[string]any
}

// SearchResult 搜索结果
type SearchResult struct {
	ID      string
	Score   float32
	Payload map[string]any
}

// VectorStore 向量存储接口
type VectorStore interface {
	Upsert(ctx context.Context, records []VectorRecord) error
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	Delete(ctx context.Context, ids []string) error
	EnsureCollection(ctx context.Context) error
	Close() error
}
