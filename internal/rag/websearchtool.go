package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/search"
)

// webSearchTool 联网搜索工具（增强模式 function calling 用）。
// 通过可插拔的 search.Provider 执行搜索，把结果格式化为文本回传 LLM。
type webSearchTool struct {
	provider search.Provider
}

// NewWebSearchTool 创建联网搜索工具
func NewWebSearchTool(p search.Provider) Tool {
	return &webSearchTool{provider: p}
}

func (t *webSearchTool) Name() string { return "web_search" }

func (t *webSearchTool) Description() string {
	return "搜索互联网获取实时信息，返回相关网页的标题、链接与内容摘要。当问题涉及实时数据、最新事件或知识库未覆盖的内容时使用。"
}

func (t *webSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "搜索查询词"},
			"count": map[string]any{"type": "integer", "description": "返回条数（可选）"},
		},
		"required": []string{"query"},
	}
}

// maxWebResultContentLen 单条结果正文截断长度（控制上下文 token 占用）
const maxWebResultContentLen = 800

func (t *webSearchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("解析 web_search 参数失败: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("web_search 缺少 query 参数")
	}

	results, err := t.provider.Search(ctx, args.Query, search.Options{Count: args.Count})
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "未搜索到相关结果。", nil
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s\n链接: %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			sb.WriteString("摘要: " + r.Snippet + "\n")
		}
		if r.Content != "" {
			sb.WriteString("内容: " + truncateRunes(r.Content, maxWebResultContentLen) + "\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
