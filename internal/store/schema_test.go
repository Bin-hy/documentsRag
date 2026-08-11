package store

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// Migrate 执行全部迁移：schemaDDL + 5 个追加迁移，幂等关键字（IF NOT EXISTS）覆盖
func TestMigrate(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}

	// 期望与实际 Exec 顺序一致（pgxmock 按序匹配）
	// 1) schemaDDL（一条多语句 SQL，正则匹配开头特征）
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS knowledge_bases").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	// 2) 追加迁移（幂等）
	mock.ExpectExec("ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS strategy").WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("ALTER TABLE chat_history ADD COLUMN IF NOT EXISTS sources").WillReturnResult(pgxmock.NewResult("ALTER", 0))
	mock.ExpectExec("ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS owner_id").WillReturnResult(pgxmock.NewResult("ALTER", 0))
	// 3) api_keys MCP 权限列（一条多语句 SQL；默认空 = 历史 Key 无 MCP 权限）
	mock.ExpectExec("ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS mcp_tools").WillReturnResult(pgxmock.NewResult("ALTER", 0))
	// 3.5) api_keys owner_id 列 + 部分唯一索引（用户 MCP 凭据，每用户至多一个）
	mock.ExpectExec("ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id").WillReturnResult(pgxmock.NewResult("ALTER", 0))
	// 4) mcp_audit_logs 审计表 + 索引（一条多语句 SQL）
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS mcp_audit_logs").WillReturnResult(pgxmock.NewResult("CREATE", 0))

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate 失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("迁移 SQL 序列与期望不符: %v", err)
	}
}
