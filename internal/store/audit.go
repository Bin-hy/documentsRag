package store

import (
	"context"
	"time"
)

// AppendAuditLog 写入一条 MCP 调用审计记录。
// 仅记录 api_key_id 引用与截断后的参数；表结构不含 Secret/Token 列（spec F7）。
func (s *pgStore) AppendAuditLog(ctx context.Context, log AuditLog) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO mcp_audit_logs (api_key_id, tool_name, params, params_len, status, error_message, duration_ms, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		log.APIKeyID, log.ToolName, log.Params, log.ParamsLen,
		log.Status, log.ErrorMessage, log.DurationMS, time.Now(),
	)
	return err
}
