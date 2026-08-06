package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateDocument 创建文档记录
func (s *pgStore) CreateDocument(ctx context.Context, doc Document) error {
	if doc.ID == "" || doc.KBID == "" {
		return fmt.Errorf("文档 ID 与 KBID 不能为空")
	}
	if doc.ChunkIDs == nil {
		doc.ChunkIDs = []string{} // chunk_ids 列 NOT NULL，nil 绑定 NULL 会违反约束
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO documents (id, kb_id, filename, format, size, status, chunk_ids, file_path, task_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		doc.ID, doc.KBID, doc.Filename, doc.Format, doc.Size, doc.Status,
		doc.ChunkIDs, doc.FilePath, doc.TaskID, time.Now(),
	)
	return err
}

// ListDocuments 按知识库列出文档
func (s *pgStore) ListDocuments(ctx context.Context, kbID string) ([]Document, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, kb_id, filename, format, size, status, chunk_ids, file_path, task_id, created_at
		 FROM documents WHERE kb_id = $1 ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]Document, 0)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	return docs, rows.Err()
}

// GetDocument 查询文档
func (s *pgStore) GetDocument(ctx context.Context, id string) (*Document, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, kb_id, filename, format, size, status, chunk_ids, file_path, task_id, created_at
		 FROM documents WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, pgx.ErrNoRows
	}
	doc, err := scanDocument(rows)
	if err != nil {
		return nil, err
	}
	return doc, rows.Err()
}

// UpdateDocumentStatus 更新文档状态并回填 chunk IDs
func (s *pgStore) UpdateDocumentStatus(ctx context.Context, id string, status string, chunkIDs []string) error {
	if chunkIDs == nil {
		chunkIDs = []string{} // 同上：NOT NULL 列不允许 NULL
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE documents SET status = $2, chunk_ids = $3 WHERE id = $1`,
		id, status, chunkIDs,
	)
	return err
}

// DeleteDocument 删除文档记录
func (s *pgStore) DeleteDocument(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM documents WHERE id = $1`, id)
	return err
}

// scanDocument 扫描一行文档
func scanDocument(row pgx.Rows) (*Document, error) {
	var doc Document
	if err := row.Scan(&doc.ID, &doc.KBID, &doc.Filename, &doc.Format, &doc.Size,
		&doc.Status, &doc.ChunkIDs, &doc.FilePath, &doc.TaskID, &doc.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return &doc, nil
}
