package api

import (
	"encoding/json"
	"log/slog"
	"strings"

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
	var snap *config.Config
	if h.cfgMgr != nil {
		snap = h.cfgMgr.Get() // 请求级配置快照（热重载一致性）
	}
	result, err := eng.Ask(c.Request.Context(), req.SessionID, req.Question,
		rag.WithKBID(req.KBID), rag.WithStrategy(h.kbStrategy(c, req.KBID), req.Strategy),
		rag.WithConfigSnapshot(snap), rag.WithThinking(true))
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

	// 思考链路：thinking 事件由 engine 内部转发到事件流（开启时），此处只做分发（G2）
	events, err := h.engine().StreamAsk(c.Request.Context(), req.SessionID, req.Question,
		rag.WithKBID(req.KBID), rag.WithStrategy(h.kbStrategy(c, req.KBID), req.Strategy),
		rag.WithConfigSnapshot(h.cfgSnapshot()), rag.WithThinking(true))
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
