package datasource

import (
	"context"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// vectorStoreSource 向量知识库数据源：包装 retriever.Retriever（含向量+BM25+重排），
// 是默认且始终可用的数据源。
type vectorStoreSource struct {
	retriever retriever.Retriever
}

// NewVectorStoreSource 创建向量知识库数据源
func NewVectorStoreSource(rt retriever.Retriever) Source {
	return &vectorStoreSource{retriever: rt}
}

func (s *vectorStoreSource) Name() string    { return SourceVectorStore }
func (s *vectorStoreSource) Available() bool { return true }

func (s *vectorStoreSource) Search(ctx context.Context, req SearchRequest) ([]retriever.RetrieveResult, error) {
	results, err := s.retriever.Search(ctx, retriever.RetrieveRequest{
		Query:  req.Query,
		TopK:   req.TopK,
		Filter: req.Filter,
	})
	if err != nil {
		return nil, err
	}

	// 标记来源类型（供引用/思考链路区分数据源）
	for i := range results {
		if results[i].Metadata == nil {
			results[i].Metadata = make(map[string]any)
		}
		results[i].Metadata["source_type"] = SourceVectorStore
	}
	return results, nil
}
