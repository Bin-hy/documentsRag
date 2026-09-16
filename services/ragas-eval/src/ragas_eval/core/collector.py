"""数据采集器（T7）：唯一与 Go 后端通信的模块，只被 worker 调用。

逐样本回调 Go 问答接口获取「回答 + 检索上下文正文」，保证
「评测所见即生成所用」（architect-design D4：include_contexts=true）。

超时/重试策略（architect-design §4.4）：
- connect 5s / read 120s（长回答生成）
- 网络错误与 5xx 指数退避重试 2 次（1s/4s）
- 4xx 不重试，直接记样本失败
"""

from __future__ import annotations

import asyncio
import hashlib
import uuid
from dataclasses import dataclass, field
from typing import Any

import httpx

from ragas_eval.config import Settings

# 网络错误与 5xx 的退避序列（重试 2 次：1s / 4s）
_RETRY_BACKOFFS = (1.0, 4.0)

# 退避睡眠间接层（测试可 patch 避免真实等待）
_sleep = asyncio.sleep


class CollectError(RuntimeError):
    """样本采集失败（超时/重试耗尽/4xx）：记样本 error，不迁移任务状态。"""


@dataclass
class CollectedSample:
    """单样本采集结果：回答 + 上下文正文（含元数据，供报告下钻展示）。"""

    answer: str
    contexts: list[dict[str, Any]] = field(default_factory=list)  # [{id,filename,heading,score,content}]

    @property
    def context_texts(self) -> list[str]:
        """纯正文列表（RAGAS retrieved_contexts 字段用，按排名顺序）。"""
        return [c.get("content", "") for c in self.contexts]


class BinRagClient:
    """Go 后端回调客户端（httpx.AsyncClient，Bearer 评测专用 API Key）。"""

    def __init__(
        self,
        settings: Settings,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        self._settings = settings
        # 允许注入 http_client 便于 respx mock；否则按配置自建
        self._client = http_client or httpx.AsyncClient(
            base_url=settings.binrag_base_url.rstrip("/"),
            headers={"Authorization": f"Bearer {settings.binrag_api_key}"},
            # connect 5s / read 120s（长回答生成）
            timeout=httpx.Timeout(120.0, connect=5.0),
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def collect(
        self,
        question: str,
        kb_id: str = "",
        strategy: str | None = None,
    ) -> CollectedSample:
        """单样本采集：POST /api/v1/chat?include_contexts=true。

        kb_id 为空表示不限定知识库（对齐 Go resolveKBScope 语义）；
        strategy 透传 Go chat 的检索策略覆盖。
        """
        body: dict[str, Any] = {
            "question": question,
            # Go chatRequest.SessionID 为必填；评测样本需要无历史污染的独立会话，
            # 每样本生成一次性 session_id（评测联调 T22 发现的契约对齐点）
            "session_id": f"eval-{uuid.uuid4()}",
        }
        if kb_id:
            body["kb_id"] = kb_id
        if strategy:
            body["strategy"] = strategy
        last_exc: Exception | None = None
        for backoff in (0.0, *_RETRY_BACKOFFS):
            if backoff:
                await _sleep(backoff)
            try:
                resp = await self._client.post(
                    "/api/v1/chat", params={"include_contexts": "true"}, json=body
                )
            except httpx.HTTPError as exc:
                last_exc = exc  # 网络错误：退避重试
                continue
            if resp.status_code >= 500:
                last_exc = CollectError(f"Go chat HTTP {resp.status_code}")
                continue  # 服务端错误：退避重试
            if resp.status_code >= 400:
                # 4xx（如 kb 不存在/参数错误）：不重试，直接记样本失败
                raise CollectError(
                    f"Go chat HTTP {resp.status_code}: {resp.text[:200]}"
                )
            return self._parse_response(resp)
        raise CollectError(f"采集重试耗尽: {last_exc}") from last_exc

    @staticmethod
    def _parse_response(resp: httpx.Response) -> CollectedSample:
        """解析 Go 统一包装响应 {code,message,data:{answer, sources[]}}。"""
        try:
            payload = resp.json()
        except ValueError as exc:
            raise CollectError(f"Go chat 响应不是合法 JSON: {resp.text[:200]}") from exc
        if payload.get("code") != 0:
            raise CollectError(f"Go chat 业务错误: {payload.get('message', '')[:200]}")
        data = payload.get("data") or {}
        contexts = [
            {
                "id": s.get("id", ""),
                "filename": s.get("filename", ""),
                "heading": s.get("heading", ""),
                "score": s.get("score", 0.0),
                "content": s.get("content", ""),  # include_contexts=true 时填充
            }
            for s in (data.get("sources") or [])
        ]
        return CollectedSample(answer=data.get("answer", ""), contexts=contexts)

    async def ping(self) -> bool:
        """Go API 探活：GET /api/v1/knowledge-bases（带评测 Key，200/401 均算可达）。"""
        try:
            resp = await self._client.get("/api/v1/knowledge-bases")
            return resp.status_code < 500
        except httpx.HTTPError:
            return False

    async def fetch_config_hash(self) -> str:
        """抓取 Go /api/v1/config 快照哈希（config_snapshot 三要素外的可复现佐证）。

        失败不阻断任务提交，记 sha256:unavailable。
        """
        try:
            resp = await self._client.get("/api/v1/config")
            if resp.status_code >= 400:
                return "sha256:unavailable"
            digest = hashlib.sha256(resp.content).hexdigest()
            return f"sha256:{digest}"
        except httpx.HTTPError:
            return "sha256:unavailable"


class SampleCollector:
    """样本级并发采集编排：信号量限流（默认 4，上限 8）+ 样本级容错。

    单样本失败不阻断整体（spec F9 / AC6），失败样本由调用方记 error。
    """

    def __init__(self, client: BinRagClient, concurrency: int = 4) -> None:
        self._client = client
        self._sem = asyncio.Semaphore(max(1, min(concurrency, 8)))

    async def collect_one(
        self,
        question: str,
        kb_id: str = "",
        strategy: str | None = None,
    ) -> CollectedSample:
        """带信号量的单样本采集（并发上限在此生效）。"""
        async with self._sem:
            return await self._client.collect(question, kb_id=kb_id, strategy=strategy)

    async def collect_many(
        self,
        items: list[dict[str, Any]],
        strategy: str | None = None,
    ) -> list[CollectedSample | CollectError]:
        """批量并发采集：信号量限流（在飞数 ≤ concurrency），逐样本容错。

        返回与输入等长且顺序一致的结果列表；单样本失败以 CollectError 占位，
        不抛出、不中断其他样本（spec F9 / AC6）。取消检查由 worker 在
        调用本方法的批次边界完成（样本粒度取消见 queue worker）。
        """
        async def _one(item: dict[str, Any]) -> CollectedSample | CollectError:
            try:
                return await self.collect_one(
                    item["question"], kb_id=item.get("kb_id", ""), strategy=strategy
                )
            except CollectError as exc:
                return exc  # 样本级失败不抛出，由 worker 记 error

        return list(await asyncio.gather(*(_one(item) for item in items)))
