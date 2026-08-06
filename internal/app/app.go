// Package app 提供 BinRag 服务装配：存储、入库 worker、RAG 引擎与 HTTP 路由的统一构建。
// Web 形态（cmd/server）与桌面形态（cmd/desktop）共用本包，保证两条启动路径行为一致。
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/Bin-hy/bin-rag/internal/api"
	"github.com/Bin-hy/bin-rag/internal/chunker"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/embedding"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/pipeline"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/reranker"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/Bin-hy/bin-rag/internal/task"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
	"github.com/Bin-hy/bin-rag/internal/webui"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// App 已装配的应用实例，持有全部运行时依赖与生命周期。
type App struct {
	cfg    config.Config
	st     store.Store
	vs     vectorstore.VectorStore
	bm25   retriever.BM25Index
	engine rag.Engine
	worker task.WorkerPool

	router *gin.Engine
	cancel context.CancelFunc
}

// New 装配完整应用：连接 PostgreSQL → 迁移 → bootstrap Key → 基础设施
// → 入库 pipeline/worker → RAG 引擎 → HTTP 路由。
func New(cfg *config.Config) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// PostgreSQL 元数据存储
	st, err := store.NewStore(ctx, cfg.Postgres.DSN)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}

	if err := st.Migrate(ctx); err != nil {
		cancel()
		st.Close()
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	if err := seedAPIKey(ctx, st, cfg.Server.BootstrapAPIKey); err != nil {
		cancel()
		st.Close()
		return nil, fmt.Errorf("初始化 API Key 失败: %w", err)
	}

	// 基础设施
	emb := embedding.NewEmbedder(cfg.Embedder)
	vs, err := vectorstore.NewQdrantStore(cfg.VectorStore)
	if err != nil {
		cancel()
		st.Close()
		return nil, fmt.Errorf("初始化向量存储失败: %w", err)
	}
	// 确保 Qdrant 集合存在（首次启动自动创建）
	if err := vs.EnsureCollection(ctx); err != nil {
		cancel()
		st.Close()
		return nil, fmt.Errorf("初始化向量集合失败: %w", err)
	}
	bm25 := retriever.NewBM25Index(retriever.NewSimpleTokenizer())

	pipe := pipeline.NewPipeline(
		loader.NewLoader(),
		chunker.NewChunker(nil),
		emb,
		vs,
		chunker.ChunkerConfig{
			Strategy:     chunker.ParseStrategy(cfg.Chunker.Strategy),
			ChunkSize:    cfg.Chunker.ChunkSize,
			ChunkOverlap: cfg.Chunker.ChunkOverlap,
			HeadingLevel: cfg.Chunker.HeadingLevel,
		},
		bm25,
	)

	// 入库 worker
	worker := task.NewWorkerPool(cfg.Server, st, pipe)
	worker.Start(ctx)

	// RAG 编排
	llmClient := llm.NewLLM(cfg.LLM)
	rr := reranker.NewReranker(cfg.Reranker)
	rt := retriever.NewRetriever(cfg.Retriever, emb, vs, bm25, rr)
	engine := rag.NewEngine(cfg.RAG, llmClient, rt, &ragHistoryAdapter{inner: st.HistoryStore()})

	// HTTP 路由（API + 前端静态托管）
	router := api.NewRouter(api.Dependencies{
		Config:   cfg.Server,
		Store:    st,
		VS:       vs,
		BM25:     bm25,
		Registry: loader.NewDefaultRegistry(),
		Engine:   engine,
		History:  st.HistoryStore(),
	})
	if err := webui.Register(router); err != nil {
		cancel()
		st.Close()
		worker.Shutdown()
		return nil, fmt.Errorf("挂载前端静态资源失败: %w", err)
	}

	return &App{
		cfg:    *cfg,
		st:     st,
		vs:     vs,
		bm25:   bm25,
		engine: engine,
		worker: worker,
		router: router,
		cancel: cancel,
	}, nil
}

// Router 返回装配完成的 HTTP 路由（含 API 与前端静态托管，见 RegisterWebUI）。
func (a *App) Router() *gin.Engine {
	return a.router
}

// Close 优雅释放资源：停止 worker（等待当前任务）、关闭数据库连接、取消上下文。
func (a *App) Close() error {
	a.worker.Shutdown()
	a.cancel()
	a.st.Close()
	return nil
}

// ParseConfigFlag 从命令行参数解析 -c / --config 指定的配置文件路径。
// 两个别名指向同一变量；未指定、缺值或解析失败时返回空串（回退环境变量/默认路径）。
func ParseConfigFlag(args []string) string {
	fs := flag.NewFlagSet("binrag", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var path string
	fs.StringVar(&path, "c", "", "配置文件路径")
	fs.StringVar(&path, "config", "", "配置文件路径")

	_ = fs.Parse(args) // 本项目无其他 flag，解析错误静默忽略
	return path
}

// seedAPIKey 首次启动时用配置的 bootstrap Key 种子（表空时）
func seedAPIKey(ctx context.Context, st store.Store, bootstrap string) error {
	if bootstrap == "" {
		return nil
	}
	keys, err := st.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return nil
	}

	sum := sha256.Sum256([]byte(bootstrap))
	key := store.APIKey{
		ID:      uuid.New().String(),
		Name:    "bootstrap",
		KeyHash: hex.EncodeToString(sum[:]),
		Enabled: true,
	}
	if err := st.CreateAPIKey(ctx, key); err != nil {
		return err
	}
	slog.Warn("已创建 bootstrap API Key，请立即从配置中移除 bootstrap_api_key 项")
	return nil
}

// ragHistoryAdapter 将 store.HistoryStore（带 ctx）适配为 rag.HistoryStore（无 ctx）
type ragHistoryAdapter struct {
	inner store.HistoryStore
}

func (a *ragHistoryAdapter) Append(sessionID string, role string, content string) error {
	return a.inner.Append(context.Background(), sessionID, role, content)
}

func (a *ragHistoryAdapter) Get(sessionID string, limit int) ([]llm.Message, error) {
	return a.inner.Get(context.Background(), sessionID, limit)
}

func (a *ragHistoryAdapter) Clear(sessionID string) error {
	return a.inner.Clear(context.Background(), sessionID)
}
