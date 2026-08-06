package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

func now() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) }

// 创建知识库：参数绑定正确
func TestCreateKB(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	kb := KnowledgeBase{ID: "kb-1", Name: "测试库", Description: "描述", CreatedAt: now(), UpdatedAt: now()}

	mock.ExpectExec("INSERT INTO knowledge_bases").
		WithArgs(kb.ID, kb.Name, kb.Description, kb.Strategy, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.CreateKB(context.Background(), kb); err != nil {
		t.Fatalf("CreateKB 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// 领取任务：UPDATE...RETURNING 且状态置为 processing
func TestClaimPendingTasks(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	ts := now()

	rows := pgxmock.NewRows([]string{"id", "kb_id", "document_id", "status", "retry_count", "error_message", "created_at", "updated_at"}).
		AddRow("task-1", "kb-1", "doc-1", "processing", 0, "", ts, ts)

	mock.ExpectQuery("UPDATE ingest_tasks").
		WithArgs(1).
		WillReturnRows(rows)

	tasks, err := s.ClaimPendingTasks(context.Background(), 1)
	if err != nil {
		t.Fatalf("ClaimPendingTasks 失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("期望 1 个任务，实际 %d", len(tasks))
	}
	if tasks[0].ID != "task-1" || tasks[0].Status != "processing" {
		t.Errorf("任务解析错误: %+v", tasks[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// 重置悬挂任务：processing → pending
func TestResetProcessingTasks(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	mock.ExpectExec("UPDATE ingest_tasks").WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	if err := s.ResetProcessingTasks(context.Background()); err != nil {
		t.Fatalf("ResetProcessingTasks 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// 按 hash 查询 API Key：命中返回记录
func TestGetAPIKeyByHash_Hit(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	ts := now()

	rows := pgxmock.NewRows([]string{"id", "name", "key_hash", "enabled", "last_used_at", "created_at"}).
		AddRow("key-1", "默认", "abc123", true, nil, ts)

	mock.ExpectQuery("SELECT id, name, key_hash, enabled, last_used_at, created_at FROM api_keys WHERE key_hash = \\$1").
		WithArgs("abc123").
		WillReturnRows(rows)

	k, err := s.GetAPIKeyByHash(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash 失败: %v", err)
	}
	if k == nil || k.ID != "key-1" || !k.Enabled || k.KeyHash != "abc123" {
		t.Errorf("API Key 解析错误: %+v", k)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// 按 hash 查询 API Key：未命中返回 nil 而非错误
func TestGetAPIKeyByHash_Miss(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	mock.ExpectQuery("SELECT id, name, key_hash, enabled, last_used_at, created_at FROM api_keys WHERE key_hash = \\$1").
		WithArgs("nope").
		WillReturnError(pgx.ErrNoRows)

	k, err := s.GetAPIKeyByHash(context.Background(), "nope")
	if err != nil {
		t.Fatalf("未命中不应报错: %v", err)
	}
	if k != nil {
		t.Errorf("未命中应返回 nil，实际 %+v", k)
	}
}

// 对话历史：Get 返回最近 limit 条（时间正序）
func TestPostgresHistory_Get(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	h := &PostgresHistoryStore{pool: mock}

	// 子查询 DESC 取最近 2 条后外层 ASC 排序
	rows := pgxmock.NewRows([]string{"role", "content"}).
		AddRow("user", "第二个问题").
		AddRow("assistant", "第二个回答")

	mock.ExpectQuery("SELECT role, content").
		WithArgs("sess-1", 2).
		WillReturnRows(rows)

	msgs, err := h.Get(context.Background(), "sess-1", 2)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(msgs))
	}
	if msgs[0].Content != "第二个问题" || msgs[1].Content != "第二个回答" {
		t.Errorf("历史顺序错误: %+v", msgs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// 错误隔离：store 错误上抛
func TestCreateKB_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	kb := KnowledgeBase{ID: "kb-1", Name: "x", CreatedAt: now(), UpdatedAt: now()}

	mock.ExpectExec("INSERT INTO knowledge_bases").
		WithArgs(kb.ID, kb.Name, kb.Description, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db down"))

	if err := s.CreateKB(context.Background(), kb); err == nil {
		t.Fatal("DB 错误应上抛")
	}
}
