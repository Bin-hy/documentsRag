"""Judge LLM RPM 令牌桶限流（T6）。

评测是重 LLM 流量的后台任务（单样本 faithfulness 就要 1+N 次调用），
必须独立于采集侧限流，避免打爆供应商配额（architect-design §4.3 第三层）。
"""

from __future__ import annotations

import asyncio
import time


class TokenBucket:
    """异步令牌桶：按 RPM 匀速补充令牌，acquire 无令牌时挂起等待。

    语义对齐 Go 侧 rate.NewLimiter：允许瞬时突发不超过桶容量，
    长期速率收敛于 RPM。
    """

    def __init__(self, rpm: int, burst: int | None = None) -> None:
        # 桶容量默认等于一分钟配额（burst 即允许的最大瞬时并发）
        self._capacity = float(burst if burst is not None else max(1, rpm))
        self._refill_per_sec = max(1.0, float(rpm)) / 60.0
        self._tokens = self._capacity  # 启动满桶
        self._last = time.monotonic()
        self._lock = asyncio.Lock()

    async def acquire(self) -> None:
        """取一个令牌；不足时按补充速率睡到够用为止。"""
        while True:
            async with self._lock:
                now = time.monotonic()
                # 按经过时间补充令牌（不超过容量）
                self._tokens = min(
                    self._capacity, self._tokens + (now - self._last) * self._refill_per_sec
                )
                self._last = now
                if self._tokens >= 1.0:
                    self._tokens -= 1.0
                    return
                # 计算还需等待多久才有一个令牌
                wait = (1.0 - self._tokens) / self._refill_per_sec
            await asyncio.sleep(wait)

    @property
    def available(self) -> float:
        """当前可用令牌数（测试/观测用，不精确加锁）。"""
        return self._tokens
