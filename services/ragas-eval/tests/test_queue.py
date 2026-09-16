"""T9 验证：任务队列与 worker——状态流转与进度、取消生效、重启断点续跑。"""

from __future__ import annotations

import asyncio

import pytest

from ragas_eval.core.queue import QueueFullError
from ragas_eval.store import models as m
from tests.conftest import FakeCollector, make_queue, seed_dataset


async def _wait_status(repo, task_id: str, targets: set[str], timeout: float = 5.0):  # type: ignore[no-untyped-def]
    """轮询等待任务到达目标状态集合（worker 在后台跑）。"""
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        task = await repo.get_task(task_id)
        if task and task.status in targets:
            return task
        await asyncio.sleep(0.02)
    raise AssertionError(f"任务未在 {timeout}s 内到达 {targets}")


async def _make_pending_task(repo) -> tuple:  # type: ignore[no-untyped-def]
    ds, _ = await seed_dataset(repo)
    task = await repo.create_task(
        name="q-test", dataset_id=ds.id, kb_id="", judge_model="m",
        metrics=["faithfulness", "answer_relevancy", "context_precision", "context_recall"],
        sample_concurrency=4, strategy=None, total=3,
        idempotency_key=None, config_snapshot={},
    )
    return ds, task


async def test_task_full_lifecycle(settings, repo):
    """提交→跑完：状态流转 pending→collecting→evaluating→completed，进度与报告正确。"""
    _, task = await _make_pending_task(repo)
    queue, collector, _ = make_queue(settings, repo)
    await queue.start()  # 恢复无中断任务；启动 worker
    try:
        await queue.submit(task.id)
        final = await _wait_status(repo, task.id, {m.STATUS_COMPLETED})
        # 进度：3 样本全部采集+评测，无失败
        assert final.progress.total == 3
        assert final.progress.evaluated == 3
        assert final.progress.failed == 0
        assert final.started_at and final.finished_at
        # 报告落表：四指标汇总
        report = await repo.get_report(task.id)
        assert report is not None
        assert report.summary["faithfulness"]["mean"] == 0.9
        # 缺 reference 样本（第 3 条）context_recall 记 N/A
        assert "缺 reference" in report.summary["context_recall"]["note"]
        # 样本分数与理由落库
        samples = await repo.list_samples(task.id)
        assert all(s.status == m.SAMPLE_OK for s in samples)
        assert samples[0].scores["faithfulness"]["reason"] == "模拟理由"
        assert len(collector.calls) == 3  # 每样本采集一次
    finally:
        await queue.stop()


async def test_sample_failure_does_not_block_task(settings, repo):
    """单样本采集失败不中断任务（AC6），报告中标记样本错误。"""
    _, task = await _make_pending_task(repo)
    samples_questions = {"支持哪些文档格式？"}  # 第 3 条采集失败
    queue, _, _ = make_queue(settings, repo, collector=FakeCollector(fail_on=samples_questions))
    await queue.start()
    try:
        await queue.submit(task.id)
        final = await _wait_status(repo, task.id, {m.STATUS_COMPLETED})
        assert final.progress.failed == 1
        assert final.progress.evaluated == 2
        samples = await repo.list_samples(task.id)
        err = [s for s in samples if s.status == m.SAMPLE_ERROR]
        assert len(err) == 1 and "模拟采集失败" in err[0].error
        # 报告 coverage 反映部分失败
        report = await repo.get_report(task.id)
        assert report is not None
        assert report.summary["error_samples"] == 1
    finally:
        await queue.stop()


async def test_cancel_running_task(settings, repo):
    """取消生效：采集中途取消 → 状态变 canceled（AC7）。"""
    _, task = await _make_pending_task(repo)

    # 采集器 double：每样本挂起 0.05s，给取消留出窗口
    class SlowCollector(FakeCollector):
        async def collect_one(self, question, kb_id="", strategy=None):  # type: ignore[no-untyped-def]
            await asyncio.sleep(0.05)
            return await super().collect_one(question, kb_id, strategy)

    queue, _, _ = make_queue(settings, repo, collector=SlowCollector())
    await queue.start()
    try:
        await queue.submit(task.id)
        await _wait_status(repo, task.id, {m.STATUS_COLLECTING, m.STATUS_EVALUATING})
        queue.request_cancel(task.id)
        final = await _wait_status(repo, task.id, {m.STATUS_CANCELED, m.STATUS_COMPLETED})
        assert final.status == m.STATUS_CANCELED
        assert final.finished_at is not None
    finally:
        await queue.stop()


async def test_restart_resume_skips_ok_samples(settings, repo):
    """模拟重启断点续跑：evaluating 重置 pending，已 ok 样本跳过（样本级幂等）。"""
    ds, task = await _make_pending_task(repo)
    # 预置样本：1 条已 ok（含分数），2 条待处理，任务卡在 evaluating
    await repo.insert_samples(
        task.id,
        [
            {"idx": 0, "question": "BinRag 用什么语言实现？", "reference": "Go 语言。",
             "kb_id": "kb-1", "expected_ids": ["c1"]},
            {"idx": 1, "question": "向量存储用什么？", "reference": "Qdrant。",
             "kb_id": "", "expected_ids": ["c2"]},
            {"idx": 2, "question": "支持哪些文档格式？", "reference": "",
             "kb_id": "", "expected_ids": []},
        ],
    )
    samples = await repo.list_samples(task.id)
    await repo.mark_sample_collected(samples[0].id, "旧回答", [{"id": "c1", "content": "旧上下文"}])
    await repo.mark_sample_scored(samples[0].id, {"faithfulness": {"score": 0.5, "reason": "旧"}})
    await repo.update_task_status(task.id, m.STATUS_COLLECTING)
    await repo.update_task_status(task.id, m.STATUS_EVALUATING)

    queue, collector, _ = make_queue(settings, repo)
    await queue.start()  # 启动恢复：evaluating → pending 重排队
    try:
        # start() 已自动重排队，无需手动 submit
        final = await _wait_status(repo, task.id, {m.STATUS_COMPLETED})
        assert final.status == m.STATUS_COMPLETED
        # 已 ok 样本被跳过：采集器只被调用 2 次（样本 1、2）
        assert len(collector.calls) == 2
        # 旧样本分数未被覆盖
        s0 = await repo.get_sample(task.id, samples[0].id)
        assert s0 is not None and s0.scores["faithfulness"]["score"] == 0.5
    finally:
        await queue.stop()


async def test_queue_full_429(settings, repo):
    """队列满拒绝新任务（QueueFullError → API 429）。"""
    ds, _ = await seed_dataset(repo)
    queue, _, _ = make_queue(settings, repo)  # max_queue_size=4
    # 不启动 worker：任务全堆在队列里
    ids = []
    for i in range(4):
        t = await repo.create_task(
            name=f"t{i}", dataset_id=ds.id, kb_id="", judge_model="m",
            metrics=["faithfulness"], sample_concurrency=4, strategy=None,
            total=3, idempotency_key=None, config_snapshot={},
        )
        await queue.submit(t.id)
        ids.append(t.id)
    extra = await repo.create_task(
        name="extra", dataset_id=ds.id, kb_id="", judge_model="m",
        metrics=["faithfulness"], sample_concurrency=4, strategy=None,
        total=3, idempotency_key=None, config_snapshot={},
    )
    with pytest.raises(QueueFullError):
        await queue.submit(extra.id)
    assert queue.stats()["queued"] == 4
