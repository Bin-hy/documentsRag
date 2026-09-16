"""RAGAS 执行器（T8）：SingleTurnSample 映射、指标分派与降级、批量评测。

对 RAGAS 的使用收敛为本文件（architect-design §3）：
「构造 EvaluationDataset → 选指标列表 → evaluate → 逐样本分数回写」，
RAGAS 版本升级只动这里。

降级表（prompt-design §4.3）：
- reference 存在 → 跑全部四指标
- reference 缺失 → faithfulness + answer_relevancy + context_precision(无参考变体)
                   context_recall 记 N/A（score=None，报告中标注）

容错（architect-design §4.1）：批内失败降级为逐样本重试一次，再失败记样本错误。
"""

from __future__ import annotations

import asyncio
import math
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any

from ragas_eval.config import ALL_METRICS, Settings
from ragas_eval.core.prompts import PROMPT_VERSION, apply_chinese_prompts

# 无参考变体指标名（context_precision 降级后语义：上下文对得出「该回答」是否有用）
METRIC_CONTEXT_PRECISION_NR = "context_precision_no_reference"


@dataclass
class RunnerSample:
    """执行器输入样本（已由采集器填充 answer/contexts）。"""

    idx: int
    question: str
    reference: str  # 空 = 缺标准答案
    answer: str
    contexts: list[str]  # 检索上下文正文（按排名顺序）


@dataclass
class SampleScore:
    """单样本评测结果：scores 键为指标名，值为 {score, reason}。

    score 为 None 表示该指标 N/A（如缺 reference 的 context_recall）
    或解析失败记 NaN 后的空值；error 非空表示样本级评测失败。
    """

    idx: int
    scores: dict[str, dict[str, Any]] = field(default_factory=dict)
    error: str = ""


# evaluate 调用签名：注入便于测试（mock ragas.evaluate 或测试 double）
# 入参：(samples: list[RunnerSample], metrics: list[str], has_reference: bool)
# 返回：list[dict]，每项 {metric: score(float|None)}，可选 "_reasons": {metric: str}
EvaluateFn = Callable[[list[RunnerSample], list[str], bool], Awaitable[list[dict[str, Any]]]]


def metrics_for_sample(requested: list[str], has_reference: bool) -> list[str]:
    """按降级表分派单样本实际运行的指标列表。

    reference 缺失时剔除 context_recall（无法降级），context_precision
    在构建 ragas 指标时换用无参考变体（指标名不变，summary 标注 degraded）。
    """
    if has_reference:
        return [m for m in requested if m in ALL_METRICS]
    return [m for m in requested if m in ALL_METRICS and m != "context_recall"]


def build_metrics(metric_names: list[str], has_reference: bool, llm: Any, embeddings: Any) -> list[Any]:
    """按指标名构造 ragas 指标实例并覆写中文提示词。

    延迟导入 ragas：测试用注入 evaluate_fn 时不依赖 ragas 重依赖链。
    """
    from ragas.metrics import (
        AnswerRelevancy,
        Faithfulness,
        LLMContextPrecisionWithoutReference,
        LLMContextPrecisionWithReference,
        LLMContextRecall,
    )

    built: list[Any] = []
    for name in metric_names:
        if name == "faithfulness":
            built.append(Faithfulness(llm=llm))
        elif name == "answer_relevancy":
            built.append(AnswerRelevancy(llm=llm, embeddings=embeddings))
        elif name == "context_precision":
            # reference 缺失降级：无参考变体（语义变化，summary 需标注 degraded）
            if has_reference:
                built.append(LLMContextPrecisionWithReference(llm=llm))
            else:
                built.append(LLMContextPrecisionWithoutReference(llm=llm))
        elif name == "context_recall":
            built.append(LLMContextRecall(llm=llm))
    apply_chinese_prompts(built)  # 整段覆写中文 Prompt（PROMPT_VERSION=zh-v1）
    return built


async def _default_evaluate(
    samples: list[RunnerSample], metric_names: list[str], has_reference: bool,
    llm: Any, embeddings: Any,
) -> list[dict[str, Any]]:
    """默认 evaluate：调 ragas.evaluate（按批喂 EvaluationDataset）。

    ragas.evaluate 为同步阻塞 API，放线程池避免卡住事件循环。
    """
    from ragas import EvaluationDataset, SingleTurnSample, evaluate

    def _run() -> list[dict[str, Any]]:
        dataset = EvaluationDataset(
            samples=[
                SingleTurnSample(
                    user_input=s.question,  # 用户问题
                    response=s.answer,  # Go chat 回答
                    retrieved_contexts=s.contexts,  # 检索上下文正文
                    reference=s.reference or None,  # 标准答案（可缺失）
                )
                for s in samples
            ]
        )
        metrics = build_metrics(metric_names, has_reference, llm, embeddings)
        result = evaluate(dataset=dataset, metrics=metrics, llm=llm, embeddings=embeddings)
        # EvaluationResult → 逐样本分数字典列表
        rows = result.to_pandas().to_dict(orient="records")
        # 列名归一化（T22 联调实测）：ragas 结果列名用指标实例的 name
        # （如 context_precision → "llm_context_precision_with_reference"），
        # 与内部规范名不一致，按位置映射回规范名，否则分数永远取不到。
        for row in rows:
            for canonical, metric in zip(metric_names, metrics, strict=False):
                ragas_name = getattr(metric, "name", canonical)
                if ragas_name != canonical and ragas_name in row and canonical not in row:
                    row[canonical] = row[ragas_name]
        return rows

    return await asyncio.to_thread(_run)


class RagasRunner:
    """RAGAS 执行器：批量评测 + 批内失败降级逐样本重试 + 分数回收。

    evaluate_fn 可注入（测试 double / mock ragas.evaluate）；
    缺省走 _default_evaluate（真实 ragas 链路）。
    """

    def __init__(
        self,
        settings: Settings,
        llm: Any = None,
        embeddings: Any = None,
        evaluate_fn: Callable[..., Awaitable[list[dict[str, Any]]]] | None = None,
    ) -> None:
        self._settings = settings
        self._llm = llm
        self._embeddings = embeddings
        self._batch_size = max(1, settings.eval_batch_size)
        if evaluate_fn is not None:
            self._evaluate = evaluate_fn
        else:
            async def _fn(samples: list[RunnerSample], metrics: list[str], has_ref: bool):
                return await _default_evaluate(samples, metrics, has_ref, self._llm, self._embeddings)

            self._evaluate = _fn

    async def evaluate_samples(
        self,
        samples: list[RunnerSample],
        requested_metrics: list[str],
        should_cancel: Callable[[], bool] | None = None,
    ) -> list[SampleScore]:
        """评测一批样本（按 eval_batch_size 切批喂 ragas.evaluate）。

        返回与输入等长的 SampleScore 列表（按输入顺序）；样本级失败不抛出
        （error 字段承载）。should_cancel 在批次边界检查取消标志（协作式取消）。
        """
        # 先为每个样本准备好结果壳（含 context_recall 的 N/A 预填）
        shells: dict[int, SampleScore] = {}
        for sample in samples:
            shell = SampleScore(idx=sample.idx)
            # context_recall 缺 reference：记 N/A（score=None），报告标注
            if not sample.reference.strip() and "context_recall" in requested_metrics:
                shell.scores["context_recall"] = {
                    "score": None,
                    "reason": "N/A（缺标准答案）",
                }
            shells[sample.idx] = shell

        # 按批 8 样本喂 ragas.evaluate（architect-design §4.3 第三层粒度）
        for start in range(0, len(samples), self._batch_size):
            if should_cancel is not None and should_cancel():
                for s in samples[start:]:
                    shells[s.idx].error = "任务已取消"
                break
            batch = samples[start : start + self._batch_size]
            # 批内按「有无 reference」再分组：两组指标集合不同（降级表）
            with_ref = [s for s in batch if s.reference.strip()]
            without_ref = [s for s in batch if not s.reference.strip()]
            for group, has_ref in ((with_ref, True), (without_ref, False)):
                if not group:
                    continue
                await self._evaluate_group(group, requested_metrics, has_ref, shells)

        return [shells[s.idx] for s in samples]

    async def _evaluate_group(
        self,
        group: list[RunnerSample],
        requested_metrics: list[str],
        has_ref: bool,
        shells: dict[int, SampleScore],
    ) -> None:
        """按批评测一组同构样本；批整体失败降级为逐样本重试一次，再失败记样本错误。"""
        metric_names = metrics_for_sample(requested_metrics, has_ref)
        if not metric_names:
            return
        try:
            rows = await self._evaluate(group, metric_names, has_ref)
        except Exception:
            # 批内失败降级：逐样本重试一次（architect-design §4.1）
            for sample in group:
                await self._evaluate_single(sample, metric_names, has_ref, shells)
            return
        # 批成功：按位置回填（ragas to_pandas 与输入样本顺序一致）
        for sample, row in zip(group, rows, strict=False):
            self._fill_scores(shells[sample.idx], row, metric_names)

    async def _evaluate_single(
        self,
        sample: RunnerSample,
        metric_names: list[str],
        has_ref: bool,
        shells: dict[int, SampleScore],
    ) -> None:
        """逐样本重试路径：再失败记样本错误（不阻断整体）。"""
        try:
            rows = await self._evaluate([sample], metric_names, has_ref)
            row = rows[0] if rows else {}
            self._fill_scores(shells[sample.idx], row, metric_names)
        except Exception as exc:  # noqa: BLE001 —— 样本级容错，任何异常都不上抛
            shells[sample.idx].error = f"评测失败（批失败后逐样本重试仍失败）: {exc}"

    @staticmethod
    def _fill_scores(shell: SampleScore, row: dict[str, Any], metric_names: list[str]) -> None:
        """把 evaluate 返回的一行分数写入结果壳（NaN 统一转 None，均值计算剔除）。"""
        reasons = row.get("_reasons") or {}
        for name in metric_names:
            score = row.get(name)
            shell.scores[name] = {
                "score": None if score is None or _is_nan(score) else float(score),
                "reason": str(reasons.get(name, "")),
            }


def _is_nan(value: Any) -> bool:
    """判断浮点 NaN（ragas 解析失败记 NaN 的口径）。"""
    return isinstance(value, float) and math.isnan(value)


def summarize(samples_scores: list[SampleScore], requested_metrics: list[str]) -> dict[str, Any]:
    """汇总四指标 mean/coverage/valid_samples（任务完成时调用，落 reports 表）。

    - coverage = 有效样本数 / 总样本数（score 非 None 为有效）
    - context_precision 若存在无参考降级样本，标注 degraded=true
    - context_recall 缺 reference 样本记 N/A 并在 note 中说明
    - parse_failure_rate：score 为 None 且非「缺 reference N/A」的比例
      （> 5% 视为提示词/模型故障，结论标记 unreliable，prompt-design §5.4）
    """
    total = len(samples_scores)
    summary: dict[str, Any] = {}
    # 统计降级与 N/A 信息
    na_recall = 0
    degraded_precision = False
    parse_failures = 0
    judged_cells = 0

    for name in ALL_METRICS:
        if name not in requested_metrics:
            continue  # 未请求的指标不出现在 summary
        values: list[float] = []
        for ss in samples_scores:
            if ss.error:
                continue  # 样本级失败不计入任何指标
            cell = ss.scores.get(name)
            if cell is None:
                continue
            judged_cells += 1
            score = cell.get("score")
            if score is None:
                if name == "context_recall" and "缺标准答案" in str(cell.get("reason", "")):
                    na_recall += 1
                else:
                    parse_failures += 1  # 解析失败记 NaN 的样本
                continue
            values.append(float(score))
        entry: dict[str, Any] = {
            "mean": round(sum(values) / len(values), 4) if values else None,
            "coverage": round(len(values) / total, 4) if total else 0.0,
            "valid_samples": len(values),
        }
        if name == "context_precision":
            # 有样本缺 reference 且请求了 precision → 部分样本走了无参考变体
            degraded_precision = any(
                not ss.error and ss.scores.get("context_recall", {}).get("score") is None
                and "缺标准答案" in str(ss.scores.get("context_recall", {}).get("reason", ""))
                for ss in samples_scores
            ) and "context_recall" in requested_metrics
            entry["degraded"] = degraded_precision
        if name == "context_recall" and na_recall:
            entry["note"] = f"{na_recall} 条样本缺 reference，记 N/A"
        summary[name] = entry

    # 解析失败率单独上报；> 5% 阻塞评测结论（标记不可靠）
    rate = round(parse_failures / judged_cells, 4) if judged_cells else 0.0
    summary["parse_failure_rate"] = rate
    summary["reliable"] = rate <= 0.05
    summary["prompt_version"] = PROMPT_VERSION
    summary["total_samples"] = total
    summary["error_samples"] = sum(1 for ss in samples_scores if ss.error)
    return summary
