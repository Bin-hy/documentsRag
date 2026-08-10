package retriever

import (
	"math"
	"sort"
	"sync"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// BM25Index 内存倒排索引接口
type BM25Index interface {
	Add(id string, content string, kbID string)
	Remove(id string)
	Search(query string, topK int) []BM25Result
	SearchFiltered(query string, topK int, kbID string) []BM25Result // 空 kbID 不过滤
	SearchFilteredByKBs(query string, topK int, kbIDs []string) []BM25Result // 空集合不过滤
	Rebuild(docs []BM25Doc)
	DocCount() int
}

type posting struct {
	docID string
	tf    int
}

type defaultBM25Index struct {
	mu        sync.RWMutex
	tokenizer Tokenizer
	inverted  map[string][]posting // term -> postings
	docLen    map[string]int       // docID -> token count
	docTerms  map[string][]string  // docID -> terms (用于 Remove)
	docKB     map[string]string    // docID -> kbID
	totalLen  int
	avgLen    float64
}

// NewBM25Index 创建 BM25 内存倒排索引
func NewBM25Index(tokenizer Tokenizer) BM25Index {
	return &defaultBM25Index{
		tokenizer: tokenizer,
		inverted:  make(map[string][]posting),
		docLen:    make(map[string]int),
		docTerms:  make(map[string][]string),
		docKB:     make(map[string]string),
	}
}

func (idx *defaultBM25Index) Add(id string, content string, kbID string) {
	tokens := idx.tokenizer.Tokenize(content)
	if len(tokens) == 0 {
		return
	}

	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 如果已存在先移除
	if _, exists := idx.docLen[id]; exists {
		idx.removeLocked(id)
	}

	idx.docLen[id] = len(tokens)
	idx.docKB[id] = kbID
	idx.totalLen += len(tokens)
	idx.avgLen = float64(idx.totalLen) / float64(len(idx.docLen))

	terms := make([]string, 0, len(tf))
	for term, count := range tf {
		idx.inverted[term] = append(idx.inverted[term], posting{docID: id, tf: count})
		terms = append(terms, term)
	}
	idx.docTerms[id] = terms
}

func (idx *defaultBM25Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(id)
}

func (idx *defaultBM25Index) removeLocked(id string) {
	docLen, exists := idx.docLen[id]
	if !exists {
		return
	}

	terms := idx.docTerms[id]
	for _, term := range terms {
		postings := idx.inverted[term]
		for i, p := range postings {
			if p.docID == id {
				idx.inverted[term] = append(postings[:i], postings[i+1:]...)
				break
			}
		}
		if len(idx.inverted[term]) == 0 {
			delete(idx.inverted, term)
		}
	}

	idx.totalLen -= docLen
	delete(idx.docLen, id)
	delete(idx.docTerms, id)
	delete(idx.docKB, id)

	if len(idx.docLen) > 0 {
		idx.avgLen = float64(idx.totalLen) / float64(len(idx.docLen))
	} else {
		idx.avgLen = 0
	}
}

func (idx *defaultBM25Index) Search(query string, topK int) []BM25Result {
	return idx.SearchFiltered(query, topK, "")
}

// SearchFiltered 按知识库过滤的 BM25 检索；kbID 为空时不过滤
func (idx *defaultBM25Index) SearchFiltered(query string, topK int, kbID string) []BM25Result {
	if kbID == "" {
		return idx.SearchFilteredByKBs(query, topK, nil)
	}
	return idx.SearchFilteredByKBs(query, topK, []string{kbID})
}

// SearchFilteredByKBs 按知识库集合过滤的 BM25 检索；kbIDs 为空时不过滤
func (idx *defaultBM25Index) SearchFilteredByKBs(query string, topK int, kbIDs []string) []BM25Result {
	tokens := idx.tokenizer.Tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := float64(len(idx.docLen))
	if n == 0 {
		return nil
	}

	allowed := make(map[string]bool, len(kbIDs))
	for _, id := range kbIDs {
		allowed[id] = true
	}

	scores := make(map[string]float64)

	for _, term := range tokens {
		postings, ok := idx.inverted[term]
		if !ok {
			continue
		}

		df := float64(len(postings))
		idf := math.Log((n-df+0.5)/(df+0.5) + 1)

		for _, p := range postings {
			// 知识库过滤（在锁内读取 docKB，安全）：空集合不过滤
			if len(allowed) > 0 && !allowed[idx.docKB[p.docID]] {
				continue
			}
			dl := float64(idx.docLen[p.docID])
			tfNorm := (float64(p.tf) * (bm25K1 + 1)) /
				(float64(p.tf) + bm25K1*(1-bm25B+bm25B*dl/idx.avgLen))
			scores[p.docID] += idf * tfNorm
		}
	}

	results := make([]BM25Result, 0, len(scores))
	for id, score := range scores {
		results = append(results, BM25Result{ID: id, Score: float32(score)})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results
}

func (idx *defaultBM25Index) Rebuild(docs []BM25Doc) {
	idx.mu.Lock()
	idx.inverted = make(map[string][]posting)
	idx.docLen = make(map[string]int)
	idx.docTerms = make(map[string][]string)
	idx.docKB = make(map[string]string)
	idx.totalLen = 0
	idx.avgLen = 0
	idx.mu.Unlock()

	for _, doc := range docs {
		idx.Add(doc.ID, doc.Content, doc.KBID)
	}
}

func (idx *defaultBM25Index) DocCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.docLen)
}
