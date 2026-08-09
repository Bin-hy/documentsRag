package rag

import (
	"context"
	"sync"
)

// maxToolRounds 增强模式 tool loop 的最大轮次（防死循环）
const maxToolRounds = 3

// maxToolResultPreviewLen 工具结果在思考链路中的预览长度（字符）
const maxToolResultPreviewLen = 200

// Tool 工具接口（增强模式 function calling 用）。
// 实现：工具名 / 描述 / JSON Schema 参数 / 执行（入参为 JSON 字符串，返回文本结果回传 LLM）。
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any // JSON Schema
	Execute(ctx context.Context, argsJSON string) (string, error)
}

// StructuredTool 可选接口：工具可同时返回结构化条目（如搜索结果标题/链接/摘要），
// 供思考链路完整展示；未实现时思考链路仅展示文本摘要（Result）。
type StructuredTool interface {
	// ExecuteStructured 与 Execute 语义一致，额外返回结构化条目
	ExecuteStructured(ctx context.Context, argsJSON string) (string, []ToolStepItem, error)
}

// ToolRegistry 工具注册中心（并发安全）
type ToolRegistry interface {
	Register(t Tool)
	Get(name string) (Tool, bool)
	List() []Tool
}

type defaultToolRegistry struct {
	mu sync.RWMutex
	m  map[string]Tool
}

// NewToolRegistry 创建工具注册中心
func NewToolRegistry() ToolRegistry {
	return &defaultToolRegistry{m: make(map[string]Tool)}
}

func (r *defaultToolRegistry) Register(t Tool) {
	if t == nil || t.Name() == "" {
		return
	}
	r.mu.Lock()
	r.m[t.Name()] = t
	r.mu.Unlock()
}

func (r *defaultToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	t, ok := r.m[name]
	r.mu.RUnlock()
	return t, ok
}

func (r *defaultToolRegistry) List() []Tool {
	r.mu.RLock()
	out := make([]Tool, 0, len(r.m))
	for _, t := range r.m {
		out = append(out, t)
	}
	r.mu.RUnlock()
	return out
}
