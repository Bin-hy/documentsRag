package api

import (
	"log/slog"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/gin-gonic/gin"
)

// ConfigUpdateRequest 可修改配置的白名单字段（防越权改启动级配置；部分更新——nil 字段不修改）
type ConfigUpdateRequest struct {
	LLM         *LLMPatch              `json:"llm,omitempty"`
	Embedder    *EmbedderPatch         `json:"embedder,omitempty"`
	Retriever   *RetrieverPatch        `json:"retriever,omitempty"`
	Reranker    *RerankerPatch         `json:"reranker,omitempty"`
	RAGStrategy *config.StrategyConfig `json:"rag_strategy,omitempty"`
	Loader      *LoaderPatch           `json:"loader,omitempty"`
}

// LLMPatch LLM 可修改字段（指针 = 仅更新提供的字段）
type LLMPatch struct {
	Model       *string  `json:"model,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Timeout     *int     `json:"timeout,omitempty"`
}

// EmbedderPatch Embedding 可修改字段
type EmbedderPatch struct {
	Model *string `json:"model,omitempty"`
}

// RetrieverPatch 检索可修改字段
type RetrieverPatch struct {
	TopK         *int     `json:"top_k,omitempty"`
	VectorWeight *float32 `json:"vector_weight,omitempty"`
	BM25Weight   *float32 `json:"bm25_weight,omitempty"`
}

// RerankerPatch 重排可修改字段
type RerankerPatch struct {
	Model *string `json:"model,omitempty"`
	TopN  *int    `json:"top_n,omitempty"`
}

// LoaderPatch Loader 可修改字段
type LoaderPatch struct {
	MinReadableChars *int `json:"min_readable_chars,omitempty"`
}

// ConfigView 配置视图：可修改组（当前值）+ 只读组（当前值 + 需重启标记）
type ConfigView struct {
	Mutable     MutableConfigView `json:"mutable"`
	ReadOnly    []ReadOnlyConfig  `json:"read_only"`
	IsBootstrap bool              `json:"is_bootstrap"` // 当前 Key 是否为 bootstrap（前端据此允许编辑）
}

// MutableConfigView 可修改配置当前值
// LLMView LLM 视图（不含 APIKey 明文）
type LLMView struct {
	Model       string  `json:"model"`
	Temperature float32 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Timeout     int     `json:"timeout"`
}

// EmbedderView Embedding 视图（不含 APIKey 明文）
type EmbedderView struct {
	Model     string `json:"model"`
	Dimension int    `json:"dimension"`
}

// RerankerView 重排视图（不含 APIKey 明文）
type RerankerView struct {
	Model string `json:"model"`
	TopN  int    `json:"top_n"`
}

// MutableConfigView 可修改配置当前值（密钥掩码）
// RetrieverView 检索视图（snake_case json tag，与前端对齐）
type RetrieverView struct {
	TopK         int     `json:"top_k"`
	VectorWeight float32 `json:"vector_weight"`
	BM25Weight   float32 `json:"bm25_weight"`
}

// LoaderView Loader 视图
type LoaderView struct {
	MinReadableChars int `json:"min_readable_chars"`
}

// MutableConfigView 可修改配置当前值（密钥掩码）
type MutableConfigView struct {
	LLM         LLMView               `json:"llm"`
	Embedder    EmbedderView          `json:"embedder"`
	Retriever   RetrieverView         `json:"retriever"`
	Reranker    RerankerView          `json:"reranker"`
	RAGStrategy config.StrategyConfig `json:"rag_strategy"`
	Loader      LoaderView            `json:"loader"`
}

// ReadOnlyConfig 只读配置项（需重启生效）
type ReadOnlyConfig struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	NeedsRestart bool   `json:"needs_restart"`
}

// cfgSnapshot 返回当前配置快照（请求级用；配置管理器未初始化时 nil）
func (h *handler) cfgSnapshot() *config.Config {
	if h.cfgMgr == nil {
		return nil
	}
	return h.cfgMgr.Get()
}

// GetConfig 读取当前配置视图（任意已认证 Key）
//
//	@Summary		配置视图
//	@Description	读取当前配置：可修改组（LLM/Embedding/Retriever/Reranker/Strategy/Loader）+ 只读组（Postgres/VectorStore/Server，需重启生效）
//	@Tags			配置
//	@Produce		json
//	@Success		200	{object}	Response{data=ConfigView}
//	@Failure		401	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/config [get]
func (h *handler) GetConfig(c *gin.Context) {
	if h.cfgMgr == nil {
		Fail(c, CodeInternal, "配置管理器未初始化")
		return
	}
	cfg := h.cfgMgr.Current()
	if cfg == nil {
		Fail(c, CodeInternal, "配置为空")
		return
	}

	view := ConfigView{
		IsBootstrap: c.GetBool("is_bootstrap"),
		Mutable: MutableConfigView{
			LLM:         LLMView{Model: cfg.LLM.Model, Temperature: cfg.LLM.Temperature, MaxTokens: cfg.LLM.MaxTokens, Timeout: cfg.LLM.Timeout},
			Embedder:    EmbedderView{Model: cfg.Embedder.Model, Dimension: cfg.Embedder.Dimension},
			Retriever:   RetrieverView{TopK: cfg.Retriever.TopK, VectorWeight: cfg.Retriever.VectorWeight, BM25Weight: cfg.Retriever.BM25Weight},
			Reranker:    RerankerView{Model: cfg.Reranker.Model, TopN: cfg.Reranker.TopN},
			RAGStrategy: cfg.RAG.Strategy,
			Loader:      LoaderView{MinReadableChars: cfg.Loader.MinReadableChars},
		},
		ReadOnly: []ReadOnlyConfig{
			{Key: "postgres.dsn", Value: maskDSN(cfg.Postgres.DSN), NeedsRestart: true},
			{Key: "vectorstore.host", Value: cfg.VectorStore.Host, NeedsRestart: true},
			{Key: "server.port", Value: intStr(cfg.Server.Port), NeedsRestart: true},
			{Key: "server.upload_dir", Value: cfg.Server.FileStorageDir, NeedsRestart: true},
			{Key: "server.worker_count", Value: intStr(cfg.Server.WorkerCount), NeedsRestart: true},
			{Key: "chunker.strategy", Value: cfg.Chunker.Strategy, NeedsRestart: true},
		},
	}
	OK(c, view)
}

// UpdateConfig 更新可修改配置（需 bootstrap Key）：
// 白名单字段合并 → 校验 → 重建组件 → 写文件 → 原子替换（请求级快照保证新请求生效）
//
//	@Summary		更新配置
//	@Description	更新可修改配置（LLM/Embedding/Retriever/Reranker/Strategy/Loader）。需 bootstrap Key；写入文件并热重载，仅影响新请求。
//	@Tags			配置
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ConfigUpdateRequest	true	"可修改配置字段"
//	@Success		200		{object}	Response{data=ConfigView}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		403		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/config [put]
func (h *handler) UpdateConfig(c *gin.Context) {
	// bootstrap key 权限
	if !c.GetBool("is_bootstrap") {
		Fail(c, 403, "需要 bootstrap API Key 才能修改配置")
		return
	}
	if h.cfgMgr == nil {
		Fail(c, CodeInternal, "配置管理器未初始化")
		return
	}

	var req ConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	cur := h.cfgMgr.Current()
	if cur == nil {
		Fail(c, CodeInternal, "配置为空")
		return
	}
	// 深拷贝当前配置，合并白名单字段（部分更新：仅非 nil 字段）
	newCfg := *cur
	if req.LLM != nil {
		if req.LLM.Model != nil {
			newCfg.LLM.Model = *req.LLM.Model
		}
		if req.LLM.Temperature != nil {
			newCfg.LLM.Temperature = *req.LLM.Temperature
		}
		if req.LLM.MaxTokens != nil {
			newCfg.LLM.MaxTokens = *req.LLM.MaxTokens
		}
		if req.LLM.Timeout != nil {
			newCfg.LLM.Timeout = *req.LLM.Timeout
		}
	}
	if req.Embedder != nil && req.Embedder.Model != nil {
		newCfg.Embedder.Model = *req.Embedder.Model
	}
	if req.Retriever != nil {
		if req.Retriever.TopK != nil {
			newCfg.Retriever.TopK = *req.Retriever.TopK
		}
		if req.Retriever.VectorWeight != nil {
			newCfg.Retriever.VectorWeight = *req.Retriever.VectorWeight
		}
		if req.Retriever.BM25Weight != nil {
			newCfg.Retriever.BM25Weight = *req.Retriever.BM25Weight
		}
	}
	if req.Reranker != nil {
		if req.Reranker.Model != nil {
			newCfg.Reranker.Model = *req.Reranker.Model
		}
		if req.Reranker.TopN != nil {
			newCfg.Reranker.TopN = *req.Reranker.TopN
		}
	}
	if req.RAGStrategy != nil {
		newCfg.RAG.Strategy = *req.RAGStrategy
	}
	if req.Loader != nil && req.Loader.MinReadableChars != nil {
		newCfg.Loader.MinReadableChars = *req.Loader.MinReadableChars
	}

	// 更新：校验 → rebuild 试构建 → 写文件 → 原子替换
	if err := h.cfgMgr.Update(&newCfg, h.rebuild); err != nil {
		slog.Warn("配置更新失败", "err", err)
		Fail(c, CodeBadRequest, "配置更新失败: "+err.Error())
		return
	}
	slog.Info("配置更新成功", "path", h.cfgMgr.Path())

	view := ConfigView{
		IsBootstrap: true, // 能走到这里说明已通过 bootstrap 校验
		Mutable: MutableConfigView{
			LLM:         LLMView{Model: newCfg.LLM.Model, Temperature: newCfg.LLM.Temperature, MaxTokens: newCfg.LLM.MaxTokens, Timeout: newCfg.LLM.Timeout},
			Embedder:    EmbedderView{Model: newCfg.Embedder.Model, Dimension: newCfg.Embedder.Dimension},
			Retriever:   RetrieverView{TopK: newCfg.Retriever.TopK, VectorWeight: newCfg.Retriever.VectorWeight, BM25Weight: newCfg.Retriever.BM25Weight},
			Reranker:    RerankerView{Model: newCfg.Reranker.Model, TopN: newCfg.Reranker.TopN},
			RAGStrategy: newCfg.RAG.Strategy,
			Loader:      LoaderView{MinReadableChars: newCfg.Loader.MinReadableChars},
		},
	}
	OK(c, view)
}

// maskDSN 隐藏 DSN 中的密码（postgres://user:pass@host 形式）
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	// 形如 postgres://user:password@host/db —— 掩码 password 部分
	prefix := "postgres://"
	if len(dsn) <= len(prefix) || dsn[:len(prefix)] != prefix {
		return dsn
	}
	rest := dsn[len(prefix):]
	at := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '@' {
			at = i
			break
		}
	}
	if at <= 0 {
		return dsn
	}
	userinfo := rest[:at]
	colon := -1
	for i := 0; i < len(userinfo); i++ {
		if userinfo[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return dsn
	}
	masked := prefix + userinfo[:colon+1] + "****" + rest[at:]
	return masked
}

func intStr(v int) string {
	return intToString(v)
}

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
