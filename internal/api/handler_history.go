package api

import "github.com/gin-gonic/gin"

// GetHistory 按会话查询对话历史
//
//	@Summary		对话历史
//	@Description	按会话 ID 查询对话历史消息，时间正序
//	@Tags			问答
//	@Produce		json
//	@Param			session_id	query		string	true	"会话 ID"
//	@Success		200			{object}	Response{data=[]llm.Message}
//	@Failure		400			{object}	Response
//	@Failure		401			{object}	Response
//	@Failure		500			{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/chat/history [get]
func (h *handler) GetHistory(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		Fail(c, CodeBadRequest, "缺少 session_id")
		return
	}

	msgs, err := h.history.Get(c.Request.Context(), sessionID, 0)
	if err != nil {
		Fail(c, CodeInternal, "查询对话历史失败")
		return
	}
	OK(c, msgs)
}
