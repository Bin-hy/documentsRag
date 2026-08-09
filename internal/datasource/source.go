// Package datasource 提供数据源抽象与注册中心。
//
// 数据源是 RAG 检索的数据来源（如向量知识库、web 搜索等）。
// 新增数据源只需：
//  1. 实现 Source 接口（Name / Available / Search）；
//  2. 在注册中心 Register 一行。
//
// 内置数据源：vector_store（默认，可用）、web_search（占位，未实现）。
package datasource

import (
	"context"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// 内置数据源名称
const (
	// SourceVectorStore 向量知识库数据源（默认，始终可用）
	SourceVectorStore = "vector_store"
	// SourceWebSearch web 搜索数据源（占位，未实现，Available()==false）
	SourceWebSearch = "web_search"
)

// SearchRequest 数据源检索请求
type SearchRequest struct {
	Query  string
	TopK   int
	Filter map[string]any // 知识库范围等过滤条件（如 kb_id）
}

// Source 数据源接口：新增数据源只需实现此接口并在注册中心注册。
// Search 返回统一结构的检索结果（与向量检索结果同构，便于上下文组装）。
type Source interface {
	// Name 数据源名称（如 vector_store / web_search）
	Name() string
	// Available 是否已实现可用（占位/未实现的源返回 false，路由会降级）
	Available() bool
	// Search 执行检索，返回检索结果（结果 Metadata 可携带 source_type 等来源信息）
	Search(ctx context.Context, req SearchRequest) ([]retriever.RetrieveResult, error)
}
