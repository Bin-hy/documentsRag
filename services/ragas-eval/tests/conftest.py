"""pytest 公共设施：内存库 Settings、AppState 装配、ASGI 测试客户端。

- 数据库一律 :memory:（Database 单连接，隔离由 fixture 作用域保证）
- API 测试不触发 lifespan：手动 build_app_state 并挂 app.state
- 外部依赖（Go API / Judge LLM）由 respx mock 或 double 替换
"""

from __future__ import annotations

import pytest
from httpx import ASGITransport, AsyncClient

from ragas_eval.api.deps import AppState, preflight_dependencies
from ragas_eval.config import Settings
from ragas_eval.core.collector import CollectedSample
from ragas_eval.main import create_app
from ragas_eval.store.db import Database
from ragas_eval.store.repo import Repo

# 测试用内部令牌（中间件校验用）
TEST_TOKEN = "test-internal-token"

# 测试数据集（JSON 格式，3 条样本：2 条带 reference，1 条缺 reference）
VALID_DATASET_JSON = """{
  "name": "测试评测集",
  "samples": [
    {"question": "BinRag 用什么语言实现？", "answer": "Go 语言。", "expected_ids": ["c1"], "kb_id": "kb-1"},
    {"question": "向量存储用什么？", "answer": "Qdrant。", "expected_ids": ["c2"]},
    {"question": "支持哪些文档格式？", "expected_ids": []}
  ]
}""".encode()

# 非法数据集：第 2 条样本 question 为空
INVALID_DATASET_JSON = """{
  "name": "坏数据集",
  "samples": [
    {"question": "正常问题", "expected_ids": []},
    {"question": "  ", "expected_ids": []}
  ]
}""".encode()

# JSONL 数据集（第 2 行非法 JSON）
INVALID_JSONL = b'{"question": "q1", "expected_ids": []}\n{bad json}\n'


@pytest.fixture
def settings(tmp_path) -> Settings:  # type: ignore[no-untyped-def]
    """测试配置：必填项注入假值，db 用内存库。"""
    return Settings(
        listen_port=8090,
        binrag_base_url="http://binrag-test",
        binrag_api_key="test-api-key",
        internal_token=TEST_TOKEN,
        judge_base_url="http://judge-test/v1",
        judge_api_key="test-judge-key",
        judge_model="judge-model-a",
        embed_base_url="http://embed-test/v1",
        embed_api_key="test-embed-key",
        embed_model="embed-model-a",
        db_path=":memory:",
        max_running_tasks=2,
        max_queue_size=4,
        judge_rpm=6000,  # 测试不限速
        task_deadline=3600,
    )


@pytest.fixture
async def repo(settings: Settings) -> Repo:  # type: ignore[misc]
    """内存库仓储（已完成迁移）。"""
    db = Database(":memory:")
    await db.open()
    yield Repo(db)
    await db.close()


class FakeCollector:
    """采集器 double：按 question 返回固定结果，记录调用次数。"""

    def __init__(self, fail_on: set[str] | None = None) -> None:
        self.calls: list[str] = []
        self.fail_on = fail_on or set()

    async def collect_one(self, question, kb_id="", strategy=None):  # type: ignore[no-untyped-def]
        from ragas_eval.core.collector import CollectError

        self.calls.append(question)
        if question in self.fail_on:
            raise CollectError(f"模拟采集失败: {question}")
        return CollectedSample(
            answer=f"回答：{question}",
            contexts=[{"id": "c1", "filename": "f.md", "heading": "", "score": 0.9,
                       "content": f"上下文：{question}"}],
        )

    async def collect_many(self, items, strategy=None):  # type: ignore[no-untyped-def]
        """与真实 SampleCollector.collect_many 同语义：并发采集、异常占位。"""
        import asyncio

        from ragas_eval.core.collector import CollectError

        async def _one(item):  # type: ignore[no-untyped-def]
            try:
                return await self.collect_one(
                    item["question"], kb_id=item.get("kb_id", ""), strategy=strategy
                )
            except CollectError as exc:
                return exc

        return list(await asyncio.gather(*(_one(i) for i in items)))


class FakeRunner:
    """执行器 double：全部指标打 0.9，记录收到的样本。"""

    def __init__(self) -> None:
        from ragas_eval.core.runner import RagasRunner

        # 复用真实 RagasRunner，但注入确定性 evaluate_fn
        async def _eval(samples, metrics, has_ref):  # type: ignore[no-untyped-def]
            return [
                {**{metric: 0.9 for metric in metrics},
                 "_reasons": {metric: "模拟理由" for metric in metrics}}
                for _ in samples
            ]

        self._inner = RagasRunner.__new__(RagasRunner)  # 不经 __init__，只借方法
        self._inner._batch_size = 8
        self._inner._evaluate = _eval

    async def evaluate_samples(self, samples, metrics, should_cancel=None):  # type: ignore[no-untyped-def]
        return await self._inner.evaluate_samples(samples, metrics, should_cancel)


def make_queue(settings: Settings, repo: Repo, collector=None, runner=None):  # type: ignore[no-untyped-def]
    """装配使用 double 的任务队列。"""
    from ragas_eval.core.queue import TaskQueue

    fake_collector = collector or FakeCollector()
    fake_runner = runner or FakeRunner()
    queue = TaskQueue(
        settings,
        repo,
        collector_factory=lambda concurrency: fake_collector,
        runner_factory=lambda: fake_runner,
    )
    return queue, fake_collector, fake_runner


@pytest.fixture
async def app_state(settings: Settings):  # type: ignore[misc]
    """API 测试用应用状态：内存库 + double 队列，不启动 worker。"""
    db = Database(":memory:")
    await db.open()
    repo = Repo(db)
    queue, _, _ = make_queue(settings, repo)
    state = AppState(
        settings=settings,
        database=db,
        repo=repo,
        queue=queue,
        binrag_client=None,  # type: ignore[arg-type]  # 测试用 respx/stub 替换
        judge_client=None,  # type: ignore[arg-type]
    )
    yield state
    await db.close()


@pytest.fixture
async def client(settings: Settings, app_state: AppState):  # type: ignore[misc]
    """ASGI 测试客户端：跳过 lifespan，预检依赖 override 为 no-op。"""
    app = create_app(settings)
    app.state.app_state = app_state
    # 提交任务前的依赖预检（Go/Judge 连通性）在单测中跳过
    app.dependency_overrides[preflight_dependencies] = lambda: None
    # fetch_config_hash 走 binrag_client：替换为 stub
    from ragas_eval.core.collector import BinRagClient

    binrag = BinRagClient.__new__(BinRagClient)
    binrag._settings = settings
    binrag._client = None  # ping/fetch_config_hash 均被 stub 覆盖
    binrag.ping = lambda: _true()  # type: ignore[attr-defined]
    binrag.fetch_config_hash = lambda: _hash()  # type: ignore[attr-defined]
    app_state.binrag_client = binrag

    class _JudgeStub:
        async def ping(self) -> bool:
            return True

    app_state.judge_client = _JudgeStub()  # type: ignore[assignment]

    transport = ASGITransport(app=app, raise_app_exceptions=False)
    async with AsyncClient(
        transport=transport,
        base_url="http://testserver",
        headers={"X-Eval-Internal-Token": TEST_TOKEN},
    ) as c:
        c.app = app  # type: ignore[attr-defined]  # 测试内取用 dependency_overrides
        yield c


async def _true() -> bool:
    return True


async def _hash() -> str:
    return "sha256:testconfig"


async def seed_dataset(repo: Repo, content: bytes = VALID_DATASET_JSON, filename: str = "ds.json"):
    """测试辅助：解析并落库一个数据集，返回 (Dataset, ParsedDataset)。"""
    from ragas_eval.core.dataset import parse_dataset

    parsed = parse_dataset(content, filename)
    ds = await repo.create_dataset(
        name=parsed.name,
        source_format=parsed.source_format,
        content_hash=parsed.content_hash,
        sample_count=len(parsed.samples),
        with_reference_count=parsed.with_reference_count,
        raw_blob=content.decode("utf-8"),
    )
    return ds, parsed
