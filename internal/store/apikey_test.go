package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pashagolub/pgxmock/v4"
)

// UpdateAPIKeyPermissions：全量更新（nil 切片视为清空）
func TestUpdateAPIKeyPermissions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}

	// 授权 allowlist：tools + scope + kbIDs
	mock.ExpectExec("UPDATE api_keys SET mcp_tools = \\$2, mcp_kb_scope = \\$3, mcp_kb_ids = \\$4 WHERE id = \\$1").
		WithArgs("key-1", []string{"retrieve", "ask"}, "allowlist", []string{"kb-a", "kb-b"}).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := s.UpdateAPIKeyPermissions(context.Background(), "key-1", APIKeyPermissions{
		MCPTools:   []string{"retrieve", "ask"},
		MCPKBScope: "allowlist",
		MCPKBIDs:   []string{"kb-a", "kb-b"},
	}); err != nil {
		t.Fatalf("UpdateAPIKeyPermissions(allowlist) 失败: %v", err)
	}

	// 清空：nil 切片 → 空数组；scope 置空
	mock.ExpectExec("UPDATE api_keys SET mcp_tools = \\$2, mcp_kb_scope = \\$3, mcp_kb_ids = \\$4 WHERE id = \\$1").
		WithArgs("key-1", []string{}, "", []string{}).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := s.UpdateAPIKeyPermissions(context.Background(), "key-1", APIKeyPermissions{}); err != nil {
		t.Fatalf("UpdateAPIKeyPermissions(清空) 失败: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// CreateAPIKey：MCP 权限列不指定（用默认空 = 无任何 MCP 权限）
func TestCreateAPIKeyNoMCPPermissions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}

	mock.ExpectExec("INSERT INTO api_keys \\(id, name, key_hash, enabled, created_at, owner_id\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
		WithArgs("key-1", "测试", "hash", true, pgxmock.AnyArg(), nil).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := s.CreateAPIKey(context.Background(), APIKey{ID: "key-1", Name: "测试", KeyHash: "hash", Enabled: true}); err != nil {
		t.Fatalf("CreateAPIKey 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// GetAPIKeyByOwner：用户 MCP 凭据查询（owner_id 过滤）；无结果返回 nil
func TestGetAPIKeyByOwner(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}
	ts := time.Now()

	// 命中
	rows := pgxmock.NewRows([]string{"id", "name", "key_hash", "enabled", "last_used_at", "created_at", "owner_id", "mcp_tools", "mcp_kb_scope", "mcp_kb_ids"}).
		AddRow("key-mcp", "mcp-user", "hash1", true, nil, ts, "user-1", []string{"retrieve"}, "all", []string{})
	mock.ExpectQuery("SELECT id, name, key_hash, enabled, last_used_at, created_at, owner_id, mcp_tools, mcp_kb_scope, mcp_kb_ids FROM api_keys WHERE owner_id = \\$1").
		WithArgs("user-1").WillReturnRows(rows)
	k, err := s.GetAPIKeyByOwner(context.Background(), "user-1")
	if err != nil || k == nil {
		t.Fatalf("GetAPIKeyByOwner 命中失败: %v %v", k, err)
	}
	if k.OwnerID != "user-1" || len(k.MCPTools) != 1 || k.MCPKBScope != "all" {
		t.Errorf("凭据字段解析错误: %+v", k)
	}

	// 未命中 → nil 非错误
	mock.ExpectQuery("SELECT id, name, key_hash, enabled, last_used_at, created_at, owner_id, mcp_tools, mcp_kb_scope, mcp_kb_ids FROM api_keys WHERE owner_id = \\$1").
		WithArgs("user-nobody").WillReturnError(pgx.ErrNoRows)
	k, err = s.GetAPIKeyByOwner(context.Background(), "user-nobody")
	if err != nil || k != nil {
		t.Errorf("未命中应返回 nil: %v %v", k, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}
