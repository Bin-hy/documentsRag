package store

import (
	"context"
	"os"
	"testing"
)

// TestMigrateIdempotentOnRealPG 真实 PG 迁移幂等验证（checklist「migration 幂等」）。
// 需要环境变量 BINRAG_TEST_PG_DSN 指向可用 PostgreSQL；未设置时跳过。
func TestMigrateIdempotentOnRealPG(t *testing.T) {
	dsn := os.Getenv("BINRAG_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 BINRAG_TEST_PG_DSN，跳过真实 PG 迁移验证")
	}
	ctx := context.Background()
	st, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatalf("连接 PG 失败: %v", err)
	}
	defer st.Close()

	// 连续执行两次 Migrate：均不报错（幂等，checklist）
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("第一次 Migrate 失败: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("第二次 Migrate 失败（应幂等）: %v", err)
	}

	pg := st.(*pgStore)
	// api_keys MCP 权限列存在
	var tools []string
	var scope string
	var ids []string
	if err := pg.pool.QueryRow(ctx,
		`SELECT mcp_tools, mcp_kb_scope, mcp_kb_ids FROM api_keys LIMIT 1`,
	).Scan(&tools, &scope, &ids); err != nil {
		t.Fatalf("api_keys MCP 权限列不可查询（列不存在或类型错误）: %v", err)
	}
	// mcp_audit_logs 表存在
	var cnt int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_audit_logs`).Scan(&cnt); err != nil {
		t.Fatalf("mcp_audit_logs 表不可查询: %v", err)
	}
	t.Logf("迁移幂等验证通过：api_keys 权限列存在，mcp_audit_logs 可查询（%d 行）", cnt)
}
