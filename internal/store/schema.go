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
`

// 知识库策略列迁移（幂等：已有库补列）
const kbStrategyMigration = `
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT '';
`

// Migrate 执行建表语句（幂等）
func (s *pgStore) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaDDL); err != nil {
		return err
	}
	// 追加策略列迁移（schemaDDL 的 CREATE TABLE IF NOT EXISTS 对已有表不生效，需显式 ALTER）
	_, err := s.pool.Exec(ctx, kbStrategyMigration)
	return err
}
