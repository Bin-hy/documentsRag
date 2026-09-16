"""API 中间件（T10）：内部令牌校验、request_id、统一异常包装。

契约（architect-design §2.1/§2.4）：
- 成功 {"code":0,"message":"ok","data":...}；失败 code 与 HTTP 状态一致
- 缺少/错误的 X-Eval-Internal-Token 一律 401（health 与 /healthz 豁免）
- 所有响应回显 X-Request-ID
"""

from __future__ import annotations

import uuid
from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from starlette.middleware.base import BaseHTTPMiddleware

# 令牌豁免路径（health 供 compose/Go 探活；/healthz 为裸探活端点）
_EXEMPT_PATHS = frozenset({"/api/v1/eval/health", "/healthz"})

INTERNAL_TOKEN_HEADER = "x-eval-internal-token"


class AppError(Exception):
    """业务异常：code 即 HTTP 状态语义码（错误码表 architect-design §2.4）。"""

    def __init__(self, code: int, message: str, data: Any = None) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.data = data


def ok(data: Any = None, status_code: int = 200) -> JSONResponse:
    """成功响应统一包装：{"code":0,"message":"ok","data":...}。"""
    return JSONResponse(
        status_code=status_code,
        content={"code": 0, "message": "ok", "data": data},
    )


def error_body(code: int, message: str) -> dict[str, Any]:
    """失败响应体：{"code":code,"message":...,"data":null}。"""
    return {"code": code, "message": message, "data": None}


class InternalTokenMiddleware(BaseHTTPMiddleware):
    """服务面鉴权：校验 Go 代理注入的 X-Eval-Internal-Token（共享密钥）。

    配合网络层隔离（compose 内网不发布端口）构成纵深防御。
    """

    def __init__(self, app: Any, internal_token: str) -> None:
        super().__init__(app)
        self._token = internal_token

    async def dispatch(self, request: Request, call_next):  # type: ignore[no-untyped-def]
        # request_id 生成与回显（任务全生命周期日志关联用）
        request_id = request.headers.get("x-request-id") or str(uuid.uuid4())
        request.state.request_id = request_id

        path = request.url.path
        if path not in _EXEMPT_PATHS:
            token = request.headers.get(INTERNAL_TOKEN_HEADER, "")
            # 缺失/错误一律 401；配置为空令牌时同样拒绝（不容忍裸奔）
            if not self._token or token != self._token:
                return JSONResponse(
                    status_code=401,
                    content=error_body(401, "缺少或无效的内部令牌"),
                    headers={"x-request-id": request_id},
                )

        response = await call_next(request)
        response.headers["x-request-id"] = request_id
        return response


def register_exception_handlers(app: FastAPI) -> None:
    """统一异常包装：任何异常 → {code,message}（日志侧携带 task_id/request_id）。"""

    @app.exception_handler(AppError)
    async def _app_error_handler(request: Request, exc: AppError) -> JSONResponse:
        return JSONResponse(
            status_code=exc.code,
            content={"code": exc.code, "message": exc.message, "data": exc.data},
        )

    @app.exception_handler(Exception)
    async def _unhandled_handler(request: Request, exc: Exception) -> JSONResponse:
        # 未捕获内部错误：500，日志带 request_id
        request_id = getattr(request.state, "request_id", "-")
        import logging

        logging.getLogger("ragas_eval").exception(
            "未捕获异常 request_id=%s path=%s", request_id, request.url.path
        )
        return JSONResponse(
            status_code=500,
            content=error_body(500, f"内部错误: {exc}"),
            # ServerErrorMiddleware 在最外层捕获异常，响应不经过业务中间件，
            # 故此处直接回显 X-Request-ID
            headers={"x-request-id": request_id},
        )
