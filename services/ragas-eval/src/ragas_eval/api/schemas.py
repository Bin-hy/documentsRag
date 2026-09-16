"""API 视图序列化与请求模型：snake_case 字段与契约（architect-design §2.3）对齐。"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field

from ragas_eval.config import ALL_METRICS
from ragas_eval.store import models as m


def dataset_view(ds: m.Dataset, warnings: list[str] | None = None) -> dict[str, Any]:
    """DatasetView：数据集列表/上传响应。"""
    view: dict[str, Any] = {
        "id": ds.id,
        "name": ds.name,
        "sample_count": ds.sample_count,
        "with_reference_count": ds.with_reference_count,
        "source_format": ds.source_format,
        "content_hash": ds.content_hash,
        "created_at": ds.created_at,
    }
    if warnings is not None:
        # 上传响应附带校验警告（如缺 reference 降级提示）
        view["validation"] = {"warnings": warnings}
    return view


def task_view(task: m.Task) -> dict[str, Any]:
    """TaskView：任务骨架（提交/取消响应用，含进度）。"""
    return {
        "id": task.id,
        "name": task.name,
        "status": task.status,
        "stage": task.status,  # 展示用当前阶段（与 status 同义，预留子阶段）
        "dataset_id": task.dataset_id,
        "kb_id": task.kb_id,
        "judge_model": task.judge_model,
        "metrics": task.metrics,
        "progress": {
            "total": task.progress.total,
            "collected": task.progress.collected,
            "evaluated": task.progress.evaluated,
            "failed": task.progress.failed,
        },
        "error_message": task.error_message,
        "created_at": task.created_at,
        "started_at": task.started_at,
        "finished_at": task.finished_at,
        "report_ready": task.status == m.STATUS_COMPLETED,
    }


# 任务详情与 TaskView 同构（契约 §2.3-9：轮询主接口，轻量不含样本明细）
task_detail_view = task_view


class CreateTaskRequest(BaseModel):
    """提交评测任务请求体（契约 §2.3-7）。"""

    name: str | None = None  # 可选，默认自动生成
    dataset_id: str  # 必填
    kb_id: str = ""  # 可选；空 = 逐样本用自带 kb_id / 不限定
    judge_model: str | None = None  # 可选，默认服务端 default
    metrics: list[str] = Field(default_factory=lambda: list(ALL_METRICS))  # 默认全量
    sample_concurrency: int = 4  # 样本级并发上限（默认 4，上限 8）
    strategy: str | None = None  # 透传 Go chat 的 strategy 覆盖


def sample_list_item(sample: m.Sample) -> dict[str, Any]:
    """报告明细行（不含 contexts 正文，下钻接口才给）。"""
    return {
        "sample_id": sample.id,
        "question": sample.question,
        # 摘要截断，明细列表不承载长文本
        "answer_excerpt": sample.answer[:100],
        "scores": {name: cell.get("score") for name, cell in sample.scores.items()},
        "status": sample.status,
        "error": sample.error,
    }


def sample_detail_item(sample: m.Sample) -> dict[str, Any]:
    """单样本下钻（含 contexts 正文与各指标判定理由）。"""
    return {
        "sample_id": sample.id,
        "question": sample.question,
        "reference": sample.reference,
        "answer": sample.answer,
        "contexts": sample.contexts,  # [{id,filename,heading,score,content}]
        "scores": sample.scores,  # {metric: {score, reason}} 含判定理由
        "status": sample.status,
        "error": sample.error,
    }
