"""任务队列与 worker（T9）：进程内 asyncio.Queue + worker pool。

- 任务级并发：worker 数 = max_running_tasks（默认 2），超出排队，队列满 429
- worker 流程：collecting（采集器）→ evaluating（执行器）→ 汇总落 reports → completed
- 取消：样本边界协作式（每样本检查取消标志，进行中的 LLM 调用不硬断）
- 任务级 deadline：默认 2h，到期迁移 failed
- 启动恢复：collecting/evaluating 重置 pending 重排队，已 ok 样本跳过（断点续跑）

collector 是唯一与 Go 通信的模块、runner 是唯一触达模型供应商的执行器，
两者只被本模块的 worker 调用（受任务生命周期管辖，architect-design §3）。
"""

from __future__ import annotations

import asyncio
import contextlib
import time
from collections.abc import Callable

from ragas_eval.config import Settings
from ragas_eval.core import lifecycle
from ragas_eval.core.collector import CollectError, SampleCollector
from ragas_eval.core.dataset import samples_from_raw_blob
from ragas_eval.core.runner import RagasRunner, RunnerSample, SampleScore, summarize
from ragas_eval.store import models as m
from ragas_eval.store.repo import Repo


class QueueFullError(RuntimeError):
    """排队已满（queued >= max_queue_size），API 层映射 429。"""


class TaskQueue:
    """评测任务队列：提交入队、worker 执行、取消与恢复。

    collector / runner 以工厂注入（测试可替换为 double），
    不在 API 层直接持有外部通信客户端。
    """

    def __init__(
        self,
        settings: Settings,
        repo: Repo,
        collector_factory: Callable[[int], SampleCollector],
        runner_factory: Callable[[], RagasRunner],
    ) -> None:
        self._settings = settings
        self._repo = repo
        self._collector_factory = collector_factory
        self._runner_factory = runner_factory
        self._queue: asyncio.Queue[str] = asyncio.Queue()
        self._workers: list[asyncio.Task] = []
        self._cancel_flags: dict[str, asyncio.Event] = {}  # task_id -> 取消标志
        self._running: set[str] = set()  # 正在运行的任务 id

    # ==================================================================
    # 生命周期
    # ==================================================================

    async def start(self) -> None:
        """启动 worker pool 并恢复中断任务（重启断点续跑）。"""
        # collecting/evaluating → pending（样本级幂等：已 ok 样本跳过）
        await self._repo.reset_interrupted_tasks()
        for _ in range(self._settings.max_running_tasks):
            self._workers.append(asyncio.create_task(self._worker_loop()))
        # 重排所有 pending 任务（新建未跑 + 重置恢复的）
        for task_id in await self._repo.list_resumable_task_ids():
            self._queue.put_nowait(task_id)

    async def stop(self) -> None:
        """关停钩子：取消 worker 任务（进行中的样本调用随 CancelledError 中断）。"""
        for w in self._workers:
            w.cancel()
        for w in self._workers:
            with contextlib.suppress(asyncio.CancelledError):
                await w
        self._workers.clear()

    # ==================================================================
    # 提交 / 取消 / 观测
    # ==================================================================

    async def submit(self, task_id: str) -> None:
        """任务入队；排队数超 max_queue_size 抛 QueueFullError（429）。"""
        if self._queue.qsize() >= self._settings.max_queue_size:
            raise QueueFullError(
                f"评测队列已满（排队 {self._queue.qsize()}，上限 {self._settings.max_queue_size}）"
            )
        self._cancel_flags.setdefault(task_id, asyncio.Event())
        self._queue.put_nowait(task_id)

    def request_cancel(self, task_id: str) -> None:
        """置取消标志：worker 在样本边界生效（协作式取消）。"""
        self._cancel_flags.setdefault(task_id, asyncio.Event()).set()

    def _is_cancelled(self, task_id: str) -> bool:
        flag = self._cancel_flags.get(task_id)
        return bool(flag and flag.is_set())

    def stats(self) -> dict[str, int]:
        """队列观测（health.checks.queue 用）。"""
        return {"running": len(self._running), "queued": self._queue.qsize()}

    # ==================================================================
    # worker 主循环
    # ==================================================================

    async def _worker_loop(self) -> None:
        """worker 常驻循环：领任务 → 执行 → 清理。"""
        while True:
            task_id = await self._queue.get()
            self._running.add(task_id)
            try:
                await self._run_task(task_id)
            except asyncio.CancelledError:
                raise  # 关停钩子取消：直接退出
            except Exception as exc:  # noqa: BLE001 —— 任务级错误兜底
                await self._fail_task(task_id, f"任务执行异常: {exc}")
            finally:
                self._running.discard(task_id)
                self._cancel_flags.pop(task_id, None)
                self._queue.task_done()

    async def _run_task(self, task_id: str) -> None:
        """单任务执行：collecting → evaluating → 汇总落库 → completed。"""
        task = await self._repo.get_task(task_id)
        if task is None:
            return  # 任务已被删除，静默跳过
        # worker 只承接 pending 任务：重启恢复已把 collecting/evaluating 重置为 pending，
        # 其他状态（如排队期间被取消/重复入队的终态任务）直接跳过
        if task.status != m.STATUS_PENDING:
            return

        started_monotonic = time.monotonic()
        dataset = await self._repo.get_dataset(task.dataset_id)
        if dataset is None:
            await self._fail_task(task_id, "数据集不存在（可能已被删除）")
            return
        eval_samples = samples_from_raw_blob(dataset.raw_blob, dataset.source_format)

        # 首次执行时写入样本骨架（重启续跑时已存在则跳过）
        existing = await self._repo.list_samples(task_id)
        if not existing:
            await self._repo.insert_samples(
                task_id,
                [
                    {
                        "idx": i,
                        "question": s.question,
                        "reference": s.answer,
                        "kb_id": s.kb_id or task.kb_id,
                        "expected_ids": s.expected_ids,
                    }
                    for i, s in enumerate(eval_samples)
                ],
            )
            existing = await self._repo.list_samples(task_id)

        def deadline_exceeded() -> bool:
            return time.monotonic() - started_monotonic > self._settings.task_deadline

        # ---------------- 阶段一：采集（collecting） ----------------
        lifecycle.ensure_transition(task.status, m.STATUS_COLLECTING)
        await self._repo.update_task_status(task_id, m.STATUS_COLLECTING, set_started=True)

        collector = self._collector_factory(task.sample_concurrency)
        progress = m.TaskProgress(total=len(existing))
        # 待采集样本（断点续跑：collected/ok 跳过）
        pending_samples = [s for s in existing if s.status == m.SAMPLE_PENDING]
        progress.collected = len(existing) - len(pending_samples)
        # 按样本并发窗口分批采集：窗口内并发（信号量限流），窗口间检查取消/deadline
        window = max(1, task.sample_concurrency)
        for start in range(0, len(pending_samples), window):
            if self._is_cancelled(task_id):
                await self._cancel_task(task_id, progress)
                return
            if deadline_exceeded():
                await self._fail_task(task_id, "超过任务级 deadline，强制终止", progress)
                return
            chunk = pending_samples[start : start + window]
            results = await collector.collect_many(
                [{"question": s.question, "kb_id": s.kb_id} for s in chunk],
                strategy=task.strategy,
            )
            for sample_row, result in zip(chunk, results, strict=True):
                if isinstance(result, CollectError):
                    # 单样本采集失败不阻断任务（AC6），记样本 error
                    await self._repo.mark_sample_error(sample_row.id, str(result))
                    progress.failed += 1
                else:
                    await self._repo.mark_sample_collected(
                        sample_row.id, result.answer, result.contexts
                    )
                    progress.collected += 1
            await self._repo.update_progress(task_id, progress)

        # ---------------- 阶段二：评测（evaluating） ----------------
        # 采集循环结束后、进入评测前再查一次取消/DB 状态，避免翻活已取消任务
        current = await self._repo.get_task(task_id)
        if current is None:
            return
        if self._is_cancelled(task_id) or current.status == m.STATUS_CANCELED:
            await self._cancel_task(task_id, progress)
            return
        lifecycle.ensure_transition(current.status, m.STATUS_EVALUATING)
        await self._repo.update_task_status(task_id, m.STATUS_EVALUATING)

        # 只评测已采集且未完成的样本（collected；ok 为断点续跑已完成）
        fresh = await self._repo.list_samples(task_id)
        to_eval = [s for s in fresh if s.status == m.SAMPLE_COLLECTED]
        progress.evaluated = sum(1 for s in fresh if s.status == m.SAMPLE_OK)
        progress.failed = sum(1 for s in fresh if s.status == m.SAMPLE_ERROR)
        progress.collected = sum(
            1 for s in fresh if s.status in (m.SAMPLE_COLLECTED, m.SAMPLE_OK)
        )
        await self._repo.update_progress(task_id, progress)

        runner = self._runner_factory()
        runner_samples = [
            RunnerSample(
                idx=s.idx,
                question=s.question,
                reference=s.reference,
                answer=s.answer,
                contexts=[c.get("content", "") for c in s.contexts],
            )
            for s in to_eval
        ]
        if self._is_cancelled(task_id):
            await self._cancel_task(task_id, progress)
            return
        scores = await runner.evaluate_samples(
            runner_samples,
            task.metrics,
            should_cancel=lambda: self._is_cancelled(task_id) or deadline_exceeded(),
        )

        by_idx = {s.idx: s for s in to_eval}
        for ss in scores:
            sample_row = by_idx.get(ss.idx)
            if sample_row is None:
                continue
            if ss.error == "任务已取消":
                await self._cancel_task(task_id, progress)
                return
            if ss.error:
                await self._repo.mark_sample_error(sample_row.id, ss.error)
                progress.failed += 1
            else:
                await self._repo.mark_sample_scored(sample_row.id, ss.scores)
                progress.evaluated += 1
            await self._repo.update_progress(task_id, progress)
            if deadline_exceeded():
                await self._fail_task(task_id, "超过任务级 deadline，强制终止", progress)
                return

        # ---------------- 阶段三：汇总落库（completed） ----------------
        final_samples = await self._repo.list_samples(task_id)
        all_scores = [
            _row_to_sample_score(s)
            for s in final_samples
        ]
        summary = summarize(all_scores, task.metrics)
        await self._repo.save_report(task_id, summary)

        # 完成判定：ok + failed == total（允许部分样本失败）
        ok_count = sum(1 for s in final_samples if s.status == m.SAMPLE_OK)
        err_count = sum(1 for s in final_samples if s.status == m.SAMPLE_ERROR)
        if not lifecycle.can_complete(len(final_samples), ok_count, err_count):
            await self._fail_task(task_id, "样本未全部到达终态，无法完成", progress)
            return
        # 最终取消检查：避免与 cancel 端点的状态迁移竞态（取消了就不许翻回 completed）
        if self._is_cancelled(task_id):
            await self._cancel_task(task_id, progress)
            return
        # 任务可能已被 cancel 端点直接落 canceled：以 DB 现状为准再迁移
        current = await self._repo.get_task(task_id)
        if current is None or current.status != m.STATUS_EVALUATING:
            return
        lifecycle.ensure_transition(current.status, m.STATUS_COMPLETED)
        progress.collected = sum(
            1 for s in final_samples if s.status in (m.SAMPLE_COLLECTED, m.SAMPLE_OK)
        )
        await self._repo.update_progress(task_id, progress)
        await self._repo.update_task_status(task_id, m.STATUS_COMPLETED, set_finished=True)

    # ==================================================================
    # 状态迁移辅助
    # ==================================================================

    async def _cancel_task(self, task_id: str, progress: m.TaskProgress) -> None:
        """协作式取消落库：status=canceled + finished_at。"""
        task = await self._repo.get_task(task_id)
        if task and lifecycle.can_transition(task.status, m.STATUS_CANCELED):
            await self._repo.update_progress(task_id, progress)
            await self._repo.update_task_status(task_id, m.STATUS_CANCELED, set_finished=True)

    async def _fail_task(
        self, task_id: str, message: str, progress: m.TaskProgress | None = None
    ) -> None:
        """任务级失败落库：status=failed + error_message + finished_at。"""
        task = await self._repo.get_task(task_id)
        if task and lifecycle.can_transition(task.status, m.STATUS_FAILED):
            if progress is not None:
                await self._repo.update_progress(task_id, progress)
            await self._repo.update_task_status(
                task_id, m.STATUS_FAILED, error_message=message, set_finished=True
            )


def _row_to_sample_score(s: m.Sample) -> SampleScore:
    """样本表行 → 执行器汇总输入（含错误样本，summarize 内部剔除）。"""
    return SampleScore(idx=s.idx, scores=s.scores, error=s.error if s.status == m.SAMPLE_ERROR else "")
