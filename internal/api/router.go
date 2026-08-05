package api

import (
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
	"github.com/gin-gonic/gin"
)

// Dependencies API 层依赖集合
type Dependencies struct {
	Config   config.ServerConfig
	Store    store.Store
	VS       vectorstore.VectorStore
	BM25     retriever.BM25Index
	Registry loader.Registry
	Engine   rag.Engine
	History  store.HistoryStore
}

// handler HTTP handler 集合
type handler struct {
	cfg      config.ServerConfig
	store    store.Store
	vs       vectorstore.VectorStore
	bm25     retriever.BM25Index
	registry loader.Registry
	engine   rag.Engine
	history  store.HistoryStore
}

// NewRouter 创建 Gin 路由
func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Logger(), CORS(), RateLimit(deps.Config.RateLimitQPS))

	h := &handler{
		cfg:      deps.Config,
		store:    deps.Store,
		vs:       deps.VS,
		bm25:     deps.BM25,
		registry: deps.Registry,
		engine:   deps.Engine,
		history:  deps.History,
	}

	v1 := r.Group("/api/v1")
	v1.Use(Auth(deps.Store, deps.Config.AuthEnabled))

	// 知识库
	v1.POST("/knowledge-bases", h.CreateKB)
	v1.GET("/knowledge-bases", h.ListKBs)
	v1.GET("/knowledge-bases/:id", h.GetKB)
	v1.PUT("/knowledge-bases/:id", h.UpdateKB)
	v1.DELETE("/knowledge-bases/:id", h.DeleteKB)

	// 文档
	v1.POST("/documents/upload", h.UploadDocument)
	v1.GET("/documents", h.ListDocuments)
	v1.DELETE("/documents/:id", h.DeleteDocument)

	// 任务
	v1.GET("/tasks/:id", h.GetTask)
	v1.POST("/tasks/:id/retry", h.RetryTask)

	// 问答（stream=1 或 Accept: text/event-stream 时 SSE 流式）
	v1.POST("/chat", h.ChatDispatch)
	v1.GET("/chat/history", h.GetHistory)

	// API Key 管理
	v1.POST("/api-keys", h.CreateAPIKey)
	v1.GET("/api-keys", h.ListAPIKeys)
	v1.DELETE("/api-keys/:id", h.DeleteAPIKey)
	v1.POST("/api-keys/:id/toggle", h.ToggleAPIKey)

	return r
}

// ChatDispatch 按请求特征分流普通/流式问答
func (h *handler) ChatDispatch(c *gin.Context) {
	if isStreamRequest(c) {
		h.ChatStream(c)
		return
	}
	h.Chat(c)
}
