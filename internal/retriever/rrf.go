package retriever

import "sort"

// RRFConfig RRF 融合配置
type RRFConfig struct {
	K            int
	VectorWeight float32
	BM25Weight   float32
}

// FuseRRF 将向量检索和 BM25 检索结果通过加权 RRF 融合
func FuseRRF(vectorResults []RetrieveResult, bm25Results []BM25Result, allDocs map[string]RetrieveResult, cfg RRFConfig) []RetrieveResult {
	if cfg.K <= 0 {
		cfg.K = 60
	}

	scores := make(map[string]float64)

	// 向量检索结果按 rank 贡献分数
	for rank, r := range vectorResults {
		scores[r.ID] += float64(cfg.VectorWeight) / float64(cfg.K+rank+1)
	}

	// BM25 检索结果按 rank 贡献分数
	for rank, r := range bm25Results {
		scores[r.ID] += float64(cfg.BM25Weight) / float64(cfg.K+rank+1)
	}

	// 构建融合结果
	results := make([]RetrieveResult, 0, len(scores))
	for id, score := range scores {
		var result RetrieveResult
		// 优先从向量结果中获取完整信息
		if doc, ok := findInVector(vectorResults, id); ok {
			result = doc
		} else if doc, ok := allDocs[id]; ok {
			result = doc
		} else {
			result = RetrieveResult{ID: id}
		}
		result.Score = float32(score)
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func findInVector(results []RetrieveResult, id string) (RetrieveResult, bool) {
	for _, r := range results {
		if r.ID == id {
			return r, true
		}
	}
	return RetrieveResult{}, false
}

// FuseMultiQuery 多路查询结果 RRF 融合：
// 每路结果作为一个排名列表，按 1/(k+rank) 贡献分数，跨路同 ID 累加，按分数降序取前 topK。
// listOfResults: 每路查询的检索结果（已按相关度排序）；k: RRF 常数（<=0 默认 60）；topK: 返回条数。
func FuseMultiQuery(listOfResults [][]RetrieveResult, k, topK int) []RetrieveResult {
	if k <= 0 {
		k = 60
	}
	if topK <= 0 {
		topK = len(listOfResults)
	}

	scores := make(map[string]float64)
	// 每路结果按 rank 贡献分数
	for _, results := range listOfResults {
		for rank, r := range results {
			scores[r.ID] += 1.0 / float64(k+rank+1)
		}
	}

	// 构建融合结果（从任一路取完整文档信息）
	results := make([]RetrieveResult, 0, len(scores))
	for id, score := range scores {
		var result RetrieveResult
		for _, list := range listOfResults {
			if doc, ok := findInVector(list, id); ok {
				result = doc
				break
			}
		}
		if result.ID == "" {
			result = RetrieveResult{ID: id}
		}
		result.Score = float32(score)
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}
