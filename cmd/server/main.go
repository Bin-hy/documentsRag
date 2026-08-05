// BinRag API 服务入口：装配存储、入库 worker、RAG 引擎与 HTTP 路由。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/google/uuid"
)

// parseConfigFlag 从命令行参数解析 -c / --config 指定的配置文件路径。
// 两个别名指向同一变量；未指定、缺值或解析失败时返回空串（回退环境变量/默认路径）。
func parseConfigFlag(args []string) string {
	fs := flag.NewFlagSet("binrag", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var path string
	fs.StringVar(&path, "c", "", "配置文件路径")
	fs.StringVar(&path, "config", "", "配置文件路径")

	_ = fs.Parse(args) // 本项目无其他 flag，解析错误静默忽略
	return path
}

func main() {
	cfgPath := parseConfigFlag(os.Args[1:])
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err, "path", cfgPath)
		os.Exit(1)
	}
	loadedPath := cfgPath
	if loadedPath == "" {
		loadedPath = "(环境变量或默认路径)"
	}
	slog.Info("配置加载完成", "path", loadedPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// PostgreSQL 元数据存储
	st, err := store.NewStore(ctx, cfg.Postgres.DSN)
	if err != nil {
		slog.Error("连接 PostgreSQL 失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		slog.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	if err := seedAPIKey(ctx, st, cfg.Server.BootstrapAPIKey); err != nil {
		slog.Error("初始化 API Key 失败", "err", err)
		os.Exit(1)
	}

	// 基础设施
	emb := embedding.NewEmbedder(cfg.Embedder)
	vs, err := vectorstore.NewQdrantStore(cfg.VectorStore)
	if err != nil {
		slog.Error("初始化向量存储失败", "err", err)
		os.Exit(1)
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
	defer worker.Shutdown()

	// RAG 编排
	llmClient := llm.NewLLM(cfg.LLM)
	rr := reranker.NewReranker(cfg.Reranker)
	rt := retriever.NewRetriever(cfg.Retriever, emb, vs, bm25, rr)
	engine := rag.NewEngine(cfg.RAG, llmClient, rt, &ragHistoryAdapter{inner: st.HistoryStore()})

	// HTTP 服务
	router := api.NewRouter(api.Dependencies{
		Config:   cfg.Server,
		Store:    st,
		VS:       vs,
		BM25:     bm25,
		Registry: loader.NewDefaultRegistry(),
		Engine:   engine,
		History:  st.HistoryStore(),
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	go func() {
		slog.Info("HTTP 服务已启动",
			"addr", server.Addr,
			"worker", cfg.Server.WorkerCount,
			"storage", cfg.Server.FileStorageDir,
			"auth", cfg.Server.AuthEnabled,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常退出", "err", err)
			cancel()
		}
	}()

	// 优雅关停：先停 worker（等待当前任务），再关 HTTP
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("收到退出信号，开始优雅关停")

	worker.Shutdown()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP 关停超时", "err", err)
	}
	slog.Info("服务已退出")
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
