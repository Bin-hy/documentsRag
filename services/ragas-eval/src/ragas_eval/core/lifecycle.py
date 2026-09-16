"""任务状态机（T4）：合法迁移集中定义，worker 与 API 层共用。

状态图（architect-design §4.1）：

    pending ──▶ collecting ──▶ evaluating ──▶ completed
       │            │              │
       ▼            ▼              ▼
    canceled ◀──（样本边界协作取消，进行中的 LLM 调用不硬断）
    任意运行态 ──▶ failed（任务级错误）

特殊迁移：collecting/evaluating → pending（仅启动恢复路径使用，样本级断点续跑）。
"""

from __future__ import annotations

from ragas_eval.store import models as m

# 合法迁移表：当前状态 -> 允许迁往的状态集合
_TRANSITIONS: dict[str, frozenset[str]] = {
    m.STATUS_PENDING: frozenset(
        {m.STATUS_COLLECTING, m.STATUS_CANCELED, m.STATUS_FAILED}
    ),
    m.STATUS_COLLECTING: frozenset(
        # pending：服务重启恢复时重置重排队
        {m.STATUS_EVALUATING, m.STATUS_CANCELED, m.STATUS_FAILED, m.STATUS_PENDING}
    ),
    m.STATUS_EVALUATING: frozenset(
        {m.STATUS_COMPLETED, m.STATUS_CANCELED, m.STATUS_FAILED, m.STATUS_PENDING}
    ),
    # 终态：不允许任何迁移
    m.STATUS_COMPLETED: frozenset(),
    m.STATUS_FAILED: frozenset(),
    m.STATUS_CANCELED: frozenset(),
}


class IllegalTransitionError(ValueError):
    """非法状态迁移。"""

    def __init__(self, from_status: str, to_status: str) -> None:
        super().__init__(f"非法状态迁移: {from_status} -> {to_status}")
        self.from_status = from_status
        self.to_status = to_status


def can_transition(from_status: str, to_status: str) -> bool:
    """判断 from_status -> to_status 是否合法。"""
    return to_status in _TRANSITIONS.get(from_status, frozenset())


def ensure_transition(from_status: str, to_status: str) -> None:
    """校验迁移合法性，非法则抛 IllegalTransitionError。"""
    if not can_transition(from_status, to_status):
        raise IllegalTransitionError(from_status, to_status)


def is_terminal(status: str) -> bool:
    """是否终态（completed/failed/canceled）。"""
    return status in m.TERMINAL_STATUSES


def is_running(status: str) -> bool:
    """是否运行态（pending/collecting/evaluating，可取消）。"""
    return status in m.RUNNING_STATUSES


def can_complete(total: int, ok: int, failed: int) -> bool:
    """完成判定：ok + failed == total（允许部分样本失败，报告含 coverage）。

    要求 total > 0，空任务不允许直接完成（数据集校验已保证 total >= 1，
    此处防御编程）。
    """
    return total > 0 and ok + failed >= total
