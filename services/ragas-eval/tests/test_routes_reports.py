"""T12 验证：报告路由——分页/过滤/排序/下钻/批量对比/409，health 装配。"""

from __future__ import annotations

from ragas_eval.store import models as m
from tests.conftest import VALID_DATASET_JSON


async def _seed_completed_task(client, name: str = "已完成任务") -> str:  # type: ignore[no-untyped-def]
    """造一个 completed 任务：3 样本（2 ok 带分数 + 1 error），报告落库。"""
    up = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    ds_id = up.json()["data"]["id"]
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id, "name": name})
    task_id = created.json()["data"]["id"]

    state = client.app.state.app_state  # type: ignore[attr-defined]
    repo = state.repo
    await repo.insert_samples(
        task_id,
        [
            {"idx": 0, "question": "q0", "reference": "a0", "kb_id": "kb-1", "expected_ids": []},
            {"idx": 1, "question": "q1", "reference": "", "kb_id": "", "expected_ids": []},
            {"idx": 2, "question": "q2", "reference": "a2", "kb_id": "", "expected_ids": []},
        ],
    )
    samples = await repo.list_samples(task_id)
    # 样本 0：高分
    await repo.mark_sample_collected(
        samples[0].id, "回答0", [{"id": "c1", "filename": "a.md", "heading": "h",
                                  "score": 0.9, "content": "正文0"}]
    )
    await repo.mark_sample_scored(samples[0].id, {
        "faithfulness": {"score": 0.95, "reason": "完全一致"},
        "context_recall": {"score": 0.9, "reason": "全部要点被支持"},
    })
    # 样本 1：低分（无 reference，recall N/A）
    await repo.mark_sample_collected(samples[1].id, "回答1", [{"id": "c2", "content": "正文1"}])
    await repo.mark_sample_scored(samples[1].id, {
        "faithfulness": {"score": 0.3, "reason": "编造内容"},
        "context_recall": {"score": None, "reason": "N/A（缺标准答案）"},
    })
    # 样本 2：错误样本
    await repo.mark_sample_error(samples[2].id, "采集超时")

    await repo.update_task_status(task_id, m.STATUS_COLLECTING, set_started=True)
    await repo.update_task_status(task_id, m.STATUS_EVALUATING)
    await repo.update_task_status(task_id, m.STATUS_COMPLETED, set_finished=True)
    await repo.save_report(task_id, {
        "faithfulness": {"mean": 0.625, "coverage": 0.6667, "valid_samples": 2},
        "context_recall": {"mean": 0.9, "coverage": 0.3333, "valid_samples": 1,
                           "note": "1 条样本缺 reference，记 N/A"},
        "parse_failure_rate": 0.0, "reliable": True, "prompt_version": "zh-v2",
        "total_samples": 3, "error_samples": 1,
    })
    return task_id


async def test_report_unfinished_409(client):
    """任务未完成 → 409「任务尚未完成」。"""
    up = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    ds_id = up.json()["data"]["id"]
    created = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    task_id = created.json()["data"]["id"]
    resp = await client.get(f"/api/v1/eval/tasks/{task_id}/report")
    assert resp.status_code == 409
    assert "尚未完成" in resp.json()["message"]


async def test_report_summary_and_pagination(client):
    """报告：汇总 + 明细分页（page/page_size），明细不含 contexts 正文。"""
    task_id = await _seed_completed_task(client)
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"page": 1, "page_size": 2}
    )
    assert resp.status_code == 200
    data = resp.json()["data"]
    # 汇总与快照
    assert data["summary"]["faithfulness"]["mean"] == 0.625
    assert data["summary"]["context_recall"]["note"]
    assert data["config_snapshot"]["prompt_version"] == "zh-v2"
    assert data["task"]["report_ready"] is True
    # 分页
    samples = data["samples"]
    assert samples["total"] == 3
    assert samples["page"] == 1 and samples["page_size"] == 2
    assert len(samples["items"]) == 2
    # 明细行结构：answer_excerpt 截断、scores 扁平化、不含 contexts
    item = samples["items"][0]
    assert "sample_id" in item and "answer_excerpt" in item
    assert "contexts" not in item
    assert item["scores"]["faithfulness"] == 0.95

    # 第二页
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"page": 2, "page_size": 2}
    )
    assert len(resp.json()["data"]["samples"]["items"]) == 1


async def test_report_metric_lt_filter(client):
    """低分过滤 metric_lt=faithfulness:0.6：只留 faithfulness < 0.6 的样本。"""
    task_id = await _seed_completed_task(client)
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"metric_lt": "faithfulness:0.6"}
    )
    data = resp.json()["data"]
    assert data["samples"]["total"] == 1
    assert data["samples"]["items"][0]["scores"]["faithfulness"] == 0.3


async def test_report_sort(client):
    """排序 sort=faithfulness:asc（低分在前，N/A 沉底）。"""
    task_id = await _seed_completed_task(client)
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"sort": "faithfulness:asc"}
    )
    items = resp.json()["data"]["samples"]["items"]
    scores = [i["scores"].get("faithfulness") for i in items]
    # 0.3 在前，0.95 随后，错误样本（None）沉底
    assert scores[0] == 0.3
    assert scores[1] == 0.95


async def test_report_bad_params_400(client):
    """metric_lt/sort 格式非法 → 400。"""
    task_id = await _seed_completed_task(client)
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"metric_lt": "bad"}
    )
    assert resp.status_code == 400
    resp = await client.get(
        f"/api/v1/eval/tasks/{task_id}/report", params={"sort": "faithfulness:sideways"}
    )
    assert resp.status_code == 400


async def test_sample_drilldown(client):
    """单样本下钻：含 contexts 正文与各指标判定理由。"""
    task_id = await _seed_completed_task(client)
    resp = await client.get(f"/api/v1/eval/tasks/{task_id}/report")
    sample_id = resp.json()["data"]["samples"]["items"][0]["sample_id"]

    resp = await client.get(f"/api/v1/eval/tasks/{task_id}/report/samples/{sample_id}")
    assert resp.status_code == 200
    data = resp.json()["data"]
    assert data["answer"] == "回答0"
    assert data["contexts"][0]["content"] == "正文0"
    assert data["scores"]["faithfulness"]["reason"] == "完全一致"
    assert data["status"] == "ok"

    # 样本不存在 404
    resp = await client.get(f"/api/v1/eval/tasks/{task_id}/report/samples/not-exist")
    assert resp.status_code == 404


async def test_reports_batch_compare(client):
    """批量对比：≤8 任务汇总一次往返；任务未完成 409；超 8 个 400。"""
    t1 = await _seed_completed_task(client, "对比A")
    t2 = await _seed_completed_task(client, "对比B")
    resp = await client.get("/api/v1/eval/reports", params={"task_ids": f"{t1},{t2}"})
    assert resp.status_code == 200
    items = resp.json()["data"]["items"]
    assert len(items) == 2
    assert items[0]["summary"]["faithfulness"]["mean"] == 0.625
    assert items[0]["config_snapshot"]["prompt_version"] == "zh-v2"
    assert items[0]["name"] == "对比A"

    # 超 8 个 → 400
    resp = await client.get(
        "/api/v1/eval/reports", params={"task_ids": ",".join(["x"] * 9)}
    )
    assert resp.status_code == 400

    # 不存在任务 → 404
    resp = await client.get("/api/v1/eval/reports", params={"task_ids": "not-exist"})
    assert resp.status_code == 404


async def test_reports_batch_unfinished_409(client):
    """批量对比中含未完成任务 → 409。"""
    t1 = await _seed_completed_task(client)
    up = await client.post(
        "/api/v1/eval/datasets",
        files={"file": ("ds.json", VALID_DATASET_JSON, "application/json")},
    )
    ds_id = up.json()["data"]["id"]
    pending = await client.post("/api/v1/eval/tasks", json={"dataset_id": ds_id})
    t2 = pending.json()["data"]["id"]
    resp = await client.get("/api/v1/eval/reports", params={"task_ids": f"{t1},{t2}"})
    assert resp.status_code == 409


async def test_health_endpoint(client):
    """health：status/checks 四件套（db/binrag_api/judge_llm/queue）。"""
    resp = await client.get("/api/v1/eval/health")
    assert resp.status_code == 200
    data = resp.json()["data"]
    assert data["status"] in ("ok", "degraded")
    assert set(data["checks"].keys()) == {"db", "binrag_api", "judge_llm", "queue"}
    assert data["checks"]["db"] == "ok"
    assert "running" in data["checks"]["queue"] and "queued" in data["checks"]["queue"]


async def test_healthz(client):
    """裸 /healthz 探活。"""
    resp = await client.get("/healthz")
    assert resp.status_code == 200
