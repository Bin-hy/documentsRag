package retriever

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/reranker"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

// Retriever 检索接口
type Retriever interface {
	Search(ctx context.Context, req RetrieveRequest) ([]RetrieveResult, error)
	// SearchMulti 多查询检索：多路并行 Search，跨路 RRF 融合为 Top-K
	SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error)
	// SearchByVector 按向量检索（HyDE 用）：不走查询 embed，直接向量搜索
	SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error)
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

	// BM25 检索（可选，按 kb_id 过滤）
	kbID, _ := req.Filter["kb_id"].(string)
	if r.config.EnableBM25 && r.bm25Index != nil && r.bm25Index.DocCount() > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bm25Results = r.bm25Index.SearchFiltered(req.Query, topK, kbID)
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

	// 思考链路：检索方式与融合召回数（F4）；rerank 前后对比随后单独上报（F5）
	if req.Trace != nil {
		method := "vector"
		if len(bm25Results) > 0 {
			method = "hybrid"
		}
		req.Trace(RetrieveTrace{
			Query:    req.Query,
			Method:   method,
			Recalled: len(fusedResults),
		})
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
			// 思考链路：rerank 前后排序对比（F5）
			if req.Trace != nil {
				req.Trace(RetrieveTrace{
					Query:        req.Query,
					RerankBefore: toRankedItems(fusedResults),
					RerankAfter:  toRankedItems(results),
				})
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
	return r.vectorSearchByVec(ctx, vectors[0], topK, filter)
}

// vectorSearchByVec 按向量执行向量检索（HyDE 与查询共用）
func (r *defaultRetriever) vectorSearchByVec(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error) {
	searchReq := vectorstore.SearchRequest{
		Vector: vector,
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

// toRankedItems 将检索结果转为排序对比项（RankedItem）
func toRankedItems(results []RetrieveResult) []RankedItem {
	items := make([]RankedItem, len(results))
	for i, r := range results {
		items[i] = RankedItem{
			ID:       r.ID,
			Filename: metaStr(r.Metadata, "filename"),
			Score:    r.Score,
			Rank:     i + 1,
		}
	}
	return items
}

// metaStr 从元数据安全提取字符串
func metaStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

// SearchByVector 按向量检索（HyDE 用）：无 BM25/重排，纯向量搜索。
// 思考链路埋点在 rag.hydeSearch 层完成（见 routing.go），此处不上报。
func (r *defaultRetriever) SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error) {
	if topK <= 0 {
		topK = r.config.TopK
	}
	return r.vectorSearchByVec(ctx, vector, topK, filter)
}

// SearchMulti 多查询检索：每路并行 Search（向量+BM25 内部 RRF），
// 跨路用 FuseMultiQuery（RRF）融合为 Top-K。单路失败忽略（warn），全部失败返回 error。
func (r *defaultRetriever) SearchMulti(ctx context.Context, req RetrieveRequest, queries []string) ([]RetrieveResult, error) {
	if len(queries) == 0 {
		return nil, fmt.Errorf("多查询检索：查询列表为空")
	}

	concurrency := r.config.MultiQueryConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	results := make([][]RetrieveResult, len(queries))
	errgroupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, q := range queries {
		i, q := i, q
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-errgroupCtx.Done():
				return
			}
			defer func() { <-sem }()

			out, err := r.Search(ctx, RetrieveRequest{Query: q, TopK: req.TopK, Filter: req.Filter})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				slog.Warn("多路检索单路失败，忽略该路", "query", q, "err", err)
				return
			}
			results[i] = out
		}()
	}
	wg.Wait()

	// 全部失败才返回错误；至少一路成功即可
	valid := 0
	perQuery := make([]PerQueryTrace, len(queries))
	for i, res := range results {
		if len(res) > 0 {
			valid++
		}
		// 思考链路：逐路数据（顺序与 queries 一致，N7）
		perQuery[i] = PerQueryTrace{Query: queries[i], Recalled: len(res)}
	}
	if valid == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("多路检索全部失败: %w", firstErr)
		}
		return nil, nil
	}

	topK := req.TopK
	if topK <= 0 {
		topK = r.config.TopK
	}
	fused := FuseMultiQuery(results, 60, topK)
	slog.Info("多路检索完成", "路数", len(queries), "有效路", valid, "融合数", len(fused))

	// 思考链路：多路融合结果（F4/N7）
	if req.Trace != nil {
		req.Trace(RetrieveTrace{
			Query:    req.Query,
			Method:   "multi_fusion",
			Recalled: len(fused),
			PerQuery: perQuery,
		})
	}
	return fused, nil
}
