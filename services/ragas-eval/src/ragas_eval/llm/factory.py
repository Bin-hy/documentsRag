"""LLM/Embedding 适配层（T6）：唯一与模型供应商通信的模块。

两部分：
1. build_ragas_llm / build_ragas_embeddings：把 OpenAI 兼容端点适配为 ragas
   接口（LangchainLLMWrapper / LangchainEmbeddingsWrapper），temperature=0，
   max_retries=0（重试由微服务统一控制，禁用底层隐式重试）。
2. JudgeLLMClient：httpx 直连 OpenAI 兼容 /chat/completions 的评审客户端，
   承载重试退避（429/5xx 指数退避 1s/2s/4s）、JSON 三层降级解析、RPM 限流、
   parse_failure 统计——供 runner 的逐样本降级路径与健康检查使用。

约束（prompt-design §2）：temperature=0 确定性优先；thinking 模式关闭；
评审模型与生成模型分离配置。
"""

from __future__ import annotations

import asyncio
import time
from dataclasses import dataclass, field
from typing import Any

import httpx

from ragas_eval.config import Settings
from ragas_eval.core.prompts import PromptParseError, _loads
from ragas_eval.llm.ratelimit import TokenBucket

# 429/5xx 指数退避序列（对齐 Go 侧 llm.go MaxRetries 与退避公式）
_RETRY_BACKOFFS = (1.0, 2.0, 4.0)
# JSON 解析失败最大重试次数（仍失败记 NaN 并计入 parse_failure_rate）
_MAX_PARSE_RETRIES = 2

# 退避睡眠间接层（测试可 patch 避免真实等待）
_sleep = asyncio.sleep


class JudgeUnavailableError(RuntimeError):
    """评审模型端点不可达（429/5xx 重试耗尽或网络错误）。"""


@dataclass
class JudgeStats:
    """评审调用统计：解析失败率单独上报（prompt-design §5.4）。"""

    total_calls: int = 0
    parse_failures: int = 0
    retry_events: int = 0
    extra: dict[str, Any] = field(default_factory=dict)

    @property
    def parse_failure_rate(self) -> float:
        """解析失败率；> 5% 视为提示词/模型故障，评测结论标记不可靠。"""
        return self.parse_failures / self.total_calls if self.total_calls else 0.0


class JudgeLLMClient:
    """评审模型客户端：OpenAI 兼容 /chat/completions，httpx 直连。

    - temperature=0、response_format=json_object（首选结构化输出）
    - 429/5xx 指数退避重试（1s/2s/4s，上限 3 次）
    - JSON 解析三层降级（response_format → {} 提取 → json-repair 兜底）
    - 解析失败 ≤2 次重试后抛 PromptParseError（调用方记 NaN）
    - 全局限速：进入请求前过 RPM 令牌桶
    """

    def __init__(
        self,
        settings: Settings,
        rate_limiter: TokenBucket | None = None,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        self._settings = settings
        self._bucket = rate_limiter or TokenBucket(settings.judge_rpm)
        # 允许注入 http_client 便于 respx mock；否则按配置自建
        self._client = http_client or httpx.AsyncClient(
            base_url=settings.judge_base_url.rstrip("/"),
            headers={"Authorization": f"Bearer {settings.judge_api_key}"},
            timeout=httpx.Timeout(settings.judge_timeout, connect=5.0),
        )
        self.stats = JudgeStats()

    async def aclose(self) -> None:
        await self._client.aclose()

    async def _post_chat(self, prompt: str) -> str:
        """单次 chat 调用（含 429/5xx 退避重试），返回 content 文本。"""
        payload = {
            "model": self._settings.judge_model,
            "temperature": 0,  # 强制确定性（N2 可复现）
            "max_tokens": self._settings.judge_max_tokens,
            "response_format": {"type": "json_object"},  # 首选结构化输出
            "messages": [{"role": "user", "content": prompt}],
        }
        last_exc: Exception | None = None
        for backoff in (0.0, *_RETRY_BACKOFFS):
            if backoff:
                self.stats.retry_events += 1
                await _sleep(backoff)
            await self._bucket.acquire()  # RPM 限流：所有评审调用必经令牌桶
            try:
                resp = await self._client.post("/chat/completions", json=payload)
            except httpx.HTTPError as exc:
                last_exc = exc  # 网络错误：进入下一轮退避
                continue
            if resp.status_code == 429 or resp.status_code >= 500:
                last_exc = JudgeUnavailableError(f"评审模型 HTTP {resp.status_code}")
                continue  # 限流/服务端错误：退避重试
            if resp.status_code >= 400:
                # 4xx（非 429）多为配置错误，不重试
                raise JudgeUnavailableError(
                    f"评审模型请求被拒 HTTP {resp.status_code}: {resp.text[:200]}"
                )
            body = resp.json()
            return body["choices"][0]["message"]["content"]
        raise JudgeUnavailableError(f"评审模型重试耗尽: {last_exc}") from last_exc

    async def chat_json(self, prompt: str) -> Any:
        """评审调用 + JSON 解析：解析失败 ≤2 次重试，仍失败抛 PromptParseError。

        解析失败计入 stats.parse_failures（每次最终失败计一次）。
        """
        self.stats.total_calls += 1
        last_exc: Exception | None = None
        for _ in range(_MAX_PARSE_RETRIES + 1):
            raw = await self._post_chat(prompt)
            try:
                return _loads(raw)
            except PromptParseError as exc:
                last_exc = exc  # 原样重试（prompt-design §2.4）
        self.stats.parse_failures += 1
        raise PromptParseError(f"JSON 解析重试 {_MAX_PARSE_RETRIES} 次仍失败") from last_exc

    async def ping(self) -> bool:
        """连通性探活（健康检查用，结果由调用方缓存 60s 防打满）。"""
        try:
            resp = await self._client.post(
                "/chat/completions",
                json={
                    "model": self._settings.judge_model,
                    "temperature": 0,
                    "max_tokens": 1,
                    "messages": [{"role": "user", "content": "ping"}],
                },
            )
            return resp.status_code < 500
        except httpx.HTTPError:
            return False


def build_ragas_llm(settings: Settings) -> Any:
    """构造 ragas 评审 LLM（LangchainLLMWrapper 包装 OpenAI 兼容端点）。

    temperature=0、max_retries=0（重试由微服务统一控制）。
    延迟导入 langchain_openai/ragas：测试环境不依赖真实供应商包也能导入本模块。
    """
    from langchain_openai import ChatOpenAI
    from ragas.llms import LangchainLLMWrapper

    return LangchainLLMWrapper(
        ChatOpenAI(
            base_url=settings.judge_base_url,
            api_key=settings.judge_api_key,
            model=settings.judge_model,
            temperature=0,  # 评分任务要求可复现
            max_tokens=settings.judge_max_tokens,
            timeout=settings.judge_timeout,
            max_retries=0,  # 禁用底层隐式重试，由微服务统一退避
        )
    )


def build_ragas_embeddings(settings: Settings) -> Any:
    """构造 ragas Embedding（answer_relevancy 用，必须与索引进库同源）。"""
    from langchain_openai import OpenAIEmbeddings
    from ragas.embeddings import LangchainEmbeddingsWrapper

    return LangchainEmbeddingsWrapper(
        OpenAIEmbeddings(
            base_url=settings.embed_base_url,
            api_key=settings.embed_api_key,
            model=settings.embed_model,
            # 兼容性修正（T22 联调实测）：langchain 默认用 tiktoken 把文本转成
            # token id 数组分块，且 encoding_format 默认 base64——非 OpenAI 官方
            # 的兼容端点（如 ollama、vLLM）不接受这两种形态，返回 400。
            # 关闭 tiktoken 分块（原样发文本）并显式用 float 编码，
            # 对官方 OpenAI 端点同样无副作用。
            check_embedding_ctx_length=False,
            encoding_format="float",
        )
    )


@dataclass
class PingCache:
    """judge_llm 探活结果缓存（health 缓存 60s，不打满供应商）。"""

    ttl_seconds: float = 60.0
    _value: bool = False
    _ts: float = 0.0

    def get(self) -> bool | None:
        """命中缓存返回结果，未命中/过期返回 None。"""
        if time.monotonic() - self._ts < self.ttl_seconds and self._ts > 0:
            return self._value
        return None

    def set(self, value: bool) -> None:
        self._value = value
        self._ts = time.monotonic()
