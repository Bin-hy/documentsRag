package store

import (
	"context"
	"testing"

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

	mock.ExpectExec("INSERT INTO api_keys \\(id, name, key_hash, enabled, created_at\\)").
		WithArgs("key-1", "测试", "hash", true, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := s.CreateAPIKey(context.Background(), APIKey{ID: "key-1", Name: "测试", KeyHash: "hash", Enabled: true}); err != nil {
		t.Fatalf("CreateAPIKey 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}
