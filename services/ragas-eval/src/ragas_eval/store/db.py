"""SQLite 连接管理与版本化迁移（T3）。

设计要点（architect-design §6.1）：
- WAL 模式：单写多读负载匹配（控制流读 QPS 低、写为 worker 单写者）
- 启动时执行版本化 DDL（user_version 递增迁移）
- 全部 SQL 收口于 repo.py，本模块只管连接与迁移
"""

from __future__ import annotations

import aiosqlite

# 当前 schema 版本（每次 DDL 变更 +1 并在 _MIGRATIONS 追加）
SCHEMA_VERSION = 1

# v1 初始建表：datasets / tasks / samples / reports 四表 + 索引（plan.md 核心数据结构）
_DDL_V1 = """
CREATE TABLE IF NOT EXISTS datasets (
    id            TEXT PRIMARY KEY,            -- UUID v4
    name          TEXT NOT NULL,               -- 数据集名称（默认取文件名）
    source_format TEXT NOT NULL,               -- json | jsonl
    content_hash  TEXT NOT NULL,               -- sha256:... 内容哈希（可复现/对比用）
    sample_count  INTEGER NOT NULL,            -- 样本总数（冗余，列表不解析原文）
    with_reference_count INTEGER NOT NULL,     -- 含标准答案样本数（决定 context_recall 覆盖率）
    raw_blob      TEXT NOT NULL,               -- 原始文件内容（预览/重建任务样本用）
    created_at    TEXT NOT NULL                -- RFC3339
);

CREATE TABLE IF NOT EXISTS tasks (
    id                  TEXT PRIMARY KEY,      -- UUID v4
    name                TEXT NOT NULL,
    dataset_id          TEXT NOT NULL REFERENCES datasets(id),
    kb_id               TEXT NOT NULL DEFAULT '',   -- 空 = 逐样本用自带 kb_id / 不限定
    judge_model         TEXT NOT NULL,
    metrics_json        TEXT NOT NULL,         -- JSON 数组：启用指标
    sample_concurrency  INTEGER NOT NULL DEFAULT 4,
    strategy            TEXT,                  -- 透传 Go chat 的 strategy 覆盖（可空）
    status              TEXT NOT NULL,         -- pending|collecting|evaluating|completed|failed|canceled
    progress_json       TEXT NOT NULL,         -- {total, collected, evaluated, failed}
    error_message       TEXT NOT NULL DEFAULT '',
    idempotency_key     TEXT UNIQUE,           -- 幂等键（24h 窗口，可空）
    config_snapshot_json TEXT NOT NULL DEFAULT '{}',  -- 可复现快照
    created_at          TEXT NOT NULL,
    started_at          TEXT,
    finished_at         TEXT
);

CREATE TABLE IF NOT EXISTS samples (
    id            TEXT PRIMARY KEY,            -- UUID v4
    task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    idx           INTEGER NOT NULL,            -- 数据集内序号（0 起）
    question      TEXT NOT NULL,
    reference     TEXT NOT NULL DEFAULT '',    -- 标准答案（可空）
    kb_id         TEXT NOT NULL DEFAULT '',    -- 样本自带知识库范围（可空）
    expected_ids_json TEXT NOT NULL DEFAULT '[]',  -- Recall@K 用期望 ID（RAGAS 侧仅存档）
    answer        TEXT NOT NULL DEFAULT '',    -- 采集到的回答
    contexts_json TEXT NOT NULL DEFAULT '[]',  -- 检索上下文正文+元数据
    scores_json   TEXT NOT NULL DEFAULT '{}',  -- {metric: {score, reason}}
    status        TEXT NOT NULL DEFAULT 'pending',  -- pending|collected|ok|error
    error         TEXT NOT NULL DEFAULT '',
    UNIQUE(task_id, idx)                       -- 样本级幂等：重启续跑跳过已 ok 样本
);

CREATE TABLE IF NOT EXISTS reports (
    task_id      TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    summary_json TEXT NOT NULL,                -- 四指标 mean/coverage/valid_samples 等
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_samples_task ON samples(task_id, idx);
CREATE INDEX IF NOT EXISTS idx_datasets_hash ON datasets(content_hash);
"""

# 版本化迁移表：版本号 -> DDL（启动时按 user_version 顺序执行）
_MIGRATIONS: dict[int, str] = {1: _DDL_V1}


async def connect(db_path: str) -> aiosqlite.Connection:
    """建立连接并开启 WAL / 外键 / 行工厂。

    调用方负责 close（或由 store.db.Database 托管生命周期）。
    """
    conn = await aiosqlite.connect(db_path)
    conn.row_factory = aiosqlite.Row
    # WAL：读不阻塞写，单写多读场景吞吐更好
    await conn.execute("PRAGMA journal_mode=WAL")
    await conn.execute("PRAGMA foreign_keys=ON")
    # 同步级别 NORMAL：WAL 下兼顾安全与性能
    await conn.execute("PRAGMA synchronous=NORMAL")
    await conn.commit()
    return conn


async def migrate(conn: aiosqlite.Connection) -> None:
    """启动时执行版本化 DDL：按 user_version 逐版本追赶。"""
    cur = await conn.execute("PRAGMA user_version")
    row = await cur.fetchone()
    current = int(row[0]) if row else 0
    for version in range(current + 1, SCHEMA_VERSION + 1):
        ddl = _MIGRATIONS.get(version)
        if ddl:
            await conn.executescript(ddl)
        await conn.execute(f"PRAGMA user_version={version}")
    await conn.commit()


class Database:
    """连接持有者：托管 aiosqlite 连接生命周期，供 DI 单例使用。"""

    def __init__(self, db_path: str) -> None:
        self._db_path = db_path
        self._conn: aiosqlite.Connection | None = None

    async def open(self) -> aiosqlite.Connection:
        """打开连接并执行迁移（幂等，重复调用返回同一连接）。"""
        if self._conn is None:
            self._conn = await connect(self._db_path)
            await migrate(self._conn)
        return self._conn

    @property
    def conn(self) -> aiosqlite.Connection:
        """取已打开的连接；未 open 时抛错（防裸用）。"""
        if self._conn is None:
            raise RuntimeError("数据库未初始化：请先调用 Database.open()")
        return self._conn

    async def close(self) -> None:
        """关闭连接（关停钩子调用）。"""
        if self._conn is not None:
            await self._conn.close()
            self._conn = None
