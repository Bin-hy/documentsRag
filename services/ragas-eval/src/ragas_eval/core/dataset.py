"""数据集解析与校验：复用 internal/eval 的 EvalSample 格式（spec F1 / architect D10）。

校验规则与 Go 侧 internal/eval/dataset.go 的 Validate 严格对齐：
- 数据集为空 / 没有样本 → 错误
- 逐样本：question 去空白后非空；expected_ids 非 nil（允许空数组）
- JSONL 解析失败报行号；错误消息文案与 Go 侧保持一致（422 带行号）
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field
from typing import Any

# 支持的数据集格式（按文件扩展名判定，与 Go LoadDataset 一致）
SUPPORTED_FORMATS = (".json", ".jsonl")


class DatasetValidationError(ValueError):
    """数据集格式校验失败（HTTP 422，message 带行号/字段，对齐 Go 报错风格）。"""


@dataclass
class EvalSample:
    """数据集单条样本（对齐 Go EvalSample 结构）。"""

    question: str
    answer: str = ""  # 标准答案（可选，缺失时 context_recall 降级 N/A）
    expected_ids: list[str] = field(default_factory=list)  # Recall@K 用（RAGAS 侧存档）
    kb_id: str = ""  # 知识库范围（可选，空 = 不限定）


@dataclass
class ParsedDataset:
    """解析+校验通过的数据集。"""

    name: str
    samples: list[EvalSample]
    source_format: str  # json | jsonl
    content_hash: str  # sha256:...（原始文件内容哈希）
    warnings: list[str] = field(default_factory=list)  # 降级提示（如缺 reference 数量）

    @property
    def with_reference_count(self) -> int:
        """含标准答案的样本数（决定 context_recall 覆盖率）。"""
        return sum(1 for s in self.samples if s.answer.strip())


def _validate_sample(idx: int, raw: Any) -> EvalSample:
    """校验并构造单条样本（idx 为 1 起的人类可读序号，用于报错行号）。"""
    if not isinstance(raw, dict):
        raise DatasetValidationError(f"第 {idx} 条样本不是 JSON 对象")
    question = str(raw.get("question") or "").strip()
    if not question:
        # 对齐 Go：第 %d 条样本 question 为空
        raise DatasetValidationError(f"第 {idx} 条样本 question 为空")
    expected_ids = raw.get("expected_ids")
    if expected_ids is None:
        # 对齐 Go：expected_ids 为 nil 时报错（应使用空数组）
        raise DatasetValidationError(f"第 {idx} 条样本 expected_ids 为 nil（应使用空数组）")
    if not isinstance(expected_ids, list) or not all(isinstance(x, str) for x in expected_ids):
        raise DatasetValidationError(f"第 {idx} 条样本 expected_ids 必须是字符串数组")
    return EvalSample(
        question=question,
        answer=str(raw.get("answer") or ""),
        expected_ids=expected_ids,
        kb_id=str(raw.get("kb_id") or ""),
    )


def parse_dataset(content: bytes, filename: str) -> ParsedDataset:
    """解析并校验数据集文件（.json 整体解析 / .jsonl 逐行解析）。

    格式非法抛 DatasetValidationError（调用方映射 422）。
    """
    lower = filename.lower()
    if lower.endswith(".jsonl"):
        source_format = "jsonl"
        name, samples = _parse_jsonl(content)
    elif lower.endswith(".json"):
        source_format = "json"
        name, samples = _parse_json(content)
    else:
        # 对齐 Go：不支持的数据集格式: %s（支持 .json / .jsonl）
        ext = lower[lower.rfind(".") :] if "." in lower else lower
        raise DatasetValidationError(f"不支持的数据集格式: {ext}（支持 .json / .jsonl）")

    # 对齐 Go Validate：数据集没有样本
    if not samples:
        raise DatasetValidationError("数据集没有样本")

    content_hash = "sha256:" + hashlib.sha256(content).hexdigest()
    ds = ParsedDataset(
        name=name or filename,
        samples=samples,
        source_format=source_format,
        content_hash=content_hash,
    )
    # 降级提示：缺 reference 的样本数（context_recall 将跳过，spec F1）
    missing = len(samples) - ds.with_reference_count
    if missing:
        ds.warnings.append(f"{missing} 条样本缺 reference，context_recall 将跳过")
    return ds


def _parse_json(content: bytes) -> tuple[str, list[EvalSample]]:
    """JSON 格式：{name, samples: [...]} 整体解析。"""
    try:
        payload = json.loads(content)
    except json.JSONDecodeError as exc:
        raise DatasetValidationError(f"解析数据集失败: {exc}") from exc
    if not isinstance(payload, dict):
        raise DatasetValidationError("JSON 数据集必须是对象（含 name 与 samples 字段）")
    raw_samples = payload.get("samples")
    if raw_samples is None:
        raise DatasetValidationError("数据集没有样本")
    if not isinstance(raw_samples, list):
        raise DatasetValidationError("samples 字段必须是数组")
    samples = [_validate_sample(i + 1, s) for i, s in enumerate(raw_samples)]
    return str(payload.get("name") or ""), samples


def _parse_jsonl(content: bytes) -> tuple[str, list[EvalSample]]:
    """JSONL 格式：每行一条样本（空行跳过），解析失败报行号。"""
    samples: list[EvalSample] = []
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise DatasetValidationError(f"解析数据集失败: 非 UTF-8 编码 ({exc})") from exc
    for line_no, line in enumerate(text.splitlines(), start=1):
        line = line.strip()
        if not line:
            continue
        try:
            raw = json.loads(line)
        except json.JSONDecodeError as exc:
            # 对齐 Go：JSONL 第 %d 行解析失败
            raise DatasetValidationError(f"JSONL 第 {line_no} 行解析失败: {exc}") from exc
        samples.append(_validate_sample(line_no, raw))
    return "", samples


def samples_from_raw_blob(raw_blob: str, source_format: str) -> list[EvalSample]:
    """从库存原文重建样本列表（worker 执行任务时用）。

    raw_blob 是入库时已校验通过的原文，此处再解析仅作重建，
    理论上不会再失败；若失败说明数据损坏，抛 DatasetValidationError。
    """
    content = raw_blob.encode("utf-8")
    fake_name = f"dataset.{source_format}"
    return parse_dataset(content, fake_name).samples
