"""报告路由（T12）：汇总+明细分页 / 单样本下钻 / 批量对比。

契约（architect-design §2.3-12/13/14）：
- GET /tasks/{id}/report：任务未完成 409；支持 page/page_size/metric_lt/sort
- GET /tasks/{id}/report/samples/{sample_id}：含 contexts 正文与判定理由
- GET /reports?task_ids=a,b,c：≤8 个任务的汇总批量查询（对比视图）
"""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Depends, Query

from ragas_eval.api.deps import get_app_state
from ragas_eval.api.middleware import AppError, ok
from ragas_eval.api.schemas import sample_detail_item, sample_list_item, task_detail_view
from ragas_eval.store import models as m

router = APIRouter(tags=["reports"])

# 批量对比任务数上限（契约 §2.3-14）
_MAX_COMPARE_TASKS = 8


def _parse_metric_lt(raw: str | None) -> tuple[str, float] | None:
    """解析低分过滤参数 metric_lt=faithfulness:0.6。"""
    if not raw:
        return None
    try:
        metric, threshold = raw.split(":", 1)
        return metric.strip(), float(threshold)
    except ValueError as exc:
        raise AppError(400, f"metric_lt 参数格式非法（期望 metric:threshold）: {raw}") from exc


def _parse_sort(raw: str | None) -> tuple[str, bool] | None:
    """解析排序参数 sort=faithfulness:asc，返回 (metric, 是否升序)。"""
    if not raw:
        return None
    try:
        metric, direction = raw.split(":", 1)
    except ValueError as exc:
        raise AppError(400, f"sort 参数格式非法（期望 metric:asc|desc）: {raw}") from exc
    if direction not in ("asc", "desc"):
        raise AppError(400, f"sort 方向非法: {direction}")
    return metric.strip(), direction == "asc"


def _filter_sort_paginate(
    samples: list[m.Sample],
    metric_lt: tuple[str, float] | None,
    sort: tuple[str, bool] | None,
    page: int,
    page_size: int,
) -> tuple[list[m.Sample], int]:
    """明细的过滤/排序/分页（样本规模千级内，Python 侧处理保持 SQL 简单）。"""
    filtered = samples
    if metric_lt is not None:
        metric, threshold = metric_lt
        # 低分过滤：该指标 score < 阈值（None/N/A 不参与过滤）
        filtered = [
            s
            for s in filtered
            if (cell := s.scores.get(metric)) is not None
            and cell.get("score") is not None
            and float(cell["score"]) < threshold
        ]
    if sort is not None:
        metric, ascending = sort

        def _key(s: m.Sample) -> float:
            cell = s.scores.get(metric) or {}
            score = cell.get("score")
            # None（N/A/解析失败）排序时沉底：升序排末尾、降序也排末尾
            return float(score) if score is not None else float("inf")

        filtered = sorted(
            filtered,
            key=_key,
            reverse=not ascending,
        )
        # None 沉底：统一把 score=None 的移到尾部
        filtered = sorted(
            filtered,
            key=lambda s: (s.scores.get(metric) or {}).get("score") is None,
        )
    total = len(filtered)
    start = (page - 1) * page_size
    return filtered[start : start + page_size], total


@router.get("/tasks/{task_id}/report")
async def get_report(
    task_id: str,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    metric_lt: str | None = Query(default=None),
    sort: str | None = Query(default=None),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """汇总报告 + 明细分页（不含 contexts 正文）；任务未完成 409。"""
    task = await state.repo.get_task(task_id)
    if task is None:
        raise AppError(404, "任务不存在")
    if task.status != m.STATUS_COMPLETED:
        raise AppError(409, "任务尚未完成")
    report = await state.repo.get_report(task_id)
    if report is None:
        raise AppError(409, "任务尚未完成")  # 报告未落库视为未就绪
    samples = await state.repo.list_samples(task_id)
    page_items, total = _filter_sort_paginate(
        samples, _parse_metric_lt(metric_lt), _parse_sort(sort), page, page_size
    )
    return ok(
        {
            "task": task_detail_view(task),
            "summary": report.summary,
            "config_snapshot": task.config_snapshot,
            "samples": {
                "items": [sample_list_item(s) for s in page_items],
                "total": total,
                "page": page,
                "page_size": page_size,
            },
        }
    )


@router.get("/tasks/{task_id}/report/samples/{sample_id}")
async def get_report_sample(
    task_id: str, sample_id: str, state=Depends(get_app_state)  # type: ignore[no-untyped-def]
):
    """单样本完整内容（下钻）：含 contexts 正文与各指标判定理由。"""
    task = await state.repo.get_task(task_id)
    if task is None:
        raise AppError(404, "任务不存在")
    if task.status != m.STATUS_COMPLETED:
        raise AppError(409, "任务尚未完成")
    sample = await state.repo.get_sample(task_id, sample_id)
    if sample is None:
        raise AppError(404, "样本不存在")
    return ok(sample_detail_item(sample))


@router.get("/reports")
async def get_reports_batch(
    task_ids: str = Query(..., description="逗号分隔的任务 id，≤8 个"),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """多任务汇总批量查询（对比视图）：summary + config_snapshot 一次往返。"""
    ids = [x.strip() for x in task_ids.split(",") if x.strip()]
    if not ids:
        raise AppError(400, "task_ids 不能为空")
    if len(ids) > _MAX_COMPARE_TASKS:
        raise AppError(400, f"批量对比最多 {_MAX_COMPARE_TASKS} 个任务")
    items: list[dict[str, Any]] = []
    for tid in ids:
        task = await state.repo.get_task(tid)
        if task is None:
            raise AppError(404, f"任务不存在: {tid}")
        if task.status != m.STATUS_COMPLETED:
            raise AppError(409, f"任务尚未完成: {tid}")
        report = await state.repo.get_report(tid)
        if report is None:
            raise AppError(409, f"任务报告未就绪: {tid}")
        items.append(
            {
                "task_id": task.id,
                "name": task.name,
                "finished_at": task.finished_at,
                "summary": report.summary,
                "config_snapshot": task.config_snapshot,
            }
        )
    return ok({"items": items})
