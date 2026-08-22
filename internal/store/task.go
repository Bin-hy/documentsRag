package store

import (
	"context"
	"fmt"
	"time"
)

// CreateTask 创建入库任务
func (s *pgStore) CreateTask(ctx context.Context, t Task) error {
	if t.ID == "" || t.DocumentID == "" {
		return fmt.Errorf("任务 ID 与 DocumentID 不能为空")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ingest_tasks (id, kb_id, document_id, status, retry_count, error_message, warning_message, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.KBID, t.DocumentID, t.Status, t.RetryCount, t.ErrorMessage, t.WarningMessage, time.Now(), time.Now(),
	)
	return err
}

// GetTask 查询任务
func (s *pgStore) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	err := s.pool.QueryRow(ctx,
		`SELECT id, kb_id, document_id, status, retry_count, error_message, warning_message, created_at, updated_at
		 FROM ingest_tasks WHERE id = $1`, id,
	).Scan(&t.ID, &t.KBID, &t.DocumentID, &t.Status, &t.RetryCount, &t.ErrorMessage, &t.WarningMessage, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTasks 按知识库列出任务
func (s *pgStore) ListTasks(ctx context.Context, kbID string) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, kb_id, document_id, status, retry_count, error_message, warning_message, created_at, updated_at
		 FROM ingest_tasks WHERE kb_id = $1 ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.KBID, &t.DocumentID, &t.Status, &t.RetryCount,
			&t.ErrorMessage, &t.WarningMessage, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateTask 更新任务（状态 / 重试次数 / 错误信息）
// t.UpdatedAt 非零时写入指定值（worker 重试退避用），零值时用当前时间
func (s *pgStore) UpdateTask(ctx context.Context, t Task) error {
	updatedAt := t.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE ingest_tasks SET status = $2, retry_count = $3, error_message = $4, warning_message = $5, updated_at = $6 WHERE id = $1`,
		t.ID, t.Status, t.RetryCount, t.ErrorMessage, t.WarningMessage, updatedAt,
	)
	return err
}

// ClaimPendingTasks 原子领取一批 pending 任务（置为 processing），防多 worker 抢同一任务
// updated_at > NOW() 的任务处于退避期，跳过不认领（worker 重试退避）
func (s *pgStore) ClaimPendingTasks(ctx context.Context, limit int) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE ingest_tasks SET status = 'processing', updated_at = now()
		 WHERE id IN (
		     SELECT id FROM ingest_tasks WHERE status = 'pending' AND updated_at <= NOW() ORDER BY created_at LIMIT $1
		 )
		 RETURNING id, kb_id, document_id, status, retry_count, error_message, warning_message, created_at, updated_at`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.KBID, &t.DocumentID, &t.Status, &t.RetryCount,
			&t.ErrorMessage, &t.WarningMessage, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ResetProcessingTasks 把 processing 任务重置为 pending（启动恢复，防悬挂）
func (s *pgStore) ResetProcessingTasks(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE ingest_tasks SET status = 'pending', updated_at = now() WHERE status = 'processing'`)
	return err
}
