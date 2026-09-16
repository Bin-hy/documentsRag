"""FastAPI 装配（T12）：中间件、路由、启动建库与任务恢复、关停钩子。

端点前缀统一 /api/v1/eval（与 Go 代理路径完全一致，纯透传无需改写）。
health 豁免内部令牌校验；/healthz 为裸探活端点（compose healthcheck 用，
db 挂才 503，探活语义与 /api/v1/eval/health 的 degraded 区分）。
"""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager

from fastapi import Depends, FastAPI
from fastapi.responses import JSONResponse

from ragas_eval.api import routes_datasets, routes_reports, routes_tasks
from ragas_eval.api.deps import (
    AppState,
    build_app_state,
    close_app_state,
    get_app_state,
)
from ragas_eval.api.middleware import (
    InternalTokenMiddleware,
    error_body,
    ok,
    register_exception_handlers,
)
from ragas_eval.config import Settings, get_settings

logger = logging.getLogger("ragas_eval")

# 服务版本（health 响应与契约 §2.3-1 对齐）
SERVICE_VERSION = "0.1.0"


def create_app(settings: Settings | None = None) -> FastAPI:
    """应用工厂：注入配置（测试可传入定制 Settings）。"""

    @asynccontextmanager
    async def lifespan(app: FastAPI):  # type: ignore[no-untyped-def]
        # 启动：建库迁移 + 装配单例 + worker 启动与中断任务恢复
        state = await build_app_state(app.state.settings)
        app.state.app_state = state
        await state.queue.start()
        logger.info("ragas-eval 启动完成，队列恢复完毕")
        yield
        # 关停：停 worker、关客户端与数据库
        await close_app_state(state)

    app = FastAPI(title="ragas-eval", version=SERVICE_VERSION, lifespan=lifespan)
    app.state.settings = settings or get_settings()

    # 中间件（后注册先生效：异常包装在最外层由 exception_handler 处理）
    app.add_middleware(
        InternalTokenMiddleware, internal_token=app.state.settings.internal_token
    )
    register_exception_handlers(app)

    # 业务路由：统一 /api/v1/eval 前缀
    for r in (routes_datasets.router, routes_tasks.router, routes_reports.router):
        app.include_router(r, prefix="/api/v1/eval")

    # ---------------- 健康检查（豁免内部令牌） ----------------

    @app.get("/api/v1/eval/health")
    async def health(state: AppState = Depends(get_app_state)):  # type: ignore[no-untyped-def]
        """健康检查：status/checks（db/binrag_api/judge_llm/queue）。

        degraded 时仍 HTTP 200（依赖不可用不代表服务进程异常）；
        judge_llm 探活结果缓存 60s 防打满供应商。
        """
        checks: dict = {}
        degraded = False
        # db：SQLite 可写探测
        try:
            await state.database.conn.execute("SELECT 1")
            checks["db"] = "ok"
        except Exception:  # noqa: BLE001
            checks["db"] = "error"
            degraded = True
        # binrag_api：回调 Go 探活
        checks["binrag_api"] = "ok" if await state.binrag_client.ping() else "error"
        if checks["binrag_api"] != "ok":
            degraded = True
        # judge_llm：缓存 60s
        cached = state.judge_ping_cache.get()
        if cached is None:
            cached = await state.judge_client.ping()
            state.judge_ping_cache.set(cached)
        checks["judge_llm"] = "ok" if cached else "error"
        if not cached:
            degraded = True
        checks["queue"] = state.queue.stats()
        return ok(
            {
                "status": "degraded" if degraded else "ok",
                "version": SERVICE_VERSION,
                "checks": checks,
            }
        )

    @app.get("/healthz")
    async def healthz(state: AppState = Depends(get_app_state)):  # type: ignore[no-untyped-def]
        """裸探活端点（compose healthcheck）：仅 db 挂才 503。"""
        try:
            await state.database.conn.execute("SELECT 1")
        except Exception:  # noqa: BLE001
            return JSONResponse(status_code=503, content=error_body(503, "db 不可用"))
        return JSONResponse(status_code=200, content={"status": "ok"})

    return app


# uvicorn 入口：uv run uvicorn ragas_eval.main:app
app = create_app()
