"""依赖注入（T10）：repo / queue / collector / judge 单例装配。

AppState 集中持有运行期单例，由 main.py 启动钩子构建、关停钩子释放；
路由通过 Depends 取用，测试可用 dependency_overrides 替换（如跳过预检）。
"""

from __future__ import annotations

from dataclasses import dataclass, field

from fastapi import Request

from ragas_eval.config import Settings
from ragas_eval.core.collector import BinRagClient
from ragas_eval.core.queue import TaskQueue
from ragas_eval.llm.factory import JudgeLLMClient, PingCache
from ragas_eval.store.db import Database
from ragas_eval.store.repo import Repo


@dataclass
class AppState:
    """应用级单例容器（挂 app.state 上，路由经 Request 取用）。"""

    settings: Settings
    database: Database
    repo: Repo
    queue: TaskQueue
    binrag_client: BinRagClient  # Go API 探活/配置快照（采集由 worker 内自建实例完成）
    judge_client: JudgeLLMClient  # 评审模型探活与统计
    judge_ping_cache: PingCache = field(default_factory=PingCache)


def get_app_state(request: Request) -> AppState:
    """从 request.app.state 取应用状态（全部路由的 DI 入口）。"""
    return request.app.state.app_state


def get_settings(request: Request) -> Settings:
    return get_app_state(request).settings


def get_repo(request: Request) -> Repo:
    return get_app_state(request).repo


def get_queue(request: Request) -> TaskQueue:
    return get_app_state(request).queue


async def preflight_dependencies(request: Request) -> None:
    """提交任务前预检依赖（T11）：Go API 与 Judge LLM 连通性，失败 503。

    独立为 Depends 依赖项：测试可 override 为 no-op，
    避免单测必须 mock 两个外部端点。
    """
    state = get_app_state(request)
    if not await state.binrag_client.ping():
        from ragas_eval.api.middleware import AppError

        raise AppError(503, "BinRag API 探活失败，评测服务依赖不可用")
    cached = state.judge_ping_cache.get()
    if cached is None:
        cached = await state.judge_client.ping()
        state.judge_ping_cache.set(cached)
    if not cached:
        from ragas_eval.api.middleware import AppError

        raise AppError(503, "Judge LLM 不可达，评测服务依赖不可用")


def build_task_queue(settings: Settings, repo: Repo) -> TaskQueue:
    """装配任务队列：注入采集器与执行器工厂（worker 内按需构造）。"""
    from ragas_eval.core.collector import SampleCollector
    from ragas_eval.core.runner import RagasRunner
    from ragas_eval.llm.factory import build_ragas_embeddings, build_ragas_llm

    def collector_factory(concurrency: int) -> SampleCollector:
        # 每个任务独立 BinRagClient（httpx.AsyncClient），避免跨任务共享连接状态
        return SampleCollector(BinRagClient(settings), concurrency=concurrency)

    def runner_factory() -> RagasRunner:
        return RagasRunner(
            settings,
            llm=build_ragas_llm(settings),
            embeddings=build_ragas_embeddings(settings),
        )

    return TaskQueue(
        settings,
        repo,
        collector_factory=collector_factory,
        runner_factory=runner_factory,
    )


async def build_app_state(settings: Settings) -> AppState:
    """构建应用状态：建库迁移 + 装配单例（main.py 启动钩子调用）。"""
    database = Database(settings.db_path)
    await database.open()
    repo = Repo(database)
    queue = build_task_queue(settings, repo)
    return AppState(
        settings=settings,
        database=database,
        repo=repo,
        queue=queue,
        binrag_client=BinRagClient(settings),
        judge_client=JudgeLLMClient(settings),
    )


async def close_app_state(state: AppState) -> None:
    """关停钩子：停 worker、关 HTTP 客户端与数据库。"""
    await state.queue.stop()
    await state.binrag_client.aclose()
    await state.judge_client.aclose()
    await state.database.close()
