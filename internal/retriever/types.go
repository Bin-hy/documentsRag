package retriever

// RetrieveRequest 检索请求
type RetrieveRequest struct {
	Query  string
	TopK   int
	Filter map[string]any
}

// RetrieveResult 检索结果
type RetrieveResult struct {
	ID       string
	Content  string
	Score    float32
	Metadata map[string]any
}

// BM25Result BM25 检索结果
type BM25Result struct {
	ID    string
	Score float32
}

// BM25Doc BM25 索引文档
type BM25Doc struct {
	ID      string
	Content string
}
