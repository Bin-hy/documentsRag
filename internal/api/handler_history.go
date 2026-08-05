package api

import "github.com/gin-gonic/gin"

// GetHistory 按会话查询对话历史
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
