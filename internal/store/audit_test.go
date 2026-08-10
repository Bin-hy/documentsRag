package store

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// ListKBsByIDs：allowlist 白名单查询（ANY 参数绑定）；空列表返回空而非错误
func TestListKBsByIDs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}
	ts := now()

	cols := []string{"id", "name", "description", "strategy", "owner_id", "created_at", "updated_at"}
	rows := pgxmock.NewRows(cols).
		AddRow("kb-a", "库A", "", "", nil, ts, ts).
		AddRow("kb-b", "库B", "", "", nil, ts, ts)

	mock.ExpectQuery("SELECT id, name, description, strategy, owner_id, created_at, updated_at FROM knowledge_bases WHERE id = ANY\\(\\$1\\) ORDER BY created_at DESC").
		WithArgs([]string{"kb-a", "kb-b"}).
		WillReturnRows(rows)

	kbs, err := s.ListKBsByIDs(context.Background(), []string{"kb-a", "kb-b"})
	if err != nil {
		t.Fatalf("ListKBsByIDs 失败: %v", err)
	}
	if len(kbs) != 2 {
		t.Errorf("应返回 2 个知识库，实际 %d", len(kbs))
	}

	// 空白名单：直接返回空，不查库
	empty, err := s.ListKBsByIDs(context.Background(), nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("空白名单应返回空列表: %v %v", empty, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// AppendAuditLog：INSERT 绑定 api_key_id 与截断参数（无 Secret 列）
func TestAppendAuditLog(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}

	mock.ExpectExec("INSERT INTO mcp_audit_logs \\(api_key_id, tool_name, params, params_len, status, error_message, duration_ms, created_at\\)").
		WithArgs("key-1", "ask", `{"question":"你好"}`, 15, "success", "", int64(123), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.AppendAuditLog(context.Background(), AuditLog{
		APIKeyID:   "key-1",
		ToolName:   "ask",
		Params:     `{"question":"你好"}`,
		ParamsLen:  15,
		Status:     "success",
		DurationMS: 123,
	}); err != nil {
		t.Fatalf("AppendAuditLog 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}
