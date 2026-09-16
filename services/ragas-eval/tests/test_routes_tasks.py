"""T11 验证：任务路由——提交/幂等/列表/详情/取消 409/删除/预检 503/评审模型清单。"""

from __future__ import annotations

from ragas_eval.store import models as m
from tests.conftest import VALID_DATASET_JSON


async def _upload(client) -> str:  # type: ignore[no-untyped-def]
    resp = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    assert resp.status_code == 201
    return resp.json()["data"]["id"]


async def test_create_task_201(client):
    """提交任务：201 + TaskView pending，进度 total 就位，config_snapshot 三要素。"""
    ds_id = await _upload(client)
    resp = await client.post(
        "/api/v1/eval/tasks",
        json={"dataset_id": ds_id, "kb_id": "kb-1", "name": "基线评测"},
    )
    assert resp.status_code == 201
    data = resp.json()["data"]
    assert data["status"] == "pending"
    assert data["name"] == "基线评测"
    assert data["progress"] == {"total": 3, "collected": 0, "evaluated": 0, "failed": 0}
    assert data["metrics"] == [
        "faithfulness", "answer_relevancy", "context_precision", "context_recall",
    ]


async def test_create_task_unknown_metric_400(client):
    """metrics 含未知指标名 → 400。"""
    ds_id = await _upload(client)
    resp = await client.post(
        "/api/v1/eval/tasks", json={"dataset_id": ds_id, "metrics": ["faithfulness", "xxx"]}
    )
    assert resp.status_code == 400
    assert "未知指标" in resp.json()["message"]


async def test_create_task_dataset_not_found_404(client):
    resp = await client.post("/api/v1/eval/tasks", json={"dataset_id": "not-exist"})
    assert resp.status_code == 404


async def test_idempotency_key_replay(client):
    """幂等重提交返回同任务（HTTP 200 而非 201）。"""
    ds_id = await _upload(client)
    headers = {"Idempotency-Key": "retry-key-1"}
    first = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id}, headers=headers)
    assert first.status_code == 201
    second = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id}, headers=headers)
    assert second.status_code == 200
    assert second.json()["data"]["id"] == first.json()["data"]["id"]


async def test_task_list_and_detail(client):
    """任务列表过滤/分页 + 详情轮询字段。"""
    ds_id = await _upload(client)
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    task_id = created.json()["data"]["id"]

    resp = await client.get("/api/v1/eval/tasks", params={"status": "pending"})
    assert resp.status_code == 200
    data = resp.json()["data"]
    assert data["total"] == 1
    assert data["items"][0]["id"] == task_id

    resp = await client.get(f"/api/v1/eval/tasks/{task_id}")
    assert resp.status_code == 200
    detail = resp.json()["data"]
    assert detail["status"] == "pending"
    assert detail["stage"] == "pending"
    assert detail["report_ready"] is False
    assert detail["error_message"] == ""

    # 非法状态过滤 400
    resp = await client.get("/api/v1/eval/tasks", params={"status": "bogus"})
    assert resp.status_code == 400


async def test_cancel_pending_task(client):
    """取消 pending 任务：status=canceled + finished_at 落时间。"""
    ds_id = await _upload(client)
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    task_id = created.json()["data"]["id"]

    resp = await client.post(f"/api/v1/eval/tasks/{task_id}/cancel")
    assert resp.status_code == 200
    data = resp.json()["data"]
    assert data["status"] == "canceled"
    assert data["finished_at"] is not None


async def test_cancel_terminal_task_409(client):
    """取消已完成任务 → 409（状态冲突）。"""
    ds_id = await _upload(client)
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    task_id = created.json()["data"]["id"]
    # 直接把任务落为 completed（绕过 worker，测 API 状态机约束）
    state = client.app.state.app_state  # type: ignore[attr-defined]
    await state.repo.update_task_status(task_id, m.STATUS_COLLECTING, set_started=True)
    await state.repo.update_task_status(task_id, m.STATUS_EVALUATING)
    await state.repo.update_task_status(task_id, m.STATUS_COMPLETED, set_finished=True)

    resp = await client.post(f"/api/v1/eval/tasks/{task_id}/cancel")
    assert resp.status_code == 409
    assert "不可取消" in resp.json()["message"]


async def test_delete_task_running_409_then_ok(client):
    """运行中任务删除 409；终态可删且级联清理。"""
    ds_id = await _upload(client)
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    task_id = created.json()["data"]["id"]

    resp = await client.delete(f"/api/v1/eval/tasks/{task_id}")
    assert resp.status_code == 409  # pending 属运行态，需先取消

    await client.post(f"/api/v1/eval/tasks/{task_id}/cancel")
    resp = await client.delete(f"/api/v1/eval/tasks/{task_id}")
    assert resp.status_code == 200
    resp = await client.get(f"/api/v1/eval/tasks/{task_id}")
    assert resp.status_code == 404


async def test_preflight_failure_503(settings, app_state):
    """提交前预检依赖失败 → 503（BinRag API 探活失败）。"""
    from ragas_eval.main import create_app

    app = create_app(settings)

    class DownClient:
        async def ping(self) -> bool:
            return False

    app_state.binrag_client = DownClient()  # type: ignore[assignment]
    app.state.app_state = app_state

    from httpx import ASGITransport, AsyncClient

    from tests.conftest import TEST_TOKEN

    async with AsyncClient(
        transport=ASGITransport(app=app, raise_app_exceptions=False),
        base_url="http://t",
        headers={"X-Eval-Internal-Token": TEST_TOKEN},
    ) as c:
        # 预检真实执行（不 override），Go API 不可达 → 503
        resp = await c.post("/api/v1/eval/tasks", json={"dataset_id": "any"})
        assert resp.status_code == 503
        assert "BinRag API 探活失败" in resp.json()["message"]


async def test_judge_models(client):
    """评审模型清单（配置驱动，含默认标记）。"""
    resp = await client.get("/api/v1/eval/judge-models")
    assert resp.status_code == 200
    items = resp.json()["data"]["items"]
    assert items[0]["id"] == "judge-model-a"
    assert items[0]["is_default"] is True
    assert items[0]["supports_structured_output"] is True
