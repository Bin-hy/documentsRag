"""存储层数据模型（T3）：Dataset / Task / Sample / Report dataclass。

字段与 store/db.py 的表结构一一对应；JSON 列在 repo 层完成序列化/反序列化，
本模块持有的是已解析的 Python 对象。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

# ---------- 任务状态枚举（生命周期见 core/lifecycle.py） ----------
STATUS_PENDING = "pending"  # 待处理（已落库排队）
STATUS_COLLECTING = "collecting"  # 采集中（逐样本回调 Go chat）
STATUS_EVALUATING = "evaluating"  # 评测中（RAGAS 四指标）
STATUS_COMPLETED = "completed"  # 完成（允许部分样本失败）
STATUS_FAILED = "failed"  # 任务级失败
STATUS_CANCELED = "canceled"  # 已取消（样本边界协作式）

TASK_STATUSES: tuple[str, ...] = (
    STATUS_PENDING,
    STATUS_COLLECTING,
    STATUS_EVALUATING,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_CANCELED,
)

# 终态集合：进入终态后不再迁移
TERMINAL_STATUSES: frozenset[str] = frozenset({STATUS_COMPLETED, STATUS_FAILED, STATUS_CANCELED})

# 运行态集合：可被取消 / 重启时被恢复重排队
RUNNING_STATUSES: frozenset[str] = frozenset({STATUS_PENDING, STATUS_COLLECTING, STATUS_EVALUATING})

# ---------- 样本状态 ----------
SAMPLE_PENDING = "pending"  # 待采集
SAMPLE_COLLECTED = "collected"  # 已采集（有 answer+contexts，未评测）
SAMPLE_OK = "ok"  # 评测完成
SAMPLE_ERROR = "error"  # 样本级失败（采集或评测），不阻断任务


@dataclass
class Dataset:
    """数据集记录（datasets 表）。"""

    id: str
    name: str
    source_format: str  # json | jsonl
    content_hash: str  # sha256:...
    sample_count: int
    with_reference_count: int
    raw_blob: str  # 原始文件内容
    created_at: str


@dataclass
class TaskProgress:
    """任务进度（tasks.progress_json）。"""

    total: int = 0
    collected: int = 0  # 采集完成数
    evaluated: int = 0  # 评测完成数
    failed: int = 0  # 样本级失败数（不阻断整体）


@dataclass
class Task:
    """评测任务记录（tasks 表）。"""

    id: str
    name: str
    dataset_id: str
    kb_id: str
    judge_model: str
    metrics: list[str] = field(default_factory=list)
    sample_concurrency: int = 4
    strategy: str | None = None
    status: str = STATUS_PENDING
    progress: TaskProgress = field(default_factory=TaskProgress)
    error_message: str = ""
    idempotency_key: str | None = None
    config_snapshot: dict[str, Any] = field(default_factory=dict)
    created_at: str = ""
    started_at: str | None = None
    finished_at: str | None = None


@dataclass
class Sample:
    """样本记录（samples 表）。

    scores 结构：{metric: {"score": float|None, "reason": str}}；
    context_recall 在无 reference 样本上记 {"score": None, "reason": "N/A（缺标准答案）"}。
    """

    id: str
    task_id: str
    idx: int
    question: str
    reference: str = ""
    kb_id: str = ""
    expected_ids: list[str] = field(default_factory=list)
    answer: str = ""
    contexts: list[dict[str, Any]] = field(default_factory=list)  # [{id,filename,heading,score,content}]
    scores: dict[str, dict[str, Any]] = field(default_factory=dict)
    status: str = SAMPLE_PENDING
    error: str = ""


@dataclass
class Report:
    """汇总报告（reports 表）：任务完成时一次性算好落库，读取不扫样本表。"""

    task_id: str
    summary: dict[str, Any]
    created_at: str
