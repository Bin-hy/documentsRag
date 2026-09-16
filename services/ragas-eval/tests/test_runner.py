"""T8 验证：RAGAS 执行器——映射正确、降级路径、部分失败回写。

用注入的 evaluate_fn（测试 double）替代真实 ragas.evaluate。
"""

from __future__ import annotations

import math

from ragas_eval.core.runner import (
    RagasRunner,
    RunnerSample,
    SampleScore,
    metrics_for_sample,
    summarize,
)

ALL = ["faithfulness", "answer_relevancy", "context_precision", "context_recall"]


def _sample(idx: int, reference: str = "标准答案") -> RunnerSample:
    return RunnerSample(
        idx=idx,
        question=f"问题{idx}",
        reference=reference,
        answer=f"回答{idx}",
        contexts=[f"上下文{idx}"],
    )


def test_metrics_dispatch_with_reference():
    """reference 存在 → 跑全部四指标。"""
    assert metrics_for_sample(ALL, has_reference=True) == ALL


def test_metrics_dispatch_without_reference():
    """reference 缺失 → context_recall 剔除（无法降级），其余保留。"""
    got = metrics_for_sample(ALL, has_reference=False)
    assert got == ["faithfulness", "answer_relevancy", "context_precision"]


def test_metrics_dispatch_filters_unknown():
    """未知指标名被过滤（API 层 400 之外的兜底）。"""
    got = metrics_for_sample(["faithfulness", "bogus"], has_reference=True)
    assert got == ["faithfulness"]


async def test_evaluate_mapping_and_scores(settings):
    """映射正确：evaluate_fn 收到的样本字段与分派指标符合映射表。"""
    captured: dict = {}

    async def fake_eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
        captured["samples"] = samples
        captured["metrics"] = metrics
        captured["has_ref"] = has_ref
        return [
            {**{m: 0.8 for m in metrics}, "_reasons": {m: f"理由-{m}" for m in metrics}}
            for _ in samples
        ]

    runner = RagasRunner(settings, evaluate_fn=fake_eval)
    results = await runner.evaluate_samples([_sample(0)], ALL)
    assert len(results) == 1
    # SingleTurnSample 映射：question/answer/contexts/reference 就位
    s = captured["samples"][0]
    assert s.question == "问题0" and s.answer == "回答0"
    assert s.contexts == ["上下文0"] and s.reference == "标准答案"
    assert captured["has_ref"] is True
    # 分数与判定理由回填
    assert results[0].scores["faithfulness"] == {"score": 0.8, "reason": "理由-faithfulness"}


async def test_degradation_without_reference(settings):
    """降级路径：缺 reference 样本 context_recall 记 N/A，其余指标正常。"""
    async def fake_eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
        assert has_ref is False
        assert "context_recall" not in metrics  # 降级表：recall 剔除
        return [{m: 0.7 for m in metrics} for _ in samples]

    runner = RagasRunner(settings, evaluate_fn=fake_eval)
    results = await runner.evaluate_samples([_sample(0, reference="")], ALL)
    r = results[0]
    assert r.error == ""
    assert r.scores["context_recall"]["score"] is None
    assert "缺标准答案" in r.scores["context_recall"]["reason"]
    assert r.scores["faithfulness"]["score"] == 0.7


async def test_batch_failure_fallback_and_sample_error(settings):
    """批内失败降级逐样本重试一次，再失败记样本错误（不阻断整体）。"""
    calls: list[list[int]] = []

    async def fake_eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
        calls.append([s.idx for s in samples])
        if len(samples) > 1:
            raise RuntimeError("批整体失败")  # 批路径失败
        if samples[0].idx == 1:
            raise RuntimeError("样本 1 重试仍失败")
        return [{m: 0.6 for m in metrics} for _ in samples]

    runner = RagasRunner(settings, evaluate_fn=fake_eval)
    results = await runner.evaluate_samples([_sample(0), _sample(1)], ["faithfulness"])
    # 第一次调用是整批 [0,1]，失败后逐样本重试
    assert calls[0] == [0, 1]
    assert [0] in calls and [1] in calls
    assert results[0].error == "" and results[0].scores["faithfulness"]["score"] == 0.6
    assert "重试仍失败" in results[1].error


async def test_nan_score_normalized_to_none(settings):
    """ragas 解析失败记 NaN 的口径：NaN 统一转 None（均值计算剔除）。"""
    async def fake_eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
        return [{m: math.nan for m in metrics} for _ in samples]

    runner = RagasRunner(settings, evaluate_fn=fake_eval)
    results = await runner.evaluate_samples([_sample(0)], ["faithfulness"])
    assert results[0].scores["faithfulness"]["score"] is None


async def test_should_cancel_marks_remaining(settings):
    """协作式取消：批次边界命中取消标志，剩余样本记取消。"""
    async def fake_eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
        return [{m: 0.5 for m in metrics} for _ in samples]

    settings.eval_batch_size = 1  # 每批 1 个样本，第二批即触发取消
    runner = RagasRunner(settings, evaluate_fn=fake_eval)
    calls = {"n": 0}

    def should_cancel() -> bool:
        calls["n"] += 1
        return calls["n"] > 1  # 第一批不取消，第二批起取消

    results = await runner.evaluate_samples([_sample(0), _sample(1)], ["faithfulness"], should_cancel)
    assert results[0].error == ""
    assert results[1].error == "任务已取消"


def test_summarize_mean_coverage_degradation():
    """汇总：mean/coverage/valid_samples + context_recall N/A 标注 + precision 降级标注。"""
    scores = [
        SampleScore(idx=0, scores={
            "faithfulness": {"score": 0.8, "reason": "r"},
            "context_recall": {"score": 1.0, "reason": "r"},
            "context_precision": {"score": 0.5, "reason": "r"},
        }),
        SampleScore(idx=1, scores={
            "faithfulness": {"score": 1.0, "reason": "r"},
            "context_recall": {"score": None, "reason": "N/A（缺标准答案）"},
            "context_precision": {"score": 0.7, "reason": "r"},
        }),
        SampleScore(idx=2, error="采集失败"),  # 错误样本不计入
    ]
    summary = summarize(scores, ["faithfulness", "context_precision", "context_recall"])
    assert summary["faithfulness"]["mean"] == 0.9
    assert summary["faithfulness"]["coverage"] == round(2 / 3, 4)
    assert summary["faithfulness"]["valid_samples"] == 2
    # context_recall：1 条有效 + 1 条 N/A
    assert summary["context_recall"]["valid_samples"] == 1
    assert "缺 reference" in summary["context_recall"]["note"]
    # context_precision：存在缺 reference 样本 → 标注降级口径
    assert summary["context_precision"]["degraded"] is True
    assert summary["error_samples"] == 1
    assert summary["reliable"] is True


def test_summarize_parse_failure_rate_blocks_conclusion():
    """解析失败率 > 5% 时结论标记不可靠（prompt-design §5.4）。"""
    # 20 个样本全部解析失败（score=None 且非缺 reference N/A）
    scores = [
        SampleScore(idx=i, scores={"faithfulness": {"score": None, "reason": "解析失败"}})
        for i in range(20)
    ]
    summary = summarize(scores, ["faithfulness"])
    assert summary["parse_failure_rate"] == 1.0
    assert summary["reliable"] is False
    assert summary["faithfulness"]["mean"] is None
