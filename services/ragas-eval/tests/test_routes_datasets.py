"""T11 验证：数据集路由——合法/非法数据集上传、预览、删除引用 409。"""

from __future__ import annotations

from tests.conftest import (
    INVALID_DATASET_JSON,
    INVALID_JSONL,
    VALID_DATASET_JSON,
)


async def test_upload_valid_dataset(client):
    """合法数据集上传：201 + DatasetView + validation.warnings（缺 reference 提示）。"""
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    assert resp.status_code == 201
    body = resp.json()
    assert body["code"] == 0
    data = body["data"]
    assert data["name"] == "测试评测集"
    assert data["sample_count"] == 3
    assert data["with_reference_count"] == 2
    assert data["source_format"] == "json"
    assert data["content_hash"].startswith("sha256:")
    # 1 条样本缺 reference → 降级警告
    assert any("缺 reference" in w for w in data["validation"]["warnings"])


async def test_upload_invalid_dataset_422_with_line(client):
    """格式错误被拒绝并提示（422，message 含行号，对齐 Go Validate 文案）。"""
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("bad.json", INVALID_DATASET_JSON, "application/json")},
    )
    assert resp.status_code == 422
    body = resp.json()
    assert body["code"] == 422
    assert "第 2 条样本 question 为空" in body["message"]


async def test_upload_jsonl_parse_error_line_number(client):
    """JSONL 解析失败报行号。"""
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("bad.jsonl", INVALID_JSONL, "application/octet-stream")},
    )
    assert resp.status_code == 422
    assert "JSONL 第 2 行解析失败" in resp.json()["message"]


async def test_upload_unsupported_format_422(client):
    """不支持的扩展名 422。"""
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.csv", b"a,b", "text/csv")},
    )
    assert resp.status_code == 422
    assert "不支持的数据集格式" in resp.json()["message"]


async def test_upload_expected_ids_nil_422(client):
    """expected_ids 为 nil（缺失字段）422，对齐 Go Validate。"""
    content = b'{"name": "x", "samples": [{"question": "q"}]}'
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", content, "application/json")},
    )
    assert resp.status_code == 422
    assert "expected_ids 为 nil" in resp.json()["message"]


async def test_dataset_list_and_preview(client):
    """列表 + 预览（field_stats 与样本截断 limit）。"""
    up = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    ds_id = up.json()["data"]["id"]

    resp = await client.get("/api/v1/eval/datasets")
    assert resp.status_code == 200
    items = resp.json()["data"]["items"]
    assert len(items) == 1 and items[0]["id"] == ds_id

    resp = await client.get(f"/api/v1/eval/datasets/{ds_id}/preview", params={"limit": 2})
    assert resp.status_code == 200
    data = resp.json()["data"]
    assert data["dataset"]["id"] == ds_id
    assert data["field_stats"]["with_reference"] == 2
    assert data["field_stats"]["with_expected_ids"] == 2  # 1 条空数组
    assert data["field_stats"]["with_kb_id"] == 1
    assert len(data["samples"]) == 2  # limit 生效
    assert data["samples"][0]["question"]


async def test_preview_not_found_404(client):
    resp = await client.get("/api/v1/eval/datasets/not-exist/preview")
    assert resp.status_code == 404


async def test_delete_dataset(client):
    """删除数据集：未引用 200；被任务引用 409。"""
    up = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    ds_id = up.json()["data"]["id"]

    # 被任务引用后 409
    task_resp = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    assert task_resp.status_code == 201
    resp = await client.delete(f"/api/v1/eval/datasets/{ds_id}")
    assert resp.status_code == 409
    assert "已被评测任务引用" in resp.json()["message"]

    # 删掉任务后可删除（运行中任务需先取消再删）
    task_id = task_resp.json()["data"]["id"]
    await client.post(f"/api/v1/eval/tasks/{task_id}/cancel")
    await client.delete(f"/api/v1/eval/tasks/{task_id}")
    resp = await client.delete(f"/api/v1/eval/datasets/{ds_id}")
    assert resp.status_code == 200
    assert resp.json()["data"]["deleted"] is True
