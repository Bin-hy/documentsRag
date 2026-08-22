package store

import (
	"context"
	"fmt"
	"time"
)

// kbColumns 知识库查询列（不含 join）
const kbColumns = "id, name, description, strategy, owner_id, created_at, updated_at"

// scanKB 扫描一行知识库（owner_id NULL → OwnerID 为 nil）
func scanKB(row interface{ Scan(...any) error }) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	if err := row.Scan(&kb.ID, &kb.Name, &kb.Description, &kb.Strategy, &kb.OwnerID, &kb.CreatedAt, &kb.UpdatedAt); err != nil {
		return nil, err
	}
	return &kb, nil
}

// CreateKB 创建知识库（OwnerID：API 层显式决定——登录用户传 &UserID，系统级 API Key 传 nil 表示 NULL）
func (s *pgStore) CreateKB(ctx context.Context, kb KnowledgeBase) error {
	if kb.ID == "" {
		return fmt.Errorf("知识库 ID 不能为空")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO knowledge_bases (id, name, description, strategy, owner_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		kb.ID, kb.Name, kb.Description, kb.Strategy, kb.OwnerID, kb.CreatedAt, kb.UpdatedAt,
	)
	return err
}

// ListAllKBs 全量列出知识库（系统级 API Key 用：包含 owner_id IS NULL 与 owner_id 非 NULL 的全部）
func (s *pgStore) ListAllKBs(ctx context.Context) ([]KnowledgeBase, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+kbColumns+` FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kbs := make([]KnowledgeBase, 0)
	for rows.Next() {
		kb, err := scanKB(rows)
		if err != nil {
			return nil, err
		}
		kbs = append(kbs, *kb)
	}
	return kbs, rows.Err()
}

// ListKBsByOwner 仅列出归属该用户的知识库（owner_id = ownerID）
func (s *pgStore) ListKBsByOwner(ctx context.Context, ownerID string) ([]KnowledgeBase, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+kbColumns+` FROM knowledge_bases WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kbs := make([]KnowledgeBase, 0)
	for rows.Next() {
		kb, err := scanKB(rows)
		if err != nil {
			return nil, err
		}
		kbs = append(kbs, *kb)
	}
	return kbs, rows.Err()
}

// ListKBsByIDs 按 ID 白名单查询知识库（MCP allowlist 授权用，不过滤 owner——显式授权即访问凭证）
func (s *pgStore) ListKBsByIDs(ctx context.Context, ids []string) ([]KnowledgeBase, error) {
	if len(ids) == 0 {
		return []KnowledgeBase{}, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+kbColumns+` FROM knowledge_bases WHERE id = ANY($1) ORDER BY created_at DESC`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kbs := make([]KnowledgeBase, 0)
	for rows.Next() {
		kb, err := scanKB(rows)
		if err != nil {
			return nil, err
		}
		kbs = append(kbs, *kb)
	}
	return kbs, rows.Err()
}

// GetKB 查询知识库（返回 OwnerID，权限判断由 API 层负责）
func (s *pgStore) GetKB(ctx context.Context, id string) (*KnowledgeBase, error) {
	return scanKB(s.pool.QueryRow(ctx,
		`SELECT `+kbColumns+` FROM knowledge_bases WHERE id = $1`, id))
}

// UpdateKB 更新知识库（名称、描述、策略；owner 不在此处变更）
func (s *pgStore) UpdateKB(ctx context.Context, kb KnowledgeBase) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE knowledge_bases SET name = $2, description = $3, strategy = $4, updated_at = $5 WHERE id = $1`,
		kb.ID, kb.Name, kb.Description, kb.Strategy, time.Now(),
	)
	return err
}

// DeleteKB 删除知识库（documents 通过外键级联删除；ingest_tasks 无外键，手动清理；向量库清理由调用方负责）
func (s *pgStore) DeleteKB(ctx context.Context, id string) error {
	// 先删关联 ingest_tasks（无外键约束，手动清理避免孤儿任务；不用外键保持分库分表扩展性）
	if _, err := s.pool.Exec(ctx, `DELETE FROM ingest_tasks WHERE kb_id = $1`, id); err != nil {
		return fmt.Errorf("清理关联入库任务失败: %w", err)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM knowledge_bases WHERE id = $1`, id)
	return err
}
