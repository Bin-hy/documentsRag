package app

import (
	"fmt"
	"sync/atomic"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/datasource"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/reranker"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/search"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
)

// RuntimeComponents 可热重建的运行时组件（配置修改时整体重建）
type RuntimeComponents struct {
	LLM       llm.LLM
	Embedder  embedding.Embedder
	Retriever retriever.Retriever
	Engine    rag.Engine
}

// BuildRuntime 按配置构建运行时组件（热重载用）。
// 任一组件构建失败返回 error，调用方保持旧组件（回滚）。
// vs/bm25/history 为启动级组件（不随配置重建），复用传入。
func BuildRuntime(
	cfg *config.Config,
	vs vectorstore.VectorStore,
	bm25 retriever.BM25Index,
	history rag.HistoryStore,
) (*RuntimeComponents, error) {
	emb, err := embedding.NewEmbedder(cfg.Embedder)
	if err != nil {
		return nil, fmt.Errorf("初始化 Embedder 失败: %w", err)
	}
	llmClient := llm.NewLLM(cfg.LLM)
	rr := reranker.NewReranker(cfg.Reranker)
	rt := retriever.NewRetriever(cfg.Retriever, emb, vs, bm25, rr)
	// 数据源注册中心：注册内置源（向量库可用 + web 占位）；后续 MCP/自定义数据源在此动态注册后注入
	reg := datasource.NewRegistry()
	reg.Register(datasource.NewVectorStoreSource(rt))
	reg.Register(datasource.NewWebSearchSource())
	// 联网搜索提供者（增强模式 web_search 工具；未配置 api_key 时不可用）
	searchProvider := search.New(cfg.WebSearch)
	engine := rag.NewEngine(cfg.RAG, llmClient, rt, history, emb,
		rag.WithSources(reg), rag.WithSearchProvider(searchProvider))

	return &RuntimeComponents{
		LLM:       llmClient,
		Embedder:  emb,
		Retriever: rt,
		Engine:    engine,
	}, nil
}

// rebuildComponents 按新配置构建组件并原子替换 components。
// 自由函数（非 App 方法），供 New 中的 Rebuild 闭包和 App 方法共用，消除重复逻辑。
func rebuildComponents(
	newCfg *config.Config,
	vs vectorstore.VectorStore,
	bm25 retriever.BM25Index,
	historyAdapter *ragHistoryAdapter,
	components *atomic.Pointer[RuntimeComponents],
) error {
	rt, err := BuildRuntime(newCfg, vs, bm25, historyAdapter)
	if err != nil {
		return fmt.Errorf("构建运行时组件失败: %w", err)
	}
	components.Store(rt)
	return nil
}
