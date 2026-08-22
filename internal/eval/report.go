package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/rag"
)

// EvalResult 单样本评估结果
type EvalResult struct {
	Sample     EvalSample
	Retrieved  []string     // 检索结果 ID（前 K 内）
	Answer     string       // 问答回答
	Sources    []string     // 引用来源文件名
	RawSources []rag.Source // 原始引用来源（忠实度判定用）
	Recall     map[int]bool // K → 是否命中期望片段
	Accuracy   *float64     // LLM 准确性评分 0-10（未评 = nil）
	Faithful   *bool        // LLM 忠实判定（未评 = nil）
	Error      string       // 单样本错误（非空 = 失败）
}

// Report 评估汇总报告
type Report struct {
	DatasetName   string          `json:"dataset_name"`
	Mode          string          `json:"mode"`
	TotalSamples  int             `json:"total_samples"`
	ErrorCount    int             `json:"error_count"`
	RecallByK     map[int]float64 `json:"recall_by_k,omitempty"`
	AvgAccuracy   *float64        `json:"avg_accuracy,omitempty"`
	FaithfulRatio *float64        `json:"faithful_ratio,omitempty"`
	Results       []EvalResult    `json:"results"`
}

// ComputeMetrics 汇总指标：Recall@K = 命中样本数/有效样本数；AvgAccuracy = 非 nil 评分均值；FaithfulRatio = true 占比
func ComputeMetrics(results []EvalResult, kValues []int) Report {
	r := Report{
		TotalSamples: len(results),
		RecallByK:    make(map[int]float64),
		Results:      results,
	}
	for _, res := range results {
		if res.Error != "" {
			r.ErrorCount++
		}
	}
	for _, k := range kValues {
		hit, valid := 0, 0
		for _, res := range results {
			if res.Error != "" || len(res.Sample.ExpectedIDs) == 0 {
				continue // 错误样本与无期望片段样本不计入分母
			}
			valid++
			if res.Recall[k] {
				hit++
			}
		}
		if valid > 0 {
			r.RecallByK[k] = float64(hit) / float64(valid)
		}
	}
	// 准确性均值
	var accSum float64
	accCount := 0
	for _, res := range results {
		if res.Accuracy != nil {
			accSum += *res.Accuracy
			accCount++
		}
	}
	if accCount > 0 {
		avg := accSum / float64(accCount)
		r.AvgAccuracy = &avg
	}
	// 忠实比例
	faithSum, faithCount := 0, 0
	for _, res := range results {
		if res.Faithful != nil {
			faithCount++
			if *res.Faithful {
				faithSum++
			}
		}
	}
	if faithCount > 0 {
		ratio := float64(faithSum) / float64(faithCount)
		r.FaithfulRatio = &ratio
	}
	return r
}

// WriteReport 输出报告：text 人类可读表格 / json 完整 JSON
func WriteReport(r Report, w io.Writer, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	default:
		return writeTextReport(r, w)
	}
}

func writeTextReport(r Report, w io.Writer) error {
	var b strings.Builder
	b.WriteString("=== RAG 评估报告 ===\n")
	fmt.Fprintf(&b, "数据集: %s\n", r.DatasetName)
	fmt.Fprintf(&b, "模式: %s\n", r.Mode)
	fmt.Fprintf(&b, "样本数: %d（错误 %d）\n", r.TotalSamples, r.ErrorCount)
	if len(r.RecallByK) > 0 {
		b.WriteString("Recall@K:\n")
		for _, k := range sortedKeys(r.RecallByK) {
			fmt.Fprintf(&b, "  Recall@%d = %.4f\n", k, r.RecallByK[k])
		}
	}
	if r.AvgAccuracy != nil {
		fmt.Fprintf(&b, "平均准确性: %.2f / 10\n", *r.AvgAccuracy)
	}
	if r.FaithfulRatio != nil {
		fmt.Fprintf(&b, "忠实比例: %.2f\n", *r.FaithfulRatio)
	}
	b.WriteString("\n逐样本明细:\n")
	for i, res := range r.Results {
		fmt.Fprintf(&b, "[%d] 问题: %s\n", i+1, res.Sample.Question)
		if res.Error != "" {
			fmt.Fprintf(&b, "    错误: %s\n", res.Error)
			continue
		}
		if len(res.Recall) > 0 {
			fmt.Fprintf(&b, "    命中: %v\n", res.Recall)
		}
		if res.Answer != "" {
			fmt.Fprintf(&b, "    回答: %s\n", truncate(res.Answer, 120))
		}
		if res.Accuracy != nil {
			fmt.Fprintf(&b, "    准确性: %.1f\n", *res.Accuracy)
		}
		if res.Faithful != nil {
			fmt.Fprintf(&b, "    忠实: %v\n", *res.Faithful)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func sortedKeys(m map[int]float64) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 简单插入排序（K 值数量极少）
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// truncate 按字符（rune）截断，避免中文截断出非法 UTF-8
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
