"""T4 验证：任务状态机——遍历合法/非法迁移断言。"""

from __future__ import annotations

import pytest

from ragas_eval.core import lifecycle
from ragas_eval.store import models as m


def test_legal_transitions():
    """合法迁移：pending→collecting→evaluating→completed；运行态→canceled/failed。"""
    legal = [
        (m.STATUS_PENDING, m.STATUS_COLLECTING),
        (m.STATUS_PENDING, m.STATUS_CANCELED),
        (m.STATUS_PENDING, m.STATUS_FAILED),
        (m.STATUS_COLLECTING, m.STATUS_EVALUATING),
        (m.STATUS_COLLECTING, m.STATUS_CANCELED),
        (m.STATUS_COLLECTING, m.STATUS_FAILED),
        (m.STATUS_COLLECTING, m.STATUS_PENDING),  # 重启恢复重置
        (m.STATUS_EVALUATING, m.STATUS_COMPLETED),
        (m.STATUS_EVALUATING, m.STATUS_CANCELED),
        (m.STATUS_EVALUATING, m.STATUS_FAILED),
        (m.STATUS_EVALUATING, m.STATUS_PENDING),  # 重启恢复重置
    ]
    for from_s, to_s in legal:
        assert lifecycle.can_transition(from_s, to_s), f"{from_s}->{to_s} 应合法"
        lifecycle.ensure_transition(from_s, to_s)  # 不抛错


def test_illegal_transitions():
    """非法迁移抛错：终态不可迁移、跨级跳转、逆向迁移。"""
    illegal = [
        (m.STATUS_PENDING, m.STATUS_EVALUATING),  # 跨级
        (m.STATUS_PENDING, m.STATUS_COMPLETED),
        (m.STATUS_COLLECTING, m.STATUS_COMPLETED),  # 跨级
        (m.STATUS_EVALUATING, m.STATUS_COLLECTING),  # 逆向
        (m.STATUS_COMPLETED, m.STATUS_CANCELED),  # 终态
        (m.STATUS_COMPLETED, m.STATUS_PENDING),
        (m.STATUS_FAILED, m.STATUS_PENDING),
        (m.STATUS_CANCELED, m.STATUS_COLLECTING),
    ]
    for from_s, to_s in illegal:
        assert not lifecycle.can_transition(from_s, to_s), f"{from_s}->{to_s} 应非法"
        with pytest.raises(lifecycle.IllegalTransitionError):
            lifecycle.ensure_transition(from_s, to_s)


def test_terminal_and_running_sets():
    """终态/运行态集合语义。"""
    assert lifecycle.is_terminal(m.STATUS_COMPLETED)
    assert lifecycle.is_terminal(m.STATUS_FAILED)
    assert lifecycle.is_terminal(m.STATUS_CANCELED)
    assert not lifecycle.is_terminal(m.STATUS_PENDING)
    assert lifecycle.is_running(m.STATUS_COLLECTING)
    assert not lifecycle.is_running(m.STATUS_COMPLETED)


def test_can_complete_allows_partial_failure():
    """完成判定允许部分样本失败（ok + failed == total）。"""
    assert lifecycle.can_complete(total=10, ok=9, failed=1)
    assert lifecycle.can_complete(total=10, ok=0, failed=10)
    assert not lifecycle.can_complete(total=10, ok=8, failed=1)  # 有样本未到终态
    assert not lifecycle.can_complete(total=0, ok=0, failed=0)  # 空任务防御
