package rag

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// Source 引用来源
type Source struct {
	ID         string  `json:"id"`                    // 检索片段 ID
	Filename   string  `json:"filename"`              // 来源文件名（来自元数据）
	Heading    string  `json:"heading"`               // 标题上下文
	Score      float32 `json:"score"`                 // 检索分数
	SourceType string  `json:"source_type,omitempty"` // 来源类型（vector_store / web_search 等）
}

// ContextItem 单条上下文（供模板渲染）
type ContextItem struct {
	Index    int    // 从 1 开始的编号
	Filename string // 来源文件名
	Heading  string // 标题上下文
	Content  string // 片段内容
}

// buildContext 将检索片段按 token 预算截断，产出结构化上下文条目与引用来源。
// 按序累加估算 token 数，超过 maxTokens 或达到 maxChunks 上限时停止。
func buildContext(chunks []retriever.RetrieveResult, maxTokens int, maxChunks int) ([]ContextItem, []Source) {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	if maxChunks <= 0 {
		maxChunks = 5
	}

	items := make([]ContextItem, 0, len(chunks))
	sources := make([]Source, 0, len(chunks))
	used := 0

	for i, chunk := range chunks {
		if i >= maxChunks {
			break
		}

		item := ContextItem{
			Index:    i + 1,
			Filename: metaString(chunk.Metadata, "filename"),
			Heading:  metaString(chunk.Metadata, "heading_context"),
			Content:  chunk.Content,
		}

		// 以默认格式估算该条占用的 token（含编号与来源标注开销）
		if used+estimateTokens(formatContextItem(item)) > maxTokens {
			break
		}

		items = append(items, item)
		used += estimateTokens(formatContextItem(item))

		sources = append(sources, Source{
			ID:         chunk.ID,
			Filename:   item.Filename,
			Heading:    item.Heading,
			Score:      chunk.Score,
			SourceType: metaString(chunk.Metadata, "source_type"),
		})
	}

	return items, sources
}

// formatContextItem 按默认格式渲染单条上下文，作为 token 估算基准
func formatContextItem(item ContextItem) string {
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(strconv.Itoa(item.Index))
	sb.WriteString("]")
	if item.Filename != "" || item.Heading != "" {
		sb.WriteString("（来源：")
		sb.WriteString(item.Filename)
		if item.Heading != "" {
			sb.WriteString(" / ")
			sb.WriteString(item.Heading)
		}
		sb.WriteString("）")
	}
	sb.WriteString("\n")
	sb.WriteString(item.Content)
	sb.WriteString("\n\n")
	return sb.String()
}

// metaString 从元数据安全提取字符串
func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

// estimateTokens 轻量 token 估算：中文字符 2 token、英文按词 1、标点 1（与 chunker 思路一致）
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}

	count := 0
	wordLen := 0

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			if wordLen > 0 {
				count++
				wordLen = 0
			}
			count += 2
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			if wordLen > 0 {
				count++
				wordLen = 0
			}
			count++
		case unicode.IsSpace(r):
			if wordLen > 0 {
				count++
				wordLen = 0
			}
		default:
			wordLen++
		}
	}

	if wordLen > 0 {
		count++
	}

	return count
}
