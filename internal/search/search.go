// Package search 提供可插拔的联网搜索提供者抽象。
//
// 新增搜索后端只需：
//  1. 实现 Provider 接口（Name / Available / Search）；
//  2. 在 New 工厂中按 provider 名称注册。
//
// 第一版内置实现：bocha（博查 AI 搜索，国内，中文友好）。
package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// Result 搜索结果（与搜索引擎后端解耦的统一结构）
type Result struct {
	Title   string // 标题
	URL     string // 链接
	Snippet string // 摘要
	Content string // 完整内容（可为空，取决于后端返回）
	Site    string // 站点名
}

// Options 搜索选项
type Options struct {
	Count     int    // 返回条数（0 用配置默认）
	Freshness string // 时效：noLimit / day / week / month / year（空 = noLimit）
}

// Provider 搜索服务提供者接口
type Provider interface {
	// Name 提供者名称（如 bocha）
	Name() string
	// Available 配置是否就绪（如 api_key 已填；未就绪时增强面板应标记不可用）
	Available() bool
	// Search 执行搜索，返回结果列表
	Search(ctx context.Context, query string, opts Options) ([]Result, error)
}

// New 按配置创建搜索提供者（可插拔：provider 字段选择实现）
func New(cfg config.WebSearchConfig) Provider {
	switch strings.ToLower(cfg.Provider) {
	case "", "bocha":
		return NewBochaProvider(cfg)
	default:
		return &unavailableProvider{name: cfg.Provider}
	}
}

// unavailableProvider 未实现/未知提供者（占位）
type unavailableProvider struct {
	name string
}

func (p *unavailableProvider) Name() string    { return p.name }
func (p *unavailableProvider) Available() bool { return false }
func (p *unavailableProvider) Search(ctx context.Context, query string, opts Options) ([]Result, error) {
	return nil, fmt.Errorf("搜索提供者 %q 未实现", p.name)
}
