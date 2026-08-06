package store

import (
	"context"

	"github.com/Bin-hy/bin-rag/internal/llm"
)

// HistoryStore 对话历史存储（带 ctx，适配 DB 访问）。
// 签名与 rag.HistoryStore 对齐（rag 侧无 ctx，装配时用适配器包装）。
type HistoryStore interface {
	Append(ctx context.Context, sessionID string, role string, content string, sources string) error // sources = 引用来源 JSON 字符串（空 = 无）
	Get(ctx context.Context, sessionID string, limit int) ([]llm.Message, error)                     // 最近 limit 条，时间正序
	Clear(ctx context.Context, sessionID string) error
}

// PostgresHistoryStore 基于 chat_history 表的实现
type PostgresHistoryStore struct {
	pool queryExecutor
}

// Append 追加一条消息
func (h *PostgresHistoryStore) Append(ctx context.Context, sessionID string, role string, content string, sources string) error {
	_, err := h.pool.Exec(ctx,
		`INSERT INTO chat_history (session_id, role, content, sources) VALUES ($1, $2, $3, $4)`,
		sessionID, role, content, sources,
	)
	return err
}

// Get 返回最近 limit 条（时间正序）；limit <= 0 返回全部
func (h *PostgresHistoryStore) Get(ctx context.Context, sessionID string, limit int) ([]llm.Message, error) {
	var query string
	var args []any
	if limit > 0 {
		query = `SELECT role, content, sources FROM (
			SELECT role, content, sources, created_at FROM chat_history
			WHERE session_id = $1 ORDER BY created_at DESC LIMIT $2
		) sub ORDER BY created_at ASC`
		args = []any{sessionID, limit}
	} else {
		query = `SELECT role, content, sources FROM chat_history
			WHERE session_id = $1 ORDER BY created_at ASC`
		args = []any{sessionID}
	}

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]llm.Message, 0)
	for rows.Next() {
		var m llm.Message
		if err := rows.Scan(&m.Role, &m.Content, &m.Sources); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// Clear 清空会话历史
func (h *PostgresHistoryStore) Clear(ctx context.Context, sessionID string) error {
	_, err := h.pool.Exec(ctx, `DELETE FROM chat_history WHERE session_id = $1`, sessionID)
	return err
}
