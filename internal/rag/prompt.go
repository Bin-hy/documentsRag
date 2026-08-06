package rag

import (
	"os"
	"strings"
	"text/template"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
)

// 内置默认模板（配置未提供或读取失败时使用）

const defaultSystemPrompt = `你是一个基于企业知识库的问答助手。请严格基于以下检索到的资料回答用户问题。
如果资料中找不到答案，请明确回答「未找到相关资料」，不要编造。
回答时按 [编号] 标注引用来源，如 [1][2]。`

const defaultContextTemplate = `以下是检索到的相关资料：
{{- range .}}
[{{.Index}}]{{if .Filename}}（来源：{{.Filename}}{{if .Heading}} / {{.Heading}}{{end}}）{{end}}
{{.Content}}
{{end}}`

const defaultRewriteTemplate = `将用户问题改写为自包含、适合检索的独立查询。结合对话历史消解指代（如「它」「这个」），保留关键信息，不要添加资料中不存在的内容。仅输出改写后的查询本身，不要任何解释。
{{if .History}}
对话历史：
{{- range .History}}
{{.Role}}: {{.Content}}
{{- end}}
{{end}}
用户问题：{{.Question}}
改写后的查询：`

// defaultMultiQueryTemplate 多查询变体生成模板：输出 JSON 数组
const defaultMultiQueryTemplate = `根据用户问题生成 {{.Count}} 个不同表达角度的检索查询变体，用于多路召回提升检索效果。变体应覆盖：同义改写、不同细节粒度、可能的隐含子主题。结合对话历史消解指代（如「它」「这个」）。只输出 JSON 数组字符串（如 ["变体1","变体2","变体3"]），不要任何解释或其他内容。
{{if .History}}
对话历史：
{{- range .History}}
{{.Role}}: {{.Content}}
{{- end}}
{{end}}
用户问题：{{.Question}}
查询变体：`

// promptTemplates 一组已加载的模板
type promptTemplates struct {
	system     string // 系统提示词（纯文本，不渲染）
	context    string // 上下文注入模板
	rewrite    string // 改写模板
	multiQuery string // 多查询变体生成模板
}

// loadPromptTemplates 从配置路径加载模板，读取失败或未配置时使用内置默认
func loadPromptTemplates(cfg config.RAGConfig) promptTemplates {
	return promptTemplates{
		system:     loadOrDefault(cfg.SystemPromptPath, defaultSystemPrompt),
		context:    loadOrDefault(cfg.ContextTemplatePath, defaultContextTemplate),
		rewrite:    loadOrDefault(cfg.RewriteTemplatePath, defaultRewriteTemplate),
		multiQuery: loadOrDefault(cfg.MultiQueryTemplatePath, defaultMultiQueryTemplate),
	}
}

func loadOrDefault(path string, def string) string {
	if path == "" {
		return def
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	return string(b)
}

// rewriteData 改写模板渲染数据
type rewriteData struct {
	History  []llm.Message
	Question string
}

// renderTemplate 用 text/template 渲染模板
func renderTemplate(tpl string, data any) (string, error) {
	t, err := template.New("prompt").Parse(tpl)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// renderContext 渲染上下文注入文本
func renderContext(items []ContextItem, tpl string) (string, error) {
	return renderTemplate(tpl, items)
}

// multiQueryData 多查询模板渲染数据
type multiQueryData struct {
	History  []llm.Message
	Question string
	Count    int
}

// renderMultiQuery 渲染多查询变体生成提示
func renderMultiQuery(history []llm.Message, question string, count int, tpl string) (string, error) {
	return renderTemplate(tpl, multiQueryData{History: history, Question: question, Count: count})
}

// renderRewrite 渲染 Query 改写提示
func renderRewrite(history []llm.Message, question string, tpl string) (string, error) {
	return renderTemplate(tpl, rewriteData{History: history, Question: question})
}
