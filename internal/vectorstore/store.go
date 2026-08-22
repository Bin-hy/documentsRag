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
	// Get 按 ID 取单个点的 payload（引用来源查看 chunk 原文用）；不存在返回 ok=false
	Get(ctx context.Context, id string) (map[string]any, bool, error)
	Delete(ctx context.Context, ids []string) error
	// DeleteByFilter 按 payload 过滤条件删除（pipeline 重试补偿：清理同一 document_id 的旧 chunk）
	DeleteByFilter(ctx context.Context, filter map[string]any) error
	EnsureCollection(ctx context.Context) error
	Close() error
}
