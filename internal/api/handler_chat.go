package api

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/gin-gonic/gin"
)

// chatRequest 问答请求
type chatRequest struct {
	SessionID string                 `json:"session_id" binding:"required"`
	Question  string                 `json:"question" binding:"required"`
	KBID      string                 `json:"kb_id"`              // 知识库范围，空表示不限定
	Strategy  *config.StrategyConfig `json:"strategy,omitempty"` // 单次请求策略覆盖
	Enhanced  bool                   `json:"enhanced,omitempty"` // 增强模式（function calling 工具，如 web_search）
}

// resolveKBScope 解析知识库检索范围，返回 (单值 kbID, 多值 kbIDs, 是否继续处理)。
//   - 指定 kb_id：校验访问权（越权 404），返回单值
//   - 登录用户（JWT）未指定：展开为其名下全部知识库（owner_id = 用户ID），
//     不包含系统级库与其他用户的私有库（与 ListKBs / canAccessKB 的隔离语义一致）；名下无知识库时 400
//   - API Key 未指定：返回空（系统级"不限"：kbFilter 不过滤）
func (h *handler) resolveKBScope(c *gin.Context, reqKBID string) (string, []string, bool) {
	if reqKBID != "" {
		if !h.ensureKBAccess(c, reqKBID) {
			Fail(c, CodeNotFound, "知识库不存在")
			return "", nil, false
		}
		return reqKBID, nil, true
	}

	id := auth.IdentityOf(c)
	if id.Kind != auth.KindUser {
		return "", nil, true // API Key / 匿名：不限定
	}

	kbs, err := h.store.ListKBsByOwner(c.Request.Context(), id.UserID)
	if err != nil {
		slog.Error("查询用户知识库失败", "user", id.UserID, "err", err)
		Fail(c, CodeInternal, "查询可访问知识库失败")
		return "", nil, false
	}
	if len(kbs) == 0 {
		Fail(c, CodeBadRequest, "当前账号没有可访问的知识库，请先创建或指定知识库")
		return "", nil, false
	}
	ids := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		ids = append(ids, kb.ID)
	}
	return "", ids, true
}

// Chat 普通问答（返回回答与引用来源）
//
//	@Summary		问答
//	@Description	接收问题与可选的会话、知识库范围，走 RAG 编排返回回答与引用来源。默认返回 JSON；请求带 Accept: text/event-stream 或 query stream=1 时返回 SSE 流式（事件序列：thinking×N 思考链路 → sources 引用来源 → chunk×N 文本增量 → done 正常结束 / error 出错终止；思考链路开启时才有 thinking 事件）
//	@Tags			问答
//	@Accept			json
//	@Produce		json
//	@Produce		text/event-stream
//	@Param			body	body		chatRequest	true	"问答请求"
//	@Success		200		{object}	Response{data=rag.RAGResult}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/chat [post]
func (h *handler) Chat(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	eng := h.engine()
	if eng == nil {
		Fail(c, CodeInternal, "引擎未初始化")
		return
	}
	// 知识库范围：指定 kb_id 则校验访问权；登录用户未指定则展开为其可访问的全部知识库（防跨租户越权）
	kbID, kbIDs, ok := h.resolveKBScope(c, req.KBID)
	if !ok {
		return
	}
	var snap *config.Config
	if h.cfgMgr != nil {
		snap = h.cfgMgr.Get() // 请求级配置快照（热重载一致性）
	}
	askOpts := []rag.AskOption{
		// 多库展开（KBIDs）时策略按原始 kb_id（空 → 全局）取用，各库策略暂不合并
		rag.WithStrategy(h.kbStrategy(c, req.KBID), req.Strategy),
		rag.WithConfigSnapshot(snap), rag.WithThinking(true), rag.WithEnhanced(req.Enhanced),
	}
	if kbID != "" {
		askOpts = append(askOpts, rag.WithKBID(kbID))
	} else if len(kbIDs) > 0 {
		askOpts = append(askOpts, rag.WithKBIDs(kbIDs))
	}
	result, err := eng.Ask(c.Request.Context(), req.SessionID, req.Question, askOpts...)
	if err != nil {
		Fail(c, CodeInternal, "问答失败: "+err.Error())
		return
	}
	OK(c, result)
}

// kbStrategy 读取知识库级策略（JSON 解析失败返回 nil，用全局默认）
func (h *handler) kbStrategy(c *gin.Context, kbID string) *config.StrategyConfig {
	if kbID == "" {
		return nil
	}
	kb, err := h.store.GetKB(c.Request.Context(), kbID)
	if err != nil || kb.Strategy == "" {
		return nil
	}
	var s config.StrategyConfig
	if err := json.Unmarshal([]byte(kb.Strategy), &s); err != nil {
		slog.Warn("知识库策略解析失败，用全局默认", "kb_id", kbID, "err", err)
		return nil
	}
	return &s
}

// ChatStream SSE 流式问答：事件 sources → chunk×N → done（或 error）
// 触发条件：Accept 头含 text/event-stream 或 query 带 stream=1
// 注：本 handler 与 Chat 同路径同方法，文档合并到 Chat 的注解（OpenAPI 不允许同 path+method 多 operation）；此处仅保留函数注释
func (h *handler) ChatStream(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	// 知识库范围：指定 kb_id 则校验访问权；登录用户未指定则展开为其可访问的全部知识库（防跨租户越权）
	kbID, kbIDs, ok := h.resolveKBScope(c, req.KBID)
	if !ok {
		return
	}
	// 思考链路：thinking 事件由 engine 内部转发到事件流（开启时），此处只做分发（G2）
	askOpts := []rag.AskOption{
		// 多库展开（KBIDs）时策略按原始 kb_id（空 → 全局）取用，各库策略暂不合并
		rag.WithStrategy(h.kbStrategy(c, req.KBID), req.Strategy),
		rag.WithConfigSnapshot(h.cfgSnapshot()), rag.WithThinking(true), rag.WithEnhanced(req.Enhanced),
	}
	if kbID != "" {
		askOpts = append(askOpts, rag.WithKBID(kbID))
	} else if len(kbIDs) > 0 {
		askOpts = append(askOpts, rag.WithKBIDs(kbIDs))
	}
	events, err := h.engine().StreamAsk(c.Request.Context(), req.SessionID, req.Question, askOpts...)
	if err != nil {
		Fail(c, CodeInternal, "启动流式问答失败: "+err.Error())
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	for ev := range events {
		switch ev.Type {
		case rag.EventThinking:
			c.SSEvent("thinking", ev.Thinking)
		case rag.EventSources:
			c.SSEvent("sources", ev.Sources)
		case rag.EventChunk:
			c.SSEvent("chunk", gin.H{"content": ev.Content})
		case rag.EventDone:
			c.SSEvent("done", gin.H{})
			c.Writer.Flush()
			return
		case rag.EventError:
			c.SSEvent("error", gin.H{"message": ev.Err.Error()})
			c.Writer.Flush()
			return
		}
		c.Writer.Flush()
	}
}

// isStreamRequest 判断是否流式请求
func isStreamRequest(c *gin.Context) bool {
	if c.Query("stream") == "1" {
		return true
	}
	accept := c.GetHeader("Accept")
	return strings.Contains(accept, "text/event-stream")
}

// Enhancements 增强能力列表（前端增强面板动态渲染，多能力预留）
//
//	@Summary		增强能力列表
//	@Description	返回可用增强能力。当前能力：web_search（联网搜索）；available 表示后端是否已配置就绪（配置了 web_search.api_key）
//	@Tags			问答
//	@Success		200	{object}	Response{data=map[string]any}
//	@Security		ApiKeyAuth
//	@Router			/api/v1/chat/enhancements [get]
func (h *handler) Enhancements(c *gin.Context) {
	var webAvailable bool
	if h.cfgMgr != nil {
		if cfg := h.cfgMgr.Get(); cfg != nil {
			webAvailable = cfg.WebSearch.APIKey != ""
		}
	}
	OK(c, gin.H{"enhancements": []gin.H{
		{
			"key":         "web_search",
			"label":       "联网搜索",
			"description": "搜索互联网获取实时信息，补充知识库未覆盖的内容",
			"available":   webAvailable,
		},
	}})
}
