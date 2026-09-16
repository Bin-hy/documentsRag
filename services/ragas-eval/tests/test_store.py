"""T3 验证：SQLite 存储层——建库→插入任务/样本→查询→进度更新→汇总落表。"""

from __future__ import annotations

from ragas_eval.store import models as m
from tests.conftest import seed_dataset


async def test_dataset_crud(repo):
    """数据集：建库落表 → 查询 → 引用检查 → 删除。"""
    ds, parsed = await seed_dataset(repo)
    assert ds.sample_count == 3
    assert ds.with_reference_count == 2
    assert ds.content_hash.startswith("sha256:")
    assert ds.source_format == "json"

    fetched = await repo.get_dataset(ds.id)
    assert fetched is not None and fetched.name == "测试评测集"

    listed = await repo.list_datasets()
    assert len(listed) == 1

    assert await repo.dataset_in_use(ds.id) is False
    await repo.delete_dataset(ds.id)
    assert await repo.get_dataset(ds.id) is None


async def test_task_sample_progress_report_flow(repo):
    """任务/样本/进度/报告全链路（task.md T3 验证项）。"""
    ds, _ = await seed_dataset(repo)
    task = await repo.create_task(
        name="t1",
        dataset_id=ds.id,
        kb_id="kb-1",
        judge_model="judge-model-a",
        metrics=["faithfulness", "answer_relevancy"],
        sample_concurrency=4,
        strategy=None,
        total=3,
        idempotency_key="key-1",
        config_snapshot={"dataset_hash": ds.content_hash},
    )
    assert task.status == m.STATUS_PENDING
    assert task.progress.total == 3

    # 幂等键查重
    dup = await repo.find_task_by_idempotency_key("key-1")
    assert dup is not None and dup.id == task.id

    # 数据集被引用
    assert await repo.dataset_in_use(ds.id) is True

    # 样本批量写入
    await repo.insert_samples(
        task.id,
        [
            {"idx": 0, "question": "q0", "reference": "a0", "kb_id": "kb-1", "expected_ids": ["x"]},
            {"idx": 1, "question": "q1", "reference": "", "kb_id": "", "expected_ids": []},
        ],
    )
    samples = await repo.list_samples(task.id)
    assert len(samples) == 2
    assert samples[0].status == m.SAMPLE_PENDING
    assert samples[0].expected_ids == ["x"]

    # 采集回写 → 评分回写
    await repo.mark_sample_collected(
        samples[0].id, "回答0", [{"id": "c1", "content": "正文"}]
    )
    await repo.mark_sample_scored(
        samples[0].id, {"faithfulness": {"score": 0.9, "reason": "一致"}}
    )
    await repo.mark_sample_error(samples[1].id, "采集超时")

    s0 = await repo.get_sample(task.id, samples[0].id)
    assert s0 is not None and s0.status == m.SAMPLE_OK
    assert s0.scores["faithfulness"]["score"] == 0.9
    assert s0.scores["faithfulness"]["reason"] == "一致"
    s1 = await repo.get_sample(task.id, samples[1].id)
    assert s1 is not None and s1.status == m.SAMPLE_ERROR and "超时" in s1.error

    # 进度更新
    await repo.update_progress(task.id, m.TaskProgress(total=3, collected=2, evaluated=1, failed=1))
    fetched = await repo.get_task(task.id)
    assert fetched is not None
    assert fetched.progress.collected == 2
    assert fetched.progress.failed == 1

    # 状态迁移落库
    await repo.update_task_status(task.id, m.STATUS_COMPLETED, set_finished=True)
    fetched = await repo.get_task(task.id)
    assert fetched is not None and fetched.status == m.STATUS_COMPLETED
    assert fetched.finished_at is not None

    # 汇总落表与读取
    summary = {"faithfulness": {"mean": 0.9, "coverage": 1.0, "valid_samples": 1}}
    await repo.save_report(task.id, summary)
    report = await repo.get_report(task.id)
    assert report is not None
    assert report.summary["faithfulness"]["mean"] == 0.9

    # 列表过滤与分页
    items, total = await repo.list_tasks(status=m.STATUS_COMPLETED)
    assert total == 1 and items[0].id == task.id
    items, total = await repo.list_tasks(status=m.STATUS_PENDING)
    assert total == 0

    # 删除任务级联清理样本与报告
    await repo.delete_task(task.id)
    assert await repo.get_task(task.id) is None
    assert await repo.list_samples(task.id) == []
    assert await repo.get_report(task.id) is None


async def test_reset_interrupted_tasks(repo):
    """启动恢复：collecting/evaluating 重置 pending。"""
    ds, _ = await seed_dataset(repo)
    t1 = await repo.create_task(
        name="a", dataset_id=ds.id, kb_id="", judge_model="m",
        metrics=["faithfulness"], sample_concurrency=4, strategy=None,
        total=1, idempotency_key=None, config_snapshot={},
    )
    t2 = await repo.create_task(
        name="b", dataset_id=ds.id, kb_id="", judge_model="m",
        metrics=["faithfulness"], sample_concurrency=4, strategy=None,
        total=1, idempotency_key=None, config_snapshot={},
    )
    await repo.update_task_status(t1.id, m.STATUS_COLLECTING)
    await repo.update_task_status(t2.id, m.STATUS_EVALUATING)

    reset_ids = await repo.reset_interrupted_tasks()
    assert set(reset_ids) == {t1.id, t2.id}
    assert (await repo.get_task(t1.id)).status == m.STATUS_PENDING  # type: ignore[union-attr]
    assert (await repo.get_task(t2.id)).status == m.STATUS_PENDING  # type: ignore[union-attr]

    resumable = await repo.list_resumable_task_ids()
    assert t1.id in resumable and t2.id in resumable


async def test_dataset_jsonl_format(repo):
    """JSONL 数据集落库（source_format=jsonl）。"""
    content = b'{"question": "q1", "answer": "a1", "expected_ids": []}\n{"question": "q2", "expected_ids": ["x"]}\n'
    ds, parsed = await seed_dataset(repo, content, "ds.jsonl")
    assert ds.source_format == "jsonl"
    assert ds.sample_count == 2
    assert ds.with_reference_count == 1
    assert any("缺 reference" in w for w in parsed.warnings)
