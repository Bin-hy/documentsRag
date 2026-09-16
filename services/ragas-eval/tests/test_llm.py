"""T6 验证：LLM 适配与限流——正常评分、429 重试、坏 JSON 容错、限流生效。

respx mock OpenAI 兼容 /chat/completions 端点。
"""

from __future__ import annotations

import time

import httpx
import pytest
import respx

from ragas_eval.core.prompts import PromptParseError
from ragas_eval.llm.factory import JudgeLLMClient, JudgeUnavailableError
from ragas_eval.llm.ratelimit import TokenBucket

CHAT_URL = "http://judge-test/v1/chat/completions"


def _completion(content: str) -> dict:
    """构造 OpenAI 兼容 chat completion 响应体。"""
    return {"choices": [{"message": {"content": content}}]}


async def test_chat_json_normal(settings):
    """正常评分：200 + 合法 JSON 直接解析。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(
            return_value=httpx.Response(200, json=_completion('{"reason": "一致", "verdict": 1}'))
        )
        client = JudgeLLMClient(settings)
        out = await client.chat_json("判定一下")
        assert out == {"reason": "一致", "verdict": 1}
        # 请求体确定性参数：temperature=0 + json_object
        sent = router.calls[0].request
        import json as _json

        payload = _json.loads(sent.content)
        assert payload["temperature"] == 0
        assert payload["response_format"] == {"type": "json_object"}
        await client.aclose()


async def test_chat_json_retry_on_429(settings, monkeypatch):
    """429 限流：指数退避重试后成功（重试事件计数）。"""
    with respx.mock(assert_all_called=True) as router:
        route = router.post(CHAT_URL)
        route.side_effect = [
            httpx.Response(429, json={"error": "rate limited"}),
            httpx.Response(200, json=_completion('{"verdict": 1, "reason": "ok"}')),
        ]
        # patch 退避睡眠间接层，保持测试快且记录退避序列
        import ragas_eval.llm.factory as factory

        sleeps: list[float] = []

        async def fake_sleep(s):  # type: ignore[no-untyped-def]
            sleeps.append(s)

        monkeypatch.setattr(factory, "_sleep", fake_sleep)
        client = JudgeLLMClient(settings)
        out = await client.chat_json("判定")
        assert out["verdict"] == 1
        assert client.stats.retry_events == 1
        assert sleeps == [1.0]  # 首次退避 1s
        await client.aclose()


async def test_chat_json_5xx_exhausted(settings, monkeypatch):
    """5xx 重试耗尽：抛 JudgeUnavailableError。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(return_value=httpx.Response(500, json={"error": "boom"}))
        import ragas_eval.llm.factory as factory

        async def _noop(s):  # type: ignore[no-untyped-def]
            pass

        monkeypatch.setattr(factory, "_sleep", _noop)
        client = JudgeLLMClient(settings)
        with pytest.raises(JudgeUnavailableError):
            await client.chat_json("判定")
        assert client.stats.retry_events == 3  # 1s/2s/4s 三次退避
        await client.aclose()


async def test_chat_json_bad_json_tolerance(settings):
    """坏 JSON 容错：首个 { 至末个 } 提取成功（代码块包裹场景）。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(
            return_value=httpx.Response(
                200, json=_completion('```json\n{"verdict": 0, "reason": "矛盾"}\n```')
            )
        )
        client = JudgeLLMClient(settings)
        out = await client.chat_json("判定")
        assert out["verdict"] == 0
        await client.aclose()


async def test_chat_json_parse_failure_then_raise(settings):
    """解析失败 ≤2 次重试后抛 PromptParseError 并计入 parse_failure_rate。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(return_value=httpx.Response(200, json=_completion("不是JSON")))
        client = JudgeLLMClient(settings)
        with pytest.raises(PromptParseError):
            await client.chat_json("判定")
        assert len(router.calls) == 3  # 初次 + 2 次重试
        assert client.stats.parse_failures == 1
        assert client.stats.parse_failure_rate == 1.0
        await client.aclose()


async def test_chat_json_4xx_no_retry(settings):
    """4xx（非 429）不重试，直接抛错。"""
    with respx.mock(assert_all_called=True) as router:
        router.post(CHAT_URL).mock(return_value=httpx.Response(401, json={"error": "bad key"}))
        client = JudgeLLMClient(settings)
        with pytest.raises(JudgeUnavailableError):
            await client.chat_json("判定")
        assert len(router.calls) == 1
        await client.aclose()


async def test_token_bucket_rate_limit():
    """限流生效：RPM 令牌桶让突发后的请求等待补充。"""
    bucket = TokenBucket(rpm=600, burst=1)  # 0.1s/令牌，桶容量 1
    await bucket.acquire()  # 立刻拿到
    start = time.monotonic()
    await bucket.acquire()  # 需等待约 0.1s
    elapsed = time.monotonic() - start
    assert elapsed >= 0.05, f"令牌桶未生效，等待仅 {elapsed:.3f}s"


async def test_token_bucket_burst():
    """桶容量内允许突发。"""
    bucket = TokenBucket(rpm=60, burst=3)
    start = time.monotonic()
    for _ in range(3):
        await bucket.acquire()
    assert time.monotonic() - start < 0.5  # 3 个突发立刻放行
