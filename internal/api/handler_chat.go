package api

import (
	"strings"

	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/gin-gonic/gin"
)

// chatRequest 问答请求
type chatRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Question  string `json:"question" binding:"required"`
	KBID      string `json:"kb_id"` // 知识库范围，空表示不限定
}

// Chat 普通问答（返回回答与引用来源）
//
//	@Summary		问答
//	@Description	接收问题与可选的会话、知识库范围，走 RAG 编排返回回答与引用来源。默认返回 JSON；请求带 Accept: text/event-stream 或 query stream=1 时返回 SSE 流式（事件序列：sources 引用来源 → chunk×N 文本增量 → done 正常结束 / error 出错终止）
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

	result, err := h.engine.Ask(c.Request.Context(), req.SessionID, req.Question, rag.WithKBID(req.KBID))
	if err != nil {
		Fail(c, CodeInternal, "问答失败: "+err.Error())
		return
	}
	OK(c, result)
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

	events, err := h.engine.StreamAsk(c.Request.Context(), req.SessionID, req.Question, rag.WithKBID(req.KBID))
	if err != nil {
		Fail(c, CodeInternal, "启动流式问答失败: "+err.Error())
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	for ev := range events {
		switch ev.Type {
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
