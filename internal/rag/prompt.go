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
基于检索到的资料尽力回答；资料未覆盖的部分，明确指出「资料未覆盖该方面」，不要编造。
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

// defaultDecomposeJudgeTemplate 判定问题是否适合分解：输出 JSON
const defaultDecomposeJudgeTemplate = `判断以下问题是否需要分解为多个子问题分别检索。
需要分解：复合型问题（含多个独立信息需求）、需要对比分析、需要多步骤解答、问题复杂抽象。
不需要分解：简单事实查询、单一明确问题、定义类问题。
只输出 JSON：{"decompose": true 或 false, "reason": "简短理由"}，不要其他内容。
用户问题：{{.Question}}`

// defaultDecomposeListTemplate 生成子问题列表：输出 JSON 数组
const defaultDecomposeListTemplate = `将以下复杂问题分解为 {{.MaxSub}} 个以内、相互独立且可分别检索的子问题。每个子问题应能独立检索到相关信息。只输出 JSON 数组字符串（如 ["子问题1","子问题2"]），不要任何解释。
用户问题：{{.Question}}
子问题：`

// defaultStepBackJudgeTemplate 判定是否需要回退查询并生成回退问题：输出 JSON
const defaultStepBackJudgeTemplate = `判断以下问题是否适合「先退一步检索更抽象/更广泛的信息，再精答」的策略。
适合：需要高层概念理解、时间序列/趋势（如「最近」「历年」）、多步推理、需要广泛背景的问题。
不适合：简单事实查询、定义类问题、需要实时数据的问题。
如果适合，生成一个更抽象、更通用的回退问题用于检索。
只输出 JSON：{"step_back": true 或 false, "question": "回退问题（step_back 为 true 时）"}，不要其他内容。
用户问题：{{.Question}}`

// defaultRoutingTemplate 路由复杂度判定模板：输出 JSON（含数据源选择）
const defaultRoutingTemplate = `分析以下查询的复杂度、选择合适的检索策略，并选择本次查询使用的数据源。
复杂度：
- simple：简单事实查询、单一明确问题、定义类问题
- medium：需要多角度召回、同义改写有帮助的问题
- complex：复合型问题（多信息需求）、需要对比分析、多步骤解答、复杂抽象问题
策略：
- direct：直接检索（simple 用）
- multi_query：多查询多路召回（medium 用）
- decomposition：问题分解后逐子问题检索综合（complex 用）
数据源：
- vector_store：向量知识库（企业内部文档）
- web_search：web 搜索（外部互联网）
可选数据源（本次查询只能从以下范围内选择，不得超出）：
{{.AllowedText}}
只输出 JSON：{"complexity": "simple|medium|complex", "strategy": "direct|multi_query|decomposition", "data_source": "vector_store|web_search", "reasoning": "简短理由"}，不要其他内容。
用户问题：{{.Question}}`

// defaultHyDETemplate 假设文档生成模板
const defaultHyDETemplate = `请根据以下问题，写一段详细的假设性文档作为检索查询。即使不确定，也要写得像真实文档一样具体、详细，包含关键术语与可能的相关信息。这段假设文档将用于向量检索真实文档。
问题：{{.Question}}
假设性文档：`

// promptTemplates 一组已加载的模板
type promptTemplates struct {
	system         string // 系统提示词（纯文本，不渲染）
	context        string // 上下文注入模板
	rewrite        string // 改写模板
	multiQuery     string // 多查询变体生成模板
	decomposeJudge string // 分解判定模板
	decomposeList  string // 子问题生成模板
	stepBackJudge  string // 回退判定模板
	routing        string // 路由复杂度判定模板
	hyde           string // 假设文档生成模板
}

// loadPromptTemplates 从配置路径加载模板，读取失败或未配置时使用内置默认
func loadPromptTemplates(cfg config.RAGConfig) promptTemplates {
	return promptTemplates{
		system:         loadOrDefault(cfg.SystemPromptPath, defaultSystemPrompt),
		context:        loadOrDefault(cfg.ContextTemplatePath, defaultContextTemplate),
		rewrite:        loadOrDefault(cfg.RewriteTemplatePath, defaultRewriteTemplate),
		multiQuery:     loadOrDefault(cfg.MultiQueryTemplatePath, defaultMultiQueryTemplate),
		decomposeJudge: loadOrDefault(cfg.DecompositionTemplatePath, defaultDecomposeJudgeTemplate),
		decomposeList:  defaultDecomposeListTemplate, // 子问题生成模板暂不开放配置（与判定共用一个路径会产生歧义）
		stepBackJudge:  loadOrDefault(cfg.StepBackTemplatePath, defaultStepBackJudgeTemplate),
		routing:        loadOrDefault(cfg.RoutingTemplatePath, defaultRoutingTemplate),
		hyde:           loadOrDefault(cfg.HyDETemplatePath, defaultHyDETemplate),
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

// decomposeJudgeData 分解判定模板数据
type decomposeJudgeData struct {
	Question string
}

// renderDecomposeJudge 渲染分解判定提示
func renderDecomposeJudge(question string, tpl string) (string, error) {
	return renderTemplate(tpl, decomposeJudgeData{Question: question})
}

// decomposeListData 子问题生成模板数据
type decomposeListData struct {
	Question string
	MaxSub   int
}

// renderDecomposeList 渲染子问题生成提示
func renderDecomposeList(question string, maxSub int, tpl string) (string, error) {
	return renderTemplate(tpl, decomposeListData{Question: question, MaxSub: maxSub})
}

// stepBackJudgeData 回退判定模板数据
type stepBackJudgeData struct {
	Question string
}

// renderStepBackJudge 渲染回退判定提示
func renderStepBackJudge(question string, tpl string) (string, error) {
	return renderTemplate(tpl, stepBackJudgeData{Question: question})
}

// routeData 路由判定模板数据
type routeData struct {
	Question    string
	AllowedText string // 可选数据源说明（由允许的数据源集合渲染，约束 LLM 输出）
}

// renderRouting 渲染路由复杂度判定提示；allowedText 为允许的数据源说明文本
func renderRouting(question, allowedText, tpl string) (string, error) {
	return renderTemplate(tpl, routeData{Question: question, AllowedText: allowedText})
}

// hydeData 假设文档生成模板数据
type hydeData struct {
	Question string
}

// renderHyDE 渲染假设文档生成提示
func renderHyDE(question string, tpl string) (string, error) {
	return renderTemplate(tpl, hydeData{Question: question})
}

// renderRewrite 渲染 Query 改写提示
func renderRewrite(history []llm.Message, question string, tpl string) (string, error) {
	return renderTemplate(tpl, rewriteData{History: history, Question: question})
}
