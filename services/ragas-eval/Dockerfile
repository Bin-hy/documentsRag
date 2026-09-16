# ============================================================================
# ragas-eval 评测微服务镜像（多阶段构建）
# 构建阶段：uv 官方镜像执行 uv sync --frozen --no-dev（依赖由 uv.lock 锁定，可复现）
# 运行阶段：slim 镜像拷贝 .venv，非 root 用户运行
# ============================================================================

# ---------- 构建阶段 ----------
FROM ghcr.io/astral-sh/uv:python3.11-bookworm AS builder

WORKDIR /app

# 先拷依赖清单，利用 Docker 层缓存（源码变更不触发依赖重装）
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project

# 再拷源码并安装项目本身
COPY src ./src
RUN uv sync --frozen --no-dev

# ---------- 运行阶段 ----------
FROM python:3.11-slim-bookworm

# 非 root 运行
RUN useradd -m -u 1000 appuser

WORKDIR /app

# 从构建阶段拷贝虚拟环境
COPY --from=builder /app/.venv /app/.venv
ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    # 默认监听端口（可被 EVAL_LISTEN_PORT 覆盖）
    EVAL_LISTEN_PORT=8090 \
    # SQLite 数据目录（compose 挂载 eval_data 卷持久化）
    EVAL_DB_PATH=/data/eval.db

# 数据目录属主交给非 root 用户
RUN mkdir -p /data && chown -R appuser:appuser /data
USER appuser

EXPOSE 8090

# 启动 FastAPI 服务（仅监听内网，compose 中不发布宿主端口）
CMD ["sh", "-c", "uvicorn ragas_eval.main:app --host 0.0.0.0 --port ${EVAL_LISTEN_PORT}"]
