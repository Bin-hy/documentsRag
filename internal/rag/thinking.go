package rag

import (
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// 思考链路采集层：定义思考环节的数据结构与采集器（TraceSink）。
// 埋点在各环节同步点完成后调用 recordStep；sink 为 nil 时直接跳过（N2 零开销）。

// ThinkingStepType 思考环节类型
type ThinkingStepType string

const (
	StepRouting    ThinkingStepType = "routing"       // 路由判定
	StepRewrite    ThinkingStepType = "query_rewrite" // 单查询改写
	StepMultiQuery ThinkingStepType = "multi_query"   // 多查询变体生成
	StepRetrieval  ThinkingStepType = "retrieval"     // 检索（单路或多路）
	StepRerank     ThinkingStepType = "rerank"        // 重排序前后对比
	StepChunks     ThinkingStepType = "chunks"        // 最终目标 chunks
	StepDecompose  ThinkingStepType = "decompose"     // 分解判定 + 子问题列表
	StepStepBack   ThinkingStepType = "step_back"     // 回退问题
	StepHyDE       ThinkingStepType = "hyde"          // 假设文档
)

// ThinkingStep 思考链路环节（JSON 直接序列化给前端）
type ThinkingStep struct {
	Type      ThinkingStepType `json:"type"`
	Label     string           `json:"label"`                // 前端展示标题
	ElapsedMS int64            `json:"elapsed_ms,omitempty"` // 环节耗时
	Data      any              `json:"data,omitempty"`       // 环节结构化数据
}

// TraceSink 思考链路采集器；nil 表示关闭（所有 Record 直接跳过，N2）
type TraceSink interface {
	Record(step ThinkingStep)
}

// TraceSinkFunc 函数式 TraceSink 适配器
type TraceSinkFunc func(step ThinkingStep)

// Record 实现 TraceSink 接口
func (f TraceSinkFunc) Record(step ThinkingStep) { f(step) }

// sliceSink 非流式采集器：按序追加步骤，结束时由 withThinking 附到 RAGResult
type sliceSink struct {
	steps []ThinkingStep
}

// Record 实现 TraceSink 接口
func (s *sliceSink) Record(step ThinkingStep) { s.steps = append(s.steps, step) }

// recordStep 统一埋点入口；sink 为 nil 时直接返回（N2 零开销）
func recordStep(sink TraceSink, step ThinkingStep) {
	if sink != nil {
		sink.Record(step)
	}
}

// ---- 各环节 Data 载荷 ----

// RoutingData 路由判定
type RoutingData struct {
	Complexity string `json:"complexity"`          // simple / medium / complex
	Strategy   string `json:"strategy"`            // direct / multi_query / decomposition
	Reasoning  string `json:"reasoning,omitempty"` // 判定推理说明
}

// RewriteData 单查询改写（含失败降级）
type RewriteData struct {
	Original  string `json:"original"`
	Rewritten string `json:"rewritten"`
	Fallback  bool   `json:"fallback,omitempty"` // true=改写失败，使用原问题
}

// MultiQueryData 多查询变体（含原问题）
type MultiQueryData struct {
	Variants []string `json:"variants"`
}

// RetrievalData 检索：单路或按查询分组的检索结果
type RetrievalData struct {
	Query    string        `json:"query"`               // 主查询（多路时为汇总展示用）
	PerQuery []PerQueryRet `json:"per_query,omitempty"` // 多路时逐路；单路时省略
	Method   string        `json:"method,omitempty"`    // 单路时：vector / bm25 / hybrid
	Recalled int           `json:"recalled"`            // 召回数（单路）或融合结果数（多路）
}

// PerQueryRet 多路检索中单路的数据
type PerQueryRet struct {
	Query    string `json:"query"`
	Method   string `json:"method"`
	Recalled int    `json:"recalled"`
}

// RerankData rerank 前后对比
type RerankData struct {
	Query  string       `json:"query"`
	Before []RankedItem `json:"before"`
	After  []RankedItem `json:"after"`
}

// RankedItem 排序对比中的单项
type RankedItem struct {
	ID       string  `json:"id"`
	Filename string  `json:"filename"`
	Score    float32 `json:"score"`
	Rank     int     `json:"rank"`
}

// ChunksData 最终目标 chunks（与 sources 同集合，含内容预览）
type ChunksData struct {
	Chunks []ChunkInfo `json:"chunks"`
}

// ChunkInfo 目标 chunk 信息
type ChunkInfo struct {
	ID       string  `json:"id"`
	Filename string  `json:"filename"`
	Heading  string  `json:"heading"`
	Score    float32 `json:"score"`
	Content  string  `json:"content"` // 完整内容（前端 CSS 截断 + 展开，数量由 MaxChunks 控量）
}

// DecomposeData 分解判定 + 子问题
type DecomposeData struct {
	ShouldDecompose bool     `json:"should_decompose"`
	SubQuestions    []string `json:"sub_questions,omitempty"`
}

// StepBackData 回退问题
type StepBackData struct {
	StepBackQuery string `json:"step_back_query"`
}

// HyDEData HyDE 假设文档（服务端截断，N3）
type HyDEData struct {
	HypoDoc string `json:"hypo_doc"`
}

// maxHypoDocPreviewLen HyDE 假设文档预览最大字符数（N3）
const maxHypoDocPreviewLen = 500

// truncateRunes 按 rune 数截断字符串（保证不截断多字节字符），超长追加省略号
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// ---- retriever.RetrieveTrace → ThinkingStep 载荷翻译 ----

// retrievalDataFrom 将 retriever 检索追踪翻译为 RetrievalData（F4）
func retrievalDataFrom(t retriever.RetrieveTrace) RetrievalData {
	d := RetrievalData{Query: t.Query, Method: t.Method, Recalled: t.Recalled}
	if len(t.PerQuery) > 0 {
		d.PerQuery = make([]PerQueryRet, len(t.PerQuery))
		for i, p := range t.PerQuery {
			d.PerQuery[i] = PerQueryRet{Query: p.Query, Method: p.Method, Recalled: p.Recalled}
		}
	}
	return d
}

// rerankDataFrom 将 retriever 检索追踪翻译为 RerankData（F5）
func rerankDataFrom(t retriever.RetrieveTrace) RerankData {
	return RerankData{
		Query:  t.Query,
		Before: toRagRanked(t.RerankBefore),
		After:  toRagRanked(t.RerankAfter),
	}
}

// toRagRanked 转换 retriever.RankedItem → rag.RankedItem（类型名冲突，需转换）
func toRagRanked(items []retriever.RankedItem) []RankedItem {
	out := make([]RankedItem, len(items))
	for i, it := range items {
		out[i] = RankedItem{ID: it.ID, Filename: it.Filename, Score: it.Score, Rank: it.Rank}
	}
	return out
}

// chunksDataFrom 从上下文条目与引用来源构造目标 chunks 展示（F6，同集合）
func chunksDataFrom(items []ContextItem, sources []Source) ChunksData {
	chunks := make([]ChunkInfo, len(items))
	for i := range items {
		chunks[i] = ChunkInfo{
			ID:       sources[i].ID,
			Filename: items[i].Filename,
			Heading:  items[i].Heading,
			Score:    sources[i].Score,
			Content:  items[i].Content,
		}
	}
	return ChunksData{Chunks: chunks}
}

// traceSinkForRequest 构造检索请求的思考链路回调（翻译为 StepRetrieval / StepRerank）
func traceSinkForRequest(sink TraceSink, query string) func(t retriever.RetrieveTrace) {
	if sink == nil {
		return nil
	}
	return func(t retriever.RetrieveTrace) {
		recordStep(sink, ThinkingStep{
			Type:  StepRetrieval,
			Label: "检索",
			Data:  retrievalDataFrom(t),
		})
		if len(t.RerankAfter) > 0 {
			recordStep(sink, ThinkingStep{
				Type:  StepRerank,
				Label: "重排序",
				Data:  rerankDataFrom(t),
			})
		}
	}
}
