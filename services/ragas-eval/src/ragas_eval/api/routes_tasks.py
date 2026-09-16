"""任务路由（T11）：提交 / 列表 / 详情 / 取消 / 删除 + 评审模型清单。

契约要点（architect-design §2.3-6/7/8/9/10/11）：
- 提交：Idempotency-Key 24h 幂等（同 key 重复返回首个任务，HTTP 200 而非 201）；
  提交前预检依赖（Go API 与 Judge LLM 连通性，失败 503）；队列满 429；
  metrics 含未知指标 400
- 取消：仅 pending/collecting/evaluating 可取消，其余 409
- 删除：运行中任务需先取消（409）
"""

from __future__ import annotations

from datetime import UTC, datetime

from fastapi import APIRouter, Depends, Header, Query, Request

from ragas_eval.api.deps import get_app_state, preflight_dependencies
from ragas_eval.api.middleware import AppError, ok
from ragas_eval.api.schemas import CreateTaskRequest, task_detail_view, task_view
from ragas_eval.config import ALL_METRICS
from ragas_eval.core import lifecycle
from ragas_eval.core.dataset import DatasetValidationError, samples_from_raw_blob
from ragas_eval.core.queue import QueueFullError
from ragas_eval.store import models as m

router = APIRouter(tags=["tasks"])

# 幂等键有效窗口（24h，architect-design §2.1）
_IDEMPOTENCY_WINDOW_SECONDS = 24 * 3600


@router.get("/judge-models")
async def list_judge_models(state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """可用评审模型清单（服务端配置驱动，前端不硬编码）。"""
    default_model = state.settings.judge_model
    return ok(
        {
            "items": [
                {
                    "id": default_model,
                    "label": f"{default_model}（默认）",
                    "is_default": True,
                    "supports_structured_output": True,  # response_format=json_object
                }
            ]
        }
    )


@router.post("/tasks", status_code=201, dependencies=[Depends(preflight_dependencies)])
async def create_task(
    req: CreateTaskRequest,
    request: Request,
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """提交评测任务：预检依赖 → 幂等查重 → 落库 pending → 入队 → 201 TaskView。"""
    # metrics 合法性：未知指标名 400
    unknown = [x for x in req.metrics if x not in ALL_METRICS]
    if unknown:
        raise AppError(400, f"未知指标: {', '.join(unknown)}（支持 {', '.join(ALL_METRICS)}）")
    if not req.metrics:
        raise AppError(400, "metrics 不能为空")

    # 幂等键查重：24h 内同 key 返回首个任务（HTTP 200 而非 201）
    if idempotency_key:
        existing = await state.repo.find_task_by_idempotency_key(idempotency_key)
        if existing is not None:
            created = datetime.fromisoformat(existing.created_at)
            age = (datetime.now(UTC) - created).total_seconds()
            if age < _IDEMPOTENCY_WINDOW_SECONDS:
                return ok(task_view(existing), status_code=200)
            # 超窗：释放旧 key，允许同 key 再提交新任务
            await state.repo.clear_idempotency_key(existing.id)

    dataset = await state.repo.get_dataset(req.dataset_id)
    if dataset is None:
        raise AppError(404, "数据集不存在")
    try:
        samples = samples_from_raw_blob(dataset.raw_blob, dataset.source_format)
    except DatasetValidationError as exc:
        raise AppError(422, f"数据集内容损坏: {exc}") from exc

    # 配置快照：可复现与公平对比的前提（契约 §2.3-12）
    binrag_config_hash = await state.binrag_client.fetch_config_hash()
    import ragas

    from ragas_eval.core.prompts import PROMPT_VERSION

    config_snapshot = {
        "dataset_hash": dataset.content_hash,
        "judge_model": req.judge_model or state.settings.judge_model,
        "binrag_config_hash": binrag_config_hash,
        "ragas_version": getattr(ragas, "__version__", "unknown"),
        "prompt_version": PROMPT_VERSION,
    }

    name = req.name or f"评测-{dataset.name}-{datetime.now(UTC).strftime('%Y%m%d%H%M%S')}"
    task = await state.repo.create_task(
        name=name,
        dataset_id=req.dataset_id,
        kb_id=req.kb_id,
        judge_model=req.judge_model or state.settings.judge_model,
        metrics=req.metrics,
        sample_concurrency=max(1, min(req.sample_concurrency, 8)),
        strategy=req.strategy,
        total=len(samples),
        idempotency_key=idempotency_key,
        config_snapshot=config_snapshot,
    )
    try:
        await state.queue.submit(task.id)
    except QueueFullError as exc:
        # 队列满：清理刚落库的任务骨架，返回 429
        await state.repo.delete_task(task.id)
        raise AppError(429, str(exc)) from exc
    return ok(task_view(task), status_code=201)


@router.get("/tasks")
async def list_tasks(
    status: str | None = Query(default=None),
    kb_id: str | None = Query(default=None),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """任务列表（?status=&kb_id=&page=&page_size=，按创建时间倒序）。"""
    if status is not None and status not in m.TASK_STATUSES:
        raise AppError(400, f"非法状态过滤值: {status}")
    items, total = await state.repo.list_tasks(
        status=status, kb_id=kb_id, page=page, page_size=page_size
    )
    return ok(
        {
            "items": [task_view(t) for t in items],
            "total": total,
            "page": page,
            "page_size": page_size,
        }
    )


@router.get("/tasks/{task_id}")
async def get_task(task_id: str, state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """任务详情/进度（轮询主接口，纯 SQLite 读，目标 p95 < 50ms）。"""
    task = await state.repo.get_task(task_id)
    if task is None:
        raise AppError(404, "任务不存在")
    return ok(task_detail_view(task))


@router.post("/tasks/{task_id}/cancel")
async def cancel_task(task_id: str, state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """取消任务：仅运行态可取消，其余 409；样本边界协作式生效。"""
    task = await state.repo.get_task(task_id)
    if task is None:
        raise AppError(404, "任务不存在")
    if not lifecycle.is_running(task.status):
        raise AppError(409, f"任务已处于终态（{task.status}），不可取消")
    # 置取消标志（worker 样本边界生效）；pending 排队任务直接落 canceled
    state.queue.request_cancel(task_id)
    if task.status == m.STATUS_PENDING:
        lifecycle.ensure_transition(task.status, m.STATUS_CANCELED)
        await state.repo.update_task_status(task_id, m.STATUS_CANCELED, set_finished=True)
    else:
        # collecting/evaluating：由 worker 在样本边界落 canceled；
        # 为让响应语义明确，这里直接迁移（worker 边界检查到标志后停止写回）
        lifecycle.ensure_transition(task.status, m.STATUS_CANCELED)
        await state.repo.update_task_status(task_id, m.STATUS_CANCELED, set_finished=True)
    updated = await state.repo.get_task(task_id)
    return ok(task_view(updated))  # type: ignore[arg-type]


@router.delete("/tasks/{task_id}")
async def delete_task(task_id: str, state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """删除任务及报告；运行中任务需先取消（409）。"""
    task = await state.repo.get_task(task_id)
    if task is None:
        raise AppError(404, "任务不存在")
    if lifecycle.is_running(task.status):
        raise AppError(409, "任务运行中，请先取消")
    await state.repo.delete_task(task_id)
    return ok({"id": task_id, "deleted": True})
