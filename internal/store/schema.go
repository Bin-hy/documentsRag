package store

import "context"

// schemaDDL 五张表建表语句
const schemaDDL = `
CREATE TABLE IF NOT EXISTS knowledge_bases (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    format      TEXT NOT NULL DEFAULT '',
    size        BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending',
    chunk_ids   TEXT[] NOT NULL DEFAULT '{}',
    file_path   TEXT NOT NULL DEFAULT '',
    task_id     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_documents_kb_id ON documents(kb_id);

CREATE TABLE IF NOT EXISTS ingest_tasks (
    id            TEXT PRIMARY KEY,
    kb_id         TEXT NOT NULL,
    document_id   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    retry_count   INT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ingest_tasks_status ON ingest_tasks(status);

CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_history (
    id         BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL,
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_history_session ON chat_history(session_id, created_at);

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);
`

// 知识库策略列迁移（幂等：已有库补列）
const kbStrategyMigration = `
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT '';
`

// 对话历史 sources 列迁移（幂等：已有库补列）
const chatHistorySourcesMigration = `
ALTER TABLE chat_history ADD COLUMN IF NOT EXISTS sources TEXT NOT NULL DEFAULT '';
`

// 知识库 owner 迁移（幂等：NULL = 系统级知识库；无外键约束，用户删除不在本版范围）
const kbOwnerMigration = `
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS owner_id TEXT;
`

// api_keys MCP 权限列迁移（幂等：默认空 = 历史 Key 无任何 MCP 权限，spec F6）
const apiKeyMCPPermissionsMigration = `
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS mcp_tools TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS mcp_kb_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS mcp_kb_ids TEXT[] NOT NULL DEFAULT '{}';
`

// apiKeyOwnerMigration API Key 用户归属（幂等：NULL = 系统级 Key；非 NULL = 用户 MCP 凭据，每用户至多一个）
const apiKeyOwnerMigration = `
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_owner ON api_keys(owner_id) WHERE owner_id IS NOT NULL;
`

// mcpAuditLogsDDL MCP 调用审计表（仅记录 api_key_id 引用与截断参数，绝不存 Secret，spec F7）
const mcpAuditLogsDDL = `
CREATE TABLE IF NOT EXISTS mcp_audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    api_key_id    TEXT NOT NULL,
    tool_name     TEXT NOT NULL,
    params        TEXT NOT NULL DEFAULT '',
    params_len    INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_created_at ON mcp_audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_api_key ON mcp_audit_logs(api_key_id, created_at);
`

// Migrate 执行建表语句（幂等）
func (s *pgStore) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaDDL); err != nil {
		return err
	}
	// 追加迁移（schemaDDL 的 CREATE TABLE IF NOT EXISTS 对已有表不生效，需显式 ALTER）
	if _, err := s.pool.Exec(ctx, kbStrategyMigration); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, chatHistorySourcesMigration); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, kbOwnerMigration); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, apiKeyMCPPermissionsMigration); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, apiKeyOwnerMigration); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, mcpAuditLogsDDL)
	return err
}
