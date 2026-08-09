// BinRag API
//
//	@title						BinRag API
//	@version					1.0
//	@description				BinRag 企业级文档知识库问答系统 API。所有接口需 `Authorization: Bearer <API Key>`（除 Swagger 文档页本身）。统一响应格式 `{code, message, data}`，code=0 表示成功，非 0 为业务码（400/401/404/500）。
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						Authorization
package api

import (
	_ "github.com/Bin-hy/bin-rag/internal/api/docs"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// EngineProvider 返回当前引擎（配置热重载后新请求用新引擎）
type EngineProvider func() rag.Engine

// Dependencies API 层依赖集合
type Dependencies struct {
	Config    config.ServerConfig
	LoaderCfg config.LoaderConfig
	CfgMgr    *config.ConfigManager      // 配置管理器（热重载 + 快照）
	Rebuild   func(*config.Config) error // 配置更新时的运行时组件重建回调（app 装配传入）
	Engine    EngineProvider             // 当前引擎（可热重载）
	Store     store.Store
	VS        vectorstore.VectorStore
	BM25      retriever.BM25Index
	Registry  loader.Registry
	History   store.HistoryStore
}

// handler HTTP handler 集合
type handler struct {
	cfg       config.ServerConfig
	loaderCfg config.LoaderConfig
	cfgMgr    *config.ConfigManager
	rebuild   func(*config.Config) error
	engine    EngineProvider
	store     store.Store
	vs        vectorstore.VectorStore
	bm25      retriever.BM25Index
	registry  loader.Registry
	history   store.HistoryStore
}

// NewRouter 创建 Gin 路由
func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Logger(), CORS(), RateLimit(deps.Config.RateLimitQPS))

	h := &handler{
		cfg:       deps.Config,
		loaderCfg: deps.LoaderCfg,
		cfgMgr:    deps.CfgMgr,
		rebuild:   deps.Rebuild,
		engine:    deps.Engine,
		store:     deps.Store,
		vs:        deps.VS,
		bm25:      deps.BM25,
		registry:  deps.Registry,
		history:   deps.History,
	}

	// Swagger 文档（公开访问，接口实测仍需 API Key）
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1")
	v1.Use(Auth(deps.Store, deps.Config.AuthEnabled, deps.Config.BootstrapAPIKey))

	// 配置管理（GET 任意 Key；PUT 需 bootstrap Key）
	v1.GET("/config", h.GetConfig)
	v1.PUT("/config", h.UpdateConfig)

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
	v1.GET("/chat/enhancements", h.Enhancements)
	v1.GET("/chat/history", h.GetHistory)
	v1.GET("/chunks/:id", h.GetChunk)

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
