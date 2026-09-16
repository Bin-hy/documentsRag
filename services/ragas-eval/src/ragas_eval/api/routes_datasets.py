"""数据集路由（T11）：列表 / 上传 / 预览 / 删除。

契约（architect-design §2.3-2/3/4/5）：
- 上传：multipart（file ≤10MB + 可选 name），EvalSample 校验对齐 Go
  dataset.go Validate，422 带行号；响应 DatasetView + validation.warnings
- 删除：被任务引用时 409
"""

from __future__ import annotations

from fastapi import APIRouter, Depends, Form, Query, UploadFile
from fastapi import File as FileParam

from ragas_eval.api.deps import get_app_state
from ragas_eval.api.middleware import AppError, ok
from ragas_eval.api.schemas import dataset_view
from ragas_eval.core.dataset import (
    DatasetValidationError,
    parse_dataset,
    samples_from_raw_blob,
)

router = APIRouter(tags=["datasets"])


@router.get("/datasets")
async def list_datasets(state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """数据集列表（DatasetView 数组）。"""
    items = await state.repo.list_datasets()
    return ok({"items": [dataset_view(ds) for ds in items], "total": len(items)})


@router.post("/datasets", status_code=201)
async def upload_dataset(
    file: UploadFile = FileParam(...),
    name: str | None = Form(default=None),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """上传数据集：multipart，≤10MB，格式非法 422（message 含首条错误行号）。"""
    content = await file.read()
    if len(content) > state.settings.max_dataset_bytes:
        raise AppError(400, f"数据集文件超过大小上限（{state.settings.max_dataset_bytes} 字节）")
    filename = file.filename or "dataset.json"
    try:
        parsed = parse_dataset(content, filename)
    except DatasetValidationError as exc:
        # 422 格式非法，message 带行号/字段（对齐 Go 报错风格）
        raise AppError(422, str(exc)) from exc
    ds = await state.repo.create_dataset(
        name=name or parsed.name,
        source_format=parsed.source_format,
        content_hash=parsed.content_hash,
        sample_count=len(parsed.samples),
        with_reference_count=parsed.with_reference_count,
        raw_blob=content.decode("utf-8"),
    )
    return ok(dataset_view(ds, warnings=parsed.warnings), status_code=201)


@router.get("/datasets/{dataset_id}/preview")
async def preview_dataset(
    dataset_id: str,
    limit: int = Query(default=5, ge=1, le=100),
    state=Depends(get_app_state),  # type: ignore[no-untyped-def]
):
    """数据集预览 + 字段统计（契约 §2.3-4）。"""
    ds = await state.repo.get_dataset(dataset_id)
    if ds is None:
        raise AppError(404, "数据集不存在")
    samples = samples_from_raw_blob(ds.raw_blob, ds.source_format)
    with_reference = sum(1 for s in samples if s.answer.strip())
    with_expected_ids = sum(1 for s in samples if s.expected_ids)
    with_kb_id = sum(1 for s in samples if s.kb_id.strip())
    return ok(
        {
            "dataset": dataset_view(ds),
            "field_stats": {
                "with_reference": with_reference,
                "with_expected_ids": with_expected_ids,
                "with_kb_id": with_kb_id,
            },
            "samples": [
                {
                    "question": s.question,
                    "answer": s.answer,
                    "expected_ids": s.expected_ids,
                    "kb_id": s.kb_id,
                }
                for s in samples[:limit]
            ],
        }
    )


@router.delete("/datasets/{dataset_id}")
async def delete_dataset(dataset_id: str, state=Depends(get_app_state)):  # type: ignore[no-untyped-def]
    """删除数据集；被任务引用时 409（状态冲突）。"""
    ds = await state.repo.get_dataset(dataset_id)
    if ds is None:
        raise AppError(404, "数据集不存在")
    if await state.repo.dataset_in_use(dataset_id):
        raise AppError(409, "数据集已被评测任务引用，不可删除")
    await state.repo.delete_dataset(dataset_id)
    return ok({"id": dataset_id, "deleted": True})
