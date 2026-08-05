package api

import "github.com/gin-gonic/gin"

// 业务错误码（HTTP 状态码与业务码一致，便于前端处理）
const (
	CodeOK           = 0
	CodeBadRequest   = 400
	CodeUnauthorized = 401
	CodeNotFound     = 404
	CodeInternal     = 500
)

// Response 统一响应格式
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// OK 成功响应
func OK(c *gin.Context, data any) {
	c.JSON(200, Response{Code: CodeOK, Message: "ok", Data: data})
}

// Fail 失败响应（HTTP 状态码与业务码一致）
func Fail(c *gin.Context, code int, message string) {
	c.JSON(code, Response{Code: code, Message: message})
}
