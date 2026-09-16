"""T7 验证：数据采集器——正常采集、超时重试、4xx 直接失败、并发上限生效。"""

from __future__ import annotations

import asyncio

import httpx
import pytest
import respx

from ragas_eval.core.collector import BinRagClient, CollectError, SampleCollector

CHAT_URL = "http://binrag-test/api/v1/chat"


def _chat_payload() -> dict:
    """Go chat 统一包装响应（include_contexts=true 时 sources 含 content）。"""
    return {
        "code": 0,
        "message": "ok",
        "data": {
            "answer": "BinRag 使用 Go 实现。",
            "sources": [
                {"id": "c1", "filename": "a.md", "heading": "简介", "score": 0.9,
                 "content": "BinRag 是 Go 实现的 RAG 系统。"},
                {"id": "c2", "filename": "b.md", "heading": "", "score": 0.8,
                 "content": "向量存储使用 Qdrant。"},
            ],
        },
    }


async def test_collect_success(settings):
    """正常采集：返回 answer + contexts 正文，带 include_contexts=true 与 Bearer Key。"""
    with respx.mock(assert_all_called=True) as router:
        route = router.post(CHAT_URL).mock(return_value=httpx.Response(200, json=_chat_payload()))
        client = BinRagClient(settings)
        result = await client.collect("BinRag 用什么语言？", kb_id="kb-1")
        assert result.answer == "BinRag 使用 Go 实现。"
        assert result.context_texts == [
            "BinRag 是 Go 实现的 RAG 系统。",
            "向量存储使用 Qdrant。",
        ]
        # 出口参数与鉴权头断言
        req = router.calls[0].request
        assert req.url.params["include_contexts"] == "true"
        assert req.headers["Authorization"] == "Bearer test-api-key"
        import json as _json

        assert _json.loads(req.content)["kb_id"] == "kb-1"
        assert route.called
        await client.aclose()


async def test_collect_retry_on_network_error(settings, monkeypatch):
    """网络错误退避重试 2 次后成功。"""
    import ragas_eval.core.collector as collector_mod

    async def _noop(s):  # type: ignore[no-untyped-def]
        pass

    monkeypatch.setattr(collector_mod, "_sleep", _noop)
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(
            side_effect=[
                httpx.ConnectError("连接失败"),
                httpx.Response(500, json={"error": "boom"}),
                httpx.Response(200, json=_chat_payload()),
            ]
        )
        client = BinRagClient(settings)
        result = await client.collect("q")
        assert result.answer
        assert len(router.calls) == 3  # 2 次重试后成功
        await client.aclose()


async def test_collect_retry_exhausted(settings, monkeypatch):
    """重试耗尽（持续 5xx）记样本失败。"""
    import ragas_eval.core.collector as collector_mod

    async def _noop(s):  # type: ignore[no-untyped-def]
        pass

    monkeypatch.setattr(collector_mod, "_sleep", _noop)
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(return_value=httpx.Response(502, json={"error": "bad gateway"}))
        client = BinRagClient(settings)
        with pytest.raises(CollectError, match="重试耗尽"):
            await client.collect("q")
        assert len(router.calls) == 3  # 初次 + 2 次重试
        await client.aclose()


async def test_collect_4xx_no_retry(settings):
    """4xx（如 kb 不存在）不重试，直接样本失败。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(
            return_value=httpx.Response(
                404, json={"code": 404, "message": "知识库不存在", "data": None}
            )
        )
        client = BinRagClient(settings)
        with pytest.raises(CollectError, match="404"):
            await client.collect("q", kb_id="kb-not-exist")
        assert len(router.calls) == 1
        await client.aclose()


async def test_collect_timeout_retried(settings, monkeypatch):
    """读超时（120s 长回答）视为网络错误退避重试。"""
    import ragas_eval.core.collector as collector_mod

    async def _noop(s):  # type: ignore[no-untyped-def]
        pass

    monkeypatch.setattr(collector_mod, "_sleep", _noop)
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(
            side_effect=[
                httpx.ReadTimeout("读超时"),
                httpx.Response(200, json=_chat_payload()),
            ]
        )
        client = BinRagClient(settings)
        result = await client.collect("q")
        assert result.answer
        assert len(router.calls) == 2
        await client.aclose()


async def test_concurrency_limit(settings):
    """样本级信号量并发上限生效（concurrency=2 时最多 2 个在飞）。"""
    in_flight = 0
    max_in_flight = 0

    async def handler(request: httpx.Request) -> httpx.Response:
        nonlocal in_flight, max_in_flight
        in_flight += 1
        max_in_flight = max(max_in_flight, in_flight)
        await asyncio.sleep(0.02)  # 模拟生成耗时
        in_flight -= 1
        return httpx.Response(200, json=_chat_payload())

    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(side_effect=handler)
        client = BinRagClient(settings)
        collector = SampleCollector(client, concurrency=2)
        results = await collector.collect_many(
            [{"question": f"q{i}"} for i in range(5)]
        )
        assert all(not isinstance(r, CollectError) for r in results)
        assert max_in_flight == 2, f"并发上限未生效，最大在飞 {max_in_flight}"
        await client.aclose()


async def test_collect_many_sample_fault_tolerance(settings):
    """批量采集样本级容错：失败样本以 CollectError 占位，不中断整体。"""
    with respx.mock(assert_all_called=True) as router:
        route = router.post(CHAT_URL)

        def side(request: httpx.Request) -> httpx.Response:
            import json as _json

            q = _json.loads(request.content)["question"]
            if q == "bad":
                return httpx.Response(404, json={"code": 404, "message": "kb 不存在"})
            return httpx.Response(200, json=_chat_payload())

        route.mock(side_effect=side)
        client = BinRagClient(settings)
        collector = SampleCollector(client, concurrency=4)
        results = await collector.collect_many(
            [{"question": "ok1"}, {"question": "bad"}, {"question": "ok2"}]
        )
        assert len(results) == 3
        assert not isinstance(results[0], CollectError)
        assert isinstance(results[1], CollectError)
        assert not isinstance(results[2], CollectError)
        await client.aclose()


async def test_concurrency_clamped_to_8(settings):
    """样本并发上限钳制到 8（architect-design §4.3）。"""
    client = BinRagClient(settings)
    collector = SampleCollector(client, concurrency=99)
    assert collector._sem._value == 8  # type: ignore[attr-defined]
    await client.aclose()
