package retriever

import (
	"context"
	"fmt"
	"sync"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/reranker"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

// Retriever 检索接口
type Retriever interface {
	Search(ctx context.Context, req RetrieveRequest) ([]RetrieveResult, error)
}

type defaultRetriever struct {
	config      config.RetrieverConfig
	embedder    embedding.Embedder
	vectorstore vectorstore.VectorStore
	bm25Index   BM25Index
	reranker    reranker.Reranker
}

// NewRetriever 创建检索器
func NewRetriever(
	cfg config.RetrieverConfig,
	emb embedding.Embedder,
	vs vectorstore.VectorStore,
	idx BM25Index,
	rk reranker.Reranker,
) Retriever {
	return &defaultRetriever{
		config:      cfg,
		embedder:    emb,
		vectorstore: vs,
		bm25Index:   idx,
		reranker:    rk,
	}
}

func (r *defaultRetriever) Search(ctx context.Context, req RetrieveRequest) ([]RetrieveResult, error) {
	topK := req.TopK
	if topK <= 0 {
		topK = r.config.TopK
	}

	var (
		vectorResults []RetrieveResult
		bm25Results   []BM25Result
		vectorErr     error
		wg            sync.WaitGroup
	)

	// 向量检索（始终执行）
	wg.Add(1)
	go func() {
		defer wg.Done()
		vectorResults, vectorErr = r.vectorSearch(ctx, req.Query, topK, req.Filter)
	}()

	// BM25 检索（可选）
	if r.config.EnableBM25 && r.bm25Index != nil && r.bm25Index.DocCount() > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bm25Results = r.bm25Index.Search(req.Query, topK)
		}()
	}

	wg.Wait()

	if vectorErr != nil {
		return nil, fmt.Errorf("向量检索失败: %w", vectorErr)
	}

	// 如果没有 BM25 结果，直接用向量结果
	var fusedResults []RetrieveResult
	if len(bm25Results) == 0 {
		fusedResults = vectorResults
	} else {
		allDocs := make(map[string]RetrieveResult, len(vectorResults))
		for _, vr := range vectorResults {
			allDocs[vr.ID] = vr
		}

		rrfCfg := RRFConfig{
			K:            r.config.RRFK,
			VectorWeight: r.config.VectorWeight,
			BM25Weight:   r.config.BM25Weight,
		}
		fusedResults = FuseRRF(vectorResults, bm25Results, allDocs, rrfCfg)
	}

	if len(fusedResults) > topK {
		fusedResults = fusedResults[:topK]
	}

	// Reranker（可选）
	if r.config.EnableReranker && r.reranker != nil && len(fusedResults) > 0 {
		candidates := make([]reranker.RerankCandidate, len(fusedResults))
		for i, fr := range fusedResults {
			candidates[i] = reranker.RerankCandidate{
				ID:       fr.ID,
				Content:  fr.Content,
				Score:    fr.Score,
				Metadata: fr.Metadata,
			}
		}

		reranked, err := r.reranker.Rerank(ctx, req.Query, candidates, topK)
		if err == nil {
			results := make([]RetrieveResult, len(reranked))
			for i, rr := range reranked {
				results[i] = RetrieveResult{
					ID:       rr.ID,
					Content:  rr.Content,
					Score:    rr.Score,
					Metadata: rr.Metadata,
				}
			}
			return results, nil
		}
		// Reranker 失败时降级返回融合结果
	}

	return fusedResults, nil
}

func (r *defaultRetriever) vectorSearch(ctx context.Context, query string, topK int, filter map[string]any) ([]RetrieveResult, error) {
	vectors, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	searchReq := vectorstore.SearchRequest{
		Vector: vectors[0],
		TopK:   topK,
		Filter: filter,
	}

	searchResults, err := r.vectorstore.Search(ctx, searchReq)
	if err != nil {
		return nil, err
	}

	results := make([]RetrieveResult, len(searchResults))
	for i, sr := range searchResults {
		content, _ := sr.Payload["content"].(string)
		results[i] = RetrieveResult{
			ID:       sr.ID,
			Content:  content,
			Score:    sr.Score,
			Metadata: sr.Payload,
		}
	}

	return results, nil
}
