package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// api_keys MCP 权限列 + owner_id 存在
	var tools []string
	var scope string
	var ids []string
	var owner *string
	if err := pg.pool.QueryRow(ctx,
		`SELECT mcp_tools, mcp_kb_scope, mcp_kb_ids, owner_id FROM api_keys LIMIT 1`,
	).Scan(&tools, &scope, &ids, &owner); err != nil {
		t.Fatalf("api_keys MCP 权限列/owner_id 不可查询（列不存在或类型错误）: %v", err)
	}
	// owner_id 部分唯一索引存在（插入两个同 owner 应冲突；先删除可能存在的测试数据）
	if _, err := pg.pool.Exec(ctx, `DELETE FROM api_keys WHERE owner_id = 'migrate-test-owner'`); err != nil {
		t.Fatalf("清理测试数据失败: %v", err)
	}
	sum := sha256.Sum256([]byte("migrate-test-token-1"))
	if _, err := pg.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, key_hash, enabled, created_at, owner_id) VALUES ($1,$2,$3,true,now(),'migrate-test-owner')`,
		"migrate-key-1", "migrate-test", hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("插入测试凭据失败: %v", err)
	}
	sum2 := sha256.Sum256([]byte("migrate-test-token-2"))
	if _, err := pg.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, key_hash, enabled, created_at, owner_id) VALUES ($1,$2,$3,true,now(),'migrate-test-owner')`,
		"migrate-key-2", "migrate-test", hex.EncodeToString(sum2[:])); err == nil {
		t.Error("同一 owner 插入第二个凭据应被唯一索引拒绝")
	}
	if _, err := pg.pool.Exec(ctx, `DELETE FROM api_keys WHERE owner_id = 'migrate-test-owner'`); err != nil {
		t.Fatalf("清理测试数据失败: %v", err)
	}
	// mcp_audit_logs 表存在
	var cnt int
	if err := pg.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_audit_logs`).Scan(&cnt); err != nil {
		t.Fatalf("mcp_audit_logs 表不可查询: %v", err)
	}
	t.Logf("迁移幂等验证通过：api_keys 权限列/owner_id 存在，owner 唯一索引生效，mcp_audit_logs 可查询（%d 行）", cnt)
}
