package retriever

// RetrieveRequest 检索请求
type RetrieveRequest struct {
	Query      string
	TopK       int
	Filter     map[string]any
	Trace      func(t RetrieveTrace) // 思考链路回调，nil=关闭（N2 零开销）
	SkipRerank bool                  // true=本检索不做 rerank（由调用方在融合/汇总后统一整体重排）
}

// RetrieveTrace 检索过程追踪数据（由 rag 层翻译为 ThinkingStep）
type RetrieveTrace struct {
	Query        string          // 本次检索的查询文本
	Method       string          // vector / bm25 / hybrid / multi_fusion / hyde_vector / hyde_orig
	Recalled     int             // 召回数（单路=融合后条数；多路=融合结果数）
	PerQuery     []PerQueryTrace // 多路检索时逐路数据（顺序与传入 queries 一致）
	RerankBefore []RankedItem    // rerank 前排序（Top-N），非空表示发生了 rerank
	RerankAfter  []RankedItem    // rerank 后排序（Top-N）
}

// PerQueryTrace 多路检索中单路的数据
type PerQueryTrace struct {
	Query    string
	Method   string
	Recalled int
}

// RankedItem 排序对比中的单项
type RankedItem struct {
	ID       string
	Filename string
	Score    float32
	Rank     int
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
	KBID    string // 知识库归属（kb 过滤用）；空表示系统级/不限
}
