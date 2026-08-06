package store

import (
	"context"
	"fmt"
	"time"
)

// CreateKB 创建知识库
func (s *pgStore) CreateKB(ctx context.Context, kb KnowledgeBase) error {
	if kb.ID == "" {
		return fmt.Errorf("知识库 ID 不能为空")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO knowledge_bases (id, name, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		kb.ID, kb.Name, kb.Description, kb.CreatedAt, kb.UpdatedAt,
	)
	return err
}

// ListKBs 列出全部知识库
func (s *pgStore) ListKBs(ctx context.Context) ([]KnowledgeBase, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, description, created_at, updated_at FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kbs := make([]KnowledgeBase, 0)
	for rows.Next() {
		var kb KnowledgeBase
		if err := rows.Scan(&kb.ID, &kb.Name, &kb.Description, &kb.CreatedAt, &kb.UpdatedAt); err != nil {
			return nil, err
		}
		kbs = append(kbs, kb)
	}
	return kbs, rows.Err()
}

// GetKB 查询知识库
func (s *pgStore) GetKB(ctx context.Context, id string) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, description, created_at, updated_at FROM knowledge_bases WHERE id = $1`, id,
	).Scan(&kb.ID, &kb.Name, &kb.Description, &kb.CreatedAt, &kb.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &kb, nil
}

// UpdateKB 更新知识库（名称与描述）
func (s *pgStore) UpdateKB(ctx context.Context, kb KnowledgeBase) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE knowledge_bases SET name = $2, description = $3, updated_at = $4 WHERE id = $1`,
		kb.ID, kb.Name, kb.Description, time.Now(),
	)
	return err
}

// DeleteKB 删除知识库（documents 通过外键级联删除；向量库清理由调用方负责）
func (s *pgStore) DeleteKB(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM knowledge_bases WHERE id = $1`, id)
	return err
}
