// Package mcp 提供 MCP Server（streamable HTTP，只读 RAG 能力服务）。
//
// 认证与授权边界（spec F3/F4/F5）：
//   - 认证失败（缺失/无效/停用 Key）→ HTTP 401，不进入 JSON-RPC
//   - 授权失败（Tool 白名单外 / 知识库越权 / 任务越权）→ JSON-RPC error -32001
//   - 越权与不存在统一消息，不泄露资源存在性（spec N2）
//
// 探针结论（task T1）：mcp-go v0.57.0 的 tools/call handler 返回 error 时固定映射为
// -32603（InternalError），无法经 handler/middleware 返回自定义错误码。因此 -32001
// 由本包网关层（server.go 的请求拦截）在转发给 mcp-go 前直接构造 JSON-RPC error 响应。
package mcp

import (
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// 授权失败 JSON-RPC 错误码（区别于认证失败 HTTP 401；MCP 协议层无 403 概念）
const ErrCodePermissionDenied = -32001

// 统一消息（越权与不存在同一消息，不泄露存在性，spec N2）
const (
	msgToolForbidden = "无权限执行该操作"
	msgKBForbidden   = "知识库不存在或无权限"
	msgTaskForbidden = "任务不存在"
)

// PermissionError 授权失败错误（网关层 / 权限层共用）。
// 实现 mcp-go 的 error 接口；网关层用 ToJSONRPCError(id) 构造 -32001 响应。
type PermissionError struct {
	message string
}

// Error 实现 error 接口
func (e *PermissionError) Error() string { return e.message }

// Message 返回统一错误消息
func (e *PermissionError) Message() string { return e.message }

// NewToolForbidden Tool 白名单外（spec F4）
func NewToolForbidden() *PermissionError { return &PermissionError{message: msgToolForbidden} }

// NewKBForbidden 知识库越权/不存在（spec F5，统一消息）
func NewKBForbidden() *PermissionError { return &PermissionError{message: msgKBForbidden} }

// NewTaskForbidden 任务越权/不存在（spec F8，统一消息）
func NewTaskForbidden() *PermissionError { return &PermissionError{message: msgTaskForbidden} }

// ToJSONRPCError 构造 -32001 JSON-RPC error 响应体（网关层写回客户端用）
func (e *PermissionError) ToJSONRPCError(id any) mcpgo.JSONRPCError {
	return mcpgo.JSONRPCError{
		JSONRPC: mcpgo.JSONRPC_VERSION,
		ID:      mcpgo.NewRequestId(id),
		Error: mcpgo.NewJSONRPCErrorDetails(
			ErrCodePermissionDenied,
			e.message,
			nil,
		),
	}
}

// String 便于日志
func (e *PermissionError) String() string {
	return fmt.Sprintf("mcp permission denied (-%d): %s", -ErrCodePermissionDenied, e.message)
}
