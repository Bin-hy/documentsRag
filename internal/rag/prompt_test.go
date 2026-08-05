package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
)

// AC11: 默认上下文模板渲染正确
func TestRenderContext_DefaultTemplate(t *testing.T) {
	items := []ContextItem{
		{Index: 1, Filename: "a.md", Heading: "标题A", Content: "内容一"},
		{Index: 2, Filename: "b.md", Heading: "", Content: "内容二"},
	}

	out, err := renderContext(items, defaultContextTemplate)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "[1]（来源：a.md / 标题A）") {
		t.Errorf("编号与来源标注缺失:\n%s", out)
	}
	if !strings.Contains(out, "内容一") || !strings.Contains(out, "内容二") {
		t.Errorf("内容缺失:\n%s", out)
	}
	// 无 Heading 时只显示 filename
	if !strings.Contains(out, "[2]（来源：b.md）") {
		t.Errorf("无 Heading 时标注错误:\n%s", out)
	}
}

// AC11: 改写模板携带历史与问题
func TestRenderRewrite_DefaultTemplate(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "RAG 是什么？"},
		{Role: llm.RoleAssistant, Content: "RAG 是检索增强生成。"},
	}

	out, err := renderRewrite(history, "它有哪些优点？", defaultRewriteTemplate)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "RAG 是什么？") {
		t.Errorf("历史缺失:\n%s", out)
	}
	if !strings.Contains(out, "它有哪些优点？") {
		t.Errorf("问题缺失:\n%s", out)
	}
}

// AC11: 自定义模板渲染生效
func TestRenderContext_CustomTemplate(t *testing.T) {
	custom := "上下文开始{{range .}}|{{.Content}}{{end}}上下文结束"
	items := []ContextItem{
		{Index: 1, Content: "A"},
		{Index: 2, Content: "B"},
	}

	out, err := renderContext(items, custom)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "上下文开始|A|B上下文结束" {
		t.Errorf("自定义模板渲染错误: %q", out)
	}
}

// AC11: loadPromptTemplates 优先读配置文件
func TestLoadPromptTemplates_FromFile(t *testing.T) {
	dir := t.TempDir()
	sysPath := filepath.Join(dir, "system.txt")
	if err := os.WriteFile(sysPath, []byte("自定义系统提示词"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	cfg := config.RAGConfig{SystemPromptPath: sysPath}
	tpls := loadPromptTemplates(cfg)

	if tpls.system != "自定义系统提示词" {
		t.Errorf("未加载文件模板: %q", tpls.system)
	}
	// 未配置的仍用默认
	if !strings.Contains(tpls.context, "检索到的相关资料") {
		t.Errorf("默认上下文模板未保留: %q", tpls.context)
	}
}

// AC11 降级: 文件不存在时使用默认模板
func TestLoadPromptTemplates_FileMissing(t *testing.T) {
	cfg := config.RAGConfig{SystemPromptPath: "/nonexistent/path.txt"}
	tpls := loadPromptTemplates(cfg)
	if !strings.Contains(tpls.system, "知识库") {
		t.Errorf("文件缺失应降级默认模板: %q", tpls.system)
	}
}
