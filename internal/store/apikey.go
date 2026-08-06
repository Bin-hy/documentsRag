package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateAPIKey 创建 API Key（只存 hash）
func (s *pgStore) CreateAPIKey(ctx context.Context, k APIKey) error {
	if k.ID == "" || k.KeyHash == "" {
		return fmt.Errorf("API Key ID 与 hash 不能为空")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, key_hash, enabled, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		k.ID, k.Name, k.KeyHash, k.Enabled, time.Now(),
	)
	return err
}

// ListAPIKeys 列出 API Key（不含 hash 由调用方决定展示，此处返回全部字段）
func (s *pgStore) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, key_hash, enabled, last_used_at, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Enabled, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// GetAPIKeyByHash 按 hash 查询（认证中间件使用）
func (s *pgStore) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, key_hash, enabled, last_used_at, created_at FROM api_keys WHERE key_hash = $1`, hash,
	).Scan(&k.ID, &k.Name, &k.KeyHash, &k.Enabled, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &k, nil
}

// SetAPIKeyEnabled 启用/停用
func (s *pgStore) SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET enabled = $2 WHERE id = $1`, id, enabled)
	return err
}

// DeleteAPIKey 删除
func (s *pgStore) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}

// TouchAPIKey 更新最后使用时间
func (s *pgStore) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id)
	return err
}
