package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/rag"
)

// accuracyPrompt LLM-as-Judge 准确性评分提示词：要求输出 JSON {"score": 0-10}
const accuracyPrompt = `你是一个严格的 RAG 问答质量评审员。请根据「标准答案」评判「模型回答」的准确性。
评分标准（0-10）：
- 9-10：完全正确，覆盖标准答案所有关键点
- 6-8：基本正确，有少量遗漏或轻微偏差
- 3-5：部分正确，存在明显错误或遗漏关键点
- 0-2：完全错误或答非所问

只输出 JSON：{"score": <0-10 的整数>}，不要输出其他内容。`

// faithfulnessPrompt LLM-as-Judge 忠实度判定提示词：要求输出 JSON {"faithful": true/false}
const faithfulnessPrompt = `你是一个严格的 RAG 忠实度评审员。请判断「模型回答」是否完全基于提供的「引用资料」，没有编造或臆测。
- 回答内容全部能在引用资料中找到依据 → {"faithful": true}
- 回答包含引用资料中没有的信息、或与引用资料矛盾 → {"faithful": false}

只输出 JSON：{"faithful": <true 或 false>}，不要输出其他内容。`

// zeroTemp 评审使用确定性温度
var zeroTemp float32 = 0.0

// JudgeAccuracy 用评审模型对回答按标准答案打分（0-10）
func JudgeAccuracy(ctx context.Context, client llm.LLM, question, answer, standardAnswer, model string) (float64, error) {
	user := fmt.Sprintf("问题：%s\n\n标准答案：%s\n\n模型回答：%s", question, standardAnswer, answer)
	out, err := generateJSON(ctx, client, accuracyPrompt, user, model)
	if err != nil {
		return 0, err
	}
	var parsed struct {
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return 0, fmt.Errorf("解析准确性评分失败 %q: %w", out, err)
	}
	if parsed.Score < 0 || parsed.Score > 10 {
		return 0, fmt.Errorf("评分越界: %v", parsed.Score)
	}
	return parsed.Score, nil
}

// JudgeFaithfulness 用评审模型判断回答是否基于引用来源
func JudgeFaithfulness(ctx context.Context, client llm.LLM, question, answer string, sources []rag.Source, model string) (bool, error) {
	var sb strings.Builder
	sb.WriteString("问题：")
	sb.WriteString(question)
	sb.WriteString("\n\n引用资料：\n")
	for i, src := range sources {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, src.Filename)
		if src.Heading != "" {
			fmt.Fprintf(&sb, "    标题: %s\n", src.Heading)
		}
	}
	sb.WriteString("\n模型回答：")
	sb.WriteString(answer)

	out, err := generateJSON(ctx, client, faithfulnessPrompt, sb.String(), model)
	if err != nil {
		return false, err
	}
	var parsed struct {
		Faithful bool `json:"faithful"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return false, fmt.Errorf("解析忠实度判定失败 %q: %w", out, err)
	}
	return parsed.Faithful, nil
}

// generateJSON 调 LLM 生成并清理出 JSON（容错：提取第一个 { 到最后一个 }）
func generateJSON(ctx context.Context, client llm.LLM, system, user, model string) (string, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}
	opts := []llm.ChatOption{llm.WithTemperature(zeroTemp)}
	if model != "" {
		opts = append(opts, llm.WithModel(model))
	}
	out, err := client.Generate(ctx, msgs, opts...)
	if err != nil {
		return "", err
	}
	// 清理：提取首个 { 到末尾 }（模型可能附带解释文字）
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("评审输出不含 JSON: %q", out)
	}
	return out[start : end+1], nil
}
