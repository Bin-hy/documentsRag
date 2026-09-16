"""T10 验证：中间件与统一响应——无令牌 401、异常包装格式、request_id 回显。"""

from __future__ import annotations

from httpx import ASGITransport, AsyncClient

from ragas_eval.api.middleware import AppError
from ragas_eval.main import create_app
from tests.conftest import TEST_TOKEN


async def test_missing_token_401(settings, app_state):
    """缺少 X-Eval-Internal-Token：401 + {code,message} 包装。"""
    app = create_app(settings)
    app.state.app_state = app_state
    async with AsyncClient(transport=ASGITransport(app=app, raise_app_exceptions=False), base_url="http://t") as c:
        resp = await c.get("/api/v1/eval/datasets")
        assert resp.status_code == 401
        body = resp.json()
        assert body["code"] == 401
        assert "内部令牌" in body["message"]
        assert body["data"] is None
        # request_id 回显
        assert resp.headers.get("x-request-id")


async def test_wrong_token_401(settings, app_state):
    """错误令牌同样 401。"""
    app = create_app(settings)
    app.state.app_state = app_state
    async with AsyncClient(
        transport=ASGITransport(app=app, raise_app_exceptions=False),
        base_url="http://t",
        headers={"X-Eval-Internal-Token": "wrong"},
    ) as c:
        resp = await c.get("/api/v1/eval/datasets")
        assert resp.status_code == 401


async def test_health_exempt_from_token(settings, app_state):
    """health 与 /healthz 豁免令牌校验。"""
    app = create_app(settings)
    app_state.binrag_client = _PingStub(True)
    app_state.judge_client = _PingStub(True)
    app.state.app_state = app_state
    async with AsyncClient(transport=ASGITransport(app=app, raise_app_exceptions=False), base_url="http://t") as c:
        resp = await c.get("/api/v1/eval/health")
        assert resp.status_code == 200
        body = resp.json()
        assert body["code"] == 0
        assert body["data"]["status"] == "ok"
        assert body["data"]["checks"]["db"] == "ok"
        assert body["data"]["checks"]["binrag_api"] == "ok"
        assert body["data"]["checks"]["judge_llm"] == "ok"
        assert "queue" in body["data"]["checks"]

        resp = await c.get("/healthz")
        assert resp.status_code == 200
        assert resp.json()["status"] == "ok"


async def test_health_degraded_when_dependency_down(settings, app_state):
    """依赖不可用时 status=degraded（HTTP 仍 200）。"""
    app = create_app(settings)
    app_state.binrag_client = _PingStub(False)
    app_state.judge_client = _PingStub(True)
    app.state.app_state = app_state
    async with AsyncClient(transport=ASGITransport(app=app, raise_app_exceptions=False), base_url="http://t") as c:
        resp = await c.get("/api/v1/eval/health")
        assert resp.status_code == 200
        body = resp.json()
        assert body["data"]["status"] == "degraded"
        assert body["data"]["checks"]["binrag_api"] == "error"


async def test_unhandled_exception_wrapped(settings, app_state):
    """未捕获异常 → 500 {code,message} 统一包装。"""
    app = create_app(settings)
    app.state.app_state = app_state

    @app.get("/api/v1/eval/boom")
    async def boom():  # type: ignore[no-untyped-def]
        raise RuntimeError("炸了")

    async with AsyncClient(
        transport=ASGITransport(app=app, raise_app_exceptions=False),
        base_url="http://t",
        headers={"X-Eval-Internal-Token": TEST_TOKEN},
    ) as c:
        resp = await c.get("/api/v1/eval/boom")
        assert resp.status_code == 500
        body = resp.json()
        assert body["code"] == 500
        assert "炸了" in body["message"]
        assert resp.headers.get("x-request-id")


async def test_app_error_status_consistency(settings, app_state):
    """AppError：code 与 HTTP 状态一致（如 404）。"""
    app = create_app(settings)
    app.state.app_state = app_state

    @app.get("/api/v1/eval/notfound")
    async def nf():  # type: ignore[no-untyped-def]
        raise AppError(404, "不存在")

    async with AsyncClient(
        transport=ASGITransport(app=app, raise_app_exceptions=False),
        base_url="http://t",
        headers={"X-Eval-Internal-Token": TEST_TOKEN},
    ) as c:
        resp = await c.get("/api/v1/eval/notfound")
        assert resp.status_code == 404
        assert resp.json() == {"code": 404, "message": "不存在", "data": None}


class _PingStub:
    """探活 stub：替代 BinRagClient/JudgeLLMClient 的 ping。"""

    def __init__(self, value: bool) -> None:
        self._value = value

    async def ping(self) -> bool:
        return self._value
