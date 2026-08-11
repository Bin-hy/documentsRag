package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// apiKeyColumns api_keys 查询列（与 scanAPIKey 顺序一致）
const apiKeyColumns = "id, name, key_hash, enabled, last_used_at, created_at, owner_id, mcp_tools, mcp_kb_scope, mcp_kb_ids"

// scanAPIKey 扫描一行 API Key（owner_id 为 NULL 时映射为空串）
type rowScanner interface{ Scan(dest ...any) error }

func scanAPIKey(r rowScanner) (*APIKey, error) {
	var k APIKey
	var owner sql.NullString
	if err := r.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Enabled, &k.LastUsedAt, &k.CreatedAt, &owner, &k.MCPTools, &k.MCPKBScope, &k.MCPKBIDs); err != nil {
		return nil, err
	}
	if owner.Valid {
		k.OwnerID = owner.String
	}
	return &k, nil
}

// CreateAPIKey 创建 API Key（只存 hash；MCP 权限列用默认值：空 = 无任何 MCP 权限，权限后续通过管理接口授予）
// OwnerID 非空时为用户 MCP 凭据（owner_id 唯一索引保证每用户至多一个）
func (s *pgStore) CreateAPIKey(ctx context.Context, k APIKey) error {
	if k.ID == "" || k.KeyHash == "" {
		return fmt.Errorf("API Key ID 与 hash 不能为空")
	}
	var owner any
	if k.OwnerID != "" {
		owner = k.OwnerID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, key_hash, enabled, created_at, owner_id)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		k.ID, k.Name, k.KeyHash, k.Enabled, time.Now(), owner,
	)
	return err
}

// ListAPIKeys 列出 API Key（不含 hash 由调用方决定展示，此处返回全部字段）
func (s *pgStore) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]APIKey, 0)
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

// GetAPIKeyByHash 按 hash 查询（认证中间件使用）
func (s *pgStore) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	k, err := scanAPIKey(s.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE key_hash = $1`, hash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return k, nil
}

// GetAPIKeyByOwner 查询用户归属的 MCP 凭据（每用户至多一个，owner_id 唯一索引保证；无则返回 nil）
func (s *pgStore) GetAPIKeyByOwner(ctx context.Context, ownerID string) (*APIKey, error) {
	k, err := scanAPIKey(s.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE owner_id = $1`, ownerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return k, nil
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

// UpdateAPIKeyPermissions 全量更新 Key 的 MCP 权限（PUT 语义）。
// 历史 Key 迁移后权限字段为空（无任何 MCP 权限），必须通过本方法显式授予（spec F6）。
func (s *pgStore) UpdateAPIKeyPermissions(ctx context.Context, id string, p APIKeyPermissions) error {
	tools := p.MCPTools
	if tools == nil {
		tools = []string{}
	}
	kbIDs := p.MCPKBIDs
	if kbIDs == nil {
		kbIDs = []string{}
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET mcp_tools = $2, mcp_kb_scope = $3, mcp_kb_ids = $4 WHERE id = $1`,
		id, tools, p.MCPKBScope, kbIDs,
	)
	return err
}
