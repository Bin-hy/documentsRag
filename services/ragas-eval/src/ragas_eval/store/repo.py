"""仓储层（T3）：全部 SQL 收口于此，API/worker 不写裸 SQL。

职责：任务 CRUD、样本批量写入与状态回写、进度更新、报告落库与查询、
幂等键查重、数据集管理与引用检查、启动恢复用的状态重置。
"""

from __future__ import annotations

import json
import uuid
from datetime import UTC, datetime
from typing import Any

import aiosqlite

from ragas_eval.store import models as m
from ragas_eval.store.db import Database


def _now() -> str:
    """当前时间 RFC3339 带时区（契约 §2.1：时间一律 ISO8601 带时区）。"""
    return datetime.now(UTC).isoformat()


def _new_id() -> str:
    """UUID v4 字符串（契约 §2.1：ID 全部 UUID v4）。"""
    return str(uuid.uuid4())


# ======================================================================
# 行 <-> dataclass 映射
# ======================================================================


def _row_to_dataset(row: aiosqlite.Row) -> m.Dataset:
    return m.Dataset(
        id=row["id"],
        name=row["name"],
        source_format=row["source_format"],
        content_hash=row["content_hash"],
        sample_count=row["sample_count"],
        with_reference_count=row["with_reference_count"],
        raw_blob=row["raw_blob"],
        created_at=row["created_at"],
    )


def _row_to_task(row: aiosqlite.Row) -> m.Task:
    progress = json.loads(row["progress_json"] or "{}")
    return m.Task(
        id=row["id"],
        name=row["name"],
        dataset_id=row["dataset_id"],
        kb_id=row["kb_id"],
        judge_model=row["judge_model"],
        metrics=json.loads(row["metrics_json"] or "[]"),
        sample_concurrency=row["sample_concurrency"],
        strategy=row["strategy"],
        status=row["status"],
        progress=m.TaskProgress(
            total=progress.get("total", 0),
            collected=progress.get("collected", 0),
            evaluated=progress.get("evaluated", 0),
            failed=progress.get("failed", 0),
        ),
        error_message=row["error_message"],
        idempotency_key=row["idempotency_key"],
        config_snapshot=json.loads(row["config_snapshot_json"] or "{}"),
        created_at=row["created_at"],
        started_at=row["started_at"],
        finished_at=row["finished_at"],
    )


def _row_to_sample(row: aiosqlite.Row) -> m.Sample:
    return m.Sample(
        id=row["id"],
        task_id=row["task_id"],
        idx=row["idx"],
        question=row["question"],
        reference=row["reference"],
        kb_id=row["kb_id"],
        expected_ids=json.loads(row["expected_ids_json"] or "[]"),
        answer=row["answer"],
        contexts=json.loads(row["contexts_json"] or "[]"),
        scores=json.loads(row["scores_json"] or "{}"),
        status=row["status"],
        error=row["error"],
    )


class Repo:
    """仓储单例：包裹 Database，向上层暴露领域方法。"""

    def __init__(self, db: Database) -> None:
        self._db = db

    @property
    def _conn(self) -> aiosqlite.Connection:
        return self._db.conn

    # ==================================================================
    # 数据集
    # ==================================================================

    async def create_dataset(
        self,
        name: str,
        source_format: str,
        content_hash: str,
        sample_count: int,
        with_reference_count: int,
        raw_blob: str,
    ) -> m.Dataset:
        """新建数据集记录。"""
        ds = m.Dataset(
            id=_new_id(),
            name=name,
            source_format=source_format,
            content_hash=content_hash,
            sample_count=sample_count,
            with_reference_count=with_reference_count,
            raw_blob=raw_blob,
            created_at=_now(),
        )
        await self._conn.execute(
            "INSERT INTO datasets(id,name,source_format,content_hash,sample_count,"
            "with_reference_count,raw_blob,created_at) VALUES(?,?,?,?,?,?,?,?)",
            (
                ds.id,
                ds.name,
                ds.source_format,
                ds.content_hash,
                ds.sample_count,
                ds.with_reference_count,
                ds.raw_blob,
                ds.created_at,
            ),
        )
        await self._conn.commit()
        return ds

    async def list_datasets(self) -> list[m.Dataset]:
        """数据集列表（按创建时间倒序）。"""
        cur = await self._conn.execute("SELECT * FROM datasets ORDER BY created_at DESC")
        return [_row_to_dataset(r) for r in await cur.fetchall()]

    async def get_dataset(self, dataset_id: str) -> m.Dataset | None:
        cur = await self._conn.execute("SELECT * FROM datasets WHERE id=?", (dataset_id,))
        row = await cur.fetchone()
        return _row_to_dataset(row) if row else None

    async def dataset_in_use(self, dataset_id: str) -> bool:
        """数据集是否被任务引用（被引用时禁止删除，409）。"""
        cur = await self._conn.execute(
            "SELECT COUNT(*) FROM tasks WHERE dataset_id=?", (dataset_id,)
        )
        row = await cur.fetchone()
        return bool(row and row[0] > 0)

    async def delete_dataset(self, dataset_id: str) -> None:
        await self._conn.execute("DELETE FROM datasets WHERE id=?", (dataset_id,))
        await self._conn.commit()

    # ==================================================================
    # 任务
    # ==================================================================

    async def create_task(
        self,
        name: str,
        dataset_id: str,
        kb_id: str,
        judge_model: str,
        metrics: list[str],
        sample_concurrency: int,
        strategy: str | None,
        total: int,
        idempotency_key: str | None,
        config_snapshot: dict[str, Any],
    ) -> m.Task:
        """新建任务（初始 pending，进度 total 就位）。"""
        task = m.Task(
            id=_new_id(),
            name=name,
            dataset_id=dataset_id,
            kb_id=kb_id,
            judge_model=judge_model,
            metrics=metrics,
            sample_concurrency=sample_concurrency,
            strategy=strategy,
            status=m.STATUS_PENDING,
            progress=m.TaskProgress(total=total),
            idempotency_key=idempotency_key,
            config_snapshot=config_snapshot,
            created_at=_now(),
        )
        await self._conn.execute(
            "INSERT INTO tasks(id,name,dataset_id,kb_id,judge_model,metrics_json,"
            "sample_concurrency,strategy,status,progress_json,error_message,"
            "idempotency_key,config_snapshot_json,created_at) "
            "VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
            (
                task.id,
                task.name,
                task.dataset_id,
                task.kb_id,
                task.judge_model,
                json.dumps(task.metrics, ensure_ascii=False),
                task.sample_concurrency,
                task.strategy,
                task.status,
                json.dumps({"total": total, "collected": 0, "evaluated": 0, "failed": 0}),
                "",
                task.idempotency_key,
                json.dumps(task.config_snapshot, ensure_ascii=False),
                task.created_at,
            ),
        )
        await self._conn.commit()
        return task

    async def get_task(self, task_id: str) -> m.Task | None:
        cur = await self._conn.execute("SELECT * FROM tasks WHERE id=?", (task_id,))
        row = await cur.fetchone()
        return _row_to_task(row) if row else None

    async def find_task_by_idempotency_key(self, key: str) -> m.Task | None:
        """幂等键查重（Idempotency-Key 24h 窗口）。"""
        cur = await self._conn.execute(
            "SELECT * FROM tasks WHERE idempotency_key=?", (key,)
        )
        row = await cur.fetchone()
        return _row_to_task(row) if row else None

    async def clear_idempotency_key(self, task_id: str) -> None:
        """幂等键超窗后释放（旧任务清空 key，允许同 key 再提交新任务）。"""
        await self._conn.execute(
            "UPDATE tasks SET idempotency_key=NULL WHERE id=?", (task_id,)
        )
        await self._conn.commit()

    async def list_tasks(
        self,
        status: str | None = None,
        kb_id: str | None = None,
        page: int = 1,
        page_size: int = 20,
    ) -> tuple[list[m.Task], int]:
        """任务列表（按创建时间倒序，支持状态/知识库过滤与分页），返回 (items, total)。"""
        where, params = [], []
        if status:
            where.append("status=?")
            params.append(status)
        if kb_id:
            where.append("kb_id=?")
            params.append(kb_id)
        cond = f"WHERE {' AND '.join(where)}" if where else ""
        cur = await self._conn.execute(f"SELECT COUNT(*) FROM tasks {cond}", params)
        total = (await cur.fetchone())[0]
        cur = await self._conn.execute(
            f"SELECT * FROM tasks {cond} ORDER BY created_at DESC LIMIT ? OFFSET ?",
            (*params, page_size, (page - 1) * page_size),
        )
        return [_row_to_task(r) for r in await cur.fetchall()], total

    async def update_task_status(
        self,
        task_id: str,
        status: str,
        *,
        error_message: str | None = None,
        set_started: bool = False,
        set_finished: bool = False,
    ) -> None:
        """任务状态迁移落库（合法性由 core/lifecycle 在校验后调用）。"""
        sets, params = ["status=?"], [status]
        if error_message is not None:
            sets.append("error_message=?")
            params.append(error_message)
        if set_started:
            sets.append("started_at=?")
            params.append(_now())
        if set_finished:
            sets.append("finished_at=?")
            params.append(_now())
        params.append(task_id)
        await self._conn.execute(f"UPDATE tasks SET {', '.join(sets)} WHERE id=?", params)
        await self._conn.commit()

    async def update_progress(self, task_id: str, progress: m.TaskProgress) -> None:
        """进度即时落库（每样本状态迁移后调用，进度查询零计算）。"""
        await self._conn.execute(
            "UPDATE tasks SET progress_json=? WHERE id=?",
            (
                json.dumps(
                    {
                        "total": progress.total,
                        "collected": progress.collected,
                        "evaluated": progress.evaluated,
                        "failed": progress.failed,
                    }
                ),
                task_id,
            ),
        )
        await self._conn.commit()

    async def delete_task(self, task_id: str) -> None:
        """删除任务及其样本与报告（samples/reports 有外键级联，显式删保险）。"""
        await self._conn.execute("DELETE FROM samples WHERE task_id=?", (task_id,))
        await self._conn.execute("DELETE FROM reports WHERE task_id=?", (task_id,))
        await self._conn.execute("DELETE FROM tasks WHERE id=?", (task_id,))
        await self._conn.commit()

    async def reset_interrupted_tasks(self) -> list[str]:
        """启动恢复：collecting/evaluating 重置为 pending（样本级断点续跑）。

        返回被重置的任务 id 列表（由队列重新入队）。
        """
        cur = await self._conn.execute(
            "SELECT id FROM tasks WHERE status IN (?,?)",
            (m.STATUS_COLLECTING, m.STATUS_EVALUATING),
        )
        ids = [r["id"] for r in await cur.fetchall()]
        if ids:
            await self._conn.execute(
                "UPDATE tasks SET status=? WHERE status IN (?,?)",
                (m.STATUS_PENDING, m.STATUS_COLLECTING, m.STATUS_EVALUATING),
            )
            await self._conn.commit()
        return ids

    async def list_resumable_task_ids(self) -> list[str]:
        """列出所有 pending 任务（启动时重排队用，含新建未跑与重置的）。"""
        cur = await self._conn.execute(
            "SELECT id FROM tasks WHERE status=? ORDER BY created_at", (m.STATUS_PENDING,)
        )
        return [r["id"] for r in await cur.fetchall()]

    # ==================================================================
    # 样本
    # ==================================================================

    async def insert_samples(self, task_id: str, samples: list[dict[str, Any]]) -> None:
        """任务创建时批量写入样本骨架（状态 pending）。

        samples 元素：{idx, question, reference, kb_id, expected_ids}
        """
        await self._conn.executemany(
            "INSERT INTO samples(id,task_id,idx,question,reference,kb_id,expected_ids_json,"
            "status) VALUES(?,?,?,?,?,?,?,?)",
            [
                (
                    _new_id(),
                    task_id,
                    s["idx"],
                    s["question"],
                    s.get("reference", ""),
                    s.get("kb_id", ""),
                    json.dumps(s.get("expected_ids", []), ensure_ascii=False),
                    m.SAMPLE_PENDING,
                )
                for s in samples
            ],
        )
        await self._conn.commit()

    async def list_samples(self, task_id: str) -> list[m.Sample]:
        """任务全部样本（按 idx 升序）。"""
        cur = await self._conn.execute(
            "SELECT * FROM samples WHERE task_id=? ORDER BY idx", (task_id,)
        )
        return [_row_to_sample(r) for r in await cur.fetchall()]

    async def get_sample(self, task_id: str, sample_id: str) -> m.Sample | None:
        cur = await self._conn.execute(
            "SELECT * FROM samples WHERE task_id=? AND id=?", (task_id, sample_id)
        )
        row = await cur.fetchone()
        return _row_to_sample(row) if row else None

    async def mark_sample_collected(
        self, sample_id: str, answer: str, contexts: list[dict[str, Any]]
    ) -> None:
        """采集成功回写：answer + contexts 正文，状态 collected。"""
        await self._conn.execute(
            "UPDATE samples SET answer=?, contexts_json=?, status=?, error='' WHERE id=?",
            (answer, json.dumps(contexts, ensure_ascii=False), m.SAMPLE_COLLECTED, sample_id),
        )
        await self._conn.commit()

    async def mark_sample_scored(
        self, sample_id: str, scores: dict[str, dict[str, Any]]
    ) -> None:
        """评测完成回写：各指标分数与判定理由，状态 ok。"""
        await self._conn.execute(
            "UPDATE samples SET scores_json=?, status=?, error='' WHERE id=?",
            (json.dumps(scores, ensure_ascii=False), m.SAMPLE_OK, sample_id),
        )
        await self._conn.commit()

    async def mark_sample_error(self, sample_id: str, error: str) -> None:
        """样本级失败回写（不迁移任务状态）。"""
        await self._conn.execute(
            "UPDATE samples SET status=?, error=? WHERE id=?",
            (m.SAMPLE_ERROR, error, sample_id),
        )
        await self._conn.commit()

    # ==================================================================
    # 报告
    # ==================================================================

    async def save_report(self, task_id: str, summary: dict[str, Any]) -> None:
        """任务完成时汇总落表（冗余存储，报告读取不再扫样本表）。"""
        await self._conn.execute(
            "INSERT OR REPLACE INTO reports(task_id,summary_json,created_at) VALUES(?,?,?)",
            (task_id, json.dumps(summary, ensure_ascii=False), _now()),
        )
        await self._conn.commit()

    async def get_report(self, task_id: str) -> m.Report | None:
        cur = await self._conn.execute("SELECT * FROM reports WHERE task_id=?", (task_id,))
        row = await cur.fetchone()
        if not row:
            return None
        return m.Report(
            task_id=row["task_id"],
            summary=json.loads(row["summary_json"]),
            created_at=row["created_at"],
        )
