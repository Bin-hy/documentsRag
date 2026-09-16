"""T5 验证：中文提示词——版本常量、回归样本输出格式解析（每提示词 ≥3 样本）。"""

from __future__ import annotations

import pytest

from ragas_eval.core import prompts
from ragas_eval.core.prompts import (
    PROMPT_VERSION,
    REGRESSION_SAMPLES,
    PromptParseError,
    parse_nli_verdicts,
    parse_precision_verdict,
    parse_recall_claims,
    parse_relevancy_question,
    parse_statements,
)


def test_prompt_version_constant():
    """提示词版本常量（config_snapshot 三要素之一）。

    zh-v2：输出 shape 对齐 ragas 0.3.9 output_model（对象包裹）。
    """
    assert PROMPT_VERSION == "zh-v2"


def test_four_prompts_are_chinese_with_output_shape():
    """四套提示词实例化：中文角色段 + 对齐 ragas output_model 的输出格式说明。"""
    for p in (
        prompts.FAITHFULNESS_STATEMENT_PROMPT,
        prompts.FAITHFULNESS_NLI_PROMPT,
        prompts.ANSWER_RELEVANCY_PROMPT,
        prompts.CONTEXT_PRECISION_PROMPT,
        prompts.CONTEXT_RECALL_PROMPT,
    ):
        assert "## 角色" in p
        # 输出强制 JSON（prompt-design §2.3：提示词保留 "JSON" 字样）
        assert "JSON" in p
    # 输出 shape 对齐 ragas 0.3.9 output_model（对象包裹键名）
    assert '"statements"' in prompts.FAITHFULNESS_STATEMENT_PROMPT
    assert '"statements"' in prompts.FAITHFULNESS_NLI_PROMPT
    assert '"verdict"' in prompts.FAITHFULNESS_NLI_PROMPT
    assert '"noncommittal"' in prompts.ANSWER_RELEVANCY_PROMPT
    assert '"verdict"' in prompts.CONTEXT_PRECISION_PROMPT
    assert '"classifications"' in prompts.CONTEXT_RECALL_PROMPT
    assert '"attributed"' in prompts.CONTEXT_RECALL_PROMPT
    # ragas 0.3.9 to_string 不对 instruction 做 str.format：不得残留占位符
    for p in (
        prompts.FAITHFULNESS_STATEMENT_PROMPT,
        prompts.FAITHFULNESS_NLI_PROMPT,
        prompts.ANSWER_RELEVANCY_PROMPT,
        prompts.CONTEXT_PRECISION_PROMPT,
        prompts.CONTEXT_RECALL_PROMPT,
    ):
        assert "{answer}" not in p and "{reference}" not in p and "{response}" not in p


def test_regression_samples_count():
    """每套提示词 ≥3 个回归测试样本（prompt-design §1.5 治理要求）。"""
    for name, samples in REGRESSION_SAMPLES.items():
        assert len(samples) >= 3, f"{name} 回归样本不足 3 个"


# ---------------- faithfulness 第一步：陈述拆解 ----------------


def test_statements_regression():
    for case in REGRESSION_SAMPLES["faithfulness_statements"]:
        assert parse_statements(case["raw"]) == case["expect"], case["name"]


# ---------------- faithfulness 第二步：NLI 核验 ----------------


def test_nli_regression():
    for case in REGRESSION_SAMPLES["faithfulness_nli"]:
        if case.get("expect_error"):
            with pytest.raises(PromptParseError):
                parse_nli_verdicts(case["raw"])
        else:
            verdicts = parse_nli_verdicts(case["raw"])
            assert [v.verdict for v in verdicts] == case["expect_verdicts"], case["name"]
            assert all(v.reason for v in verdicts)  # 判定理由必须存在


# ---------------- answer_relevancy：逆向问题生成 ----------------


def test_relevancy_regression():
    for case in REGRESSION_SAMPLES["answer_relevancy"]:
        out = parse_relevancy_question(case["raw"])
        assert out.question == case["expect_question"], case["name"]
        assert out.noncommittal == case["expect_noncommittal"], case["name"]


# ---------------- context_precision：上下文有用性 ----------------


def test_precision_regression():
    for case in REGRESSION_SAMPLES["context_precision"]:
        out = parse_precision_verdict(case["raw"])
        assert out.verdict == case["expect_verdict"], case["name"]
        assert out.reason  # 中文理由


# ---------------- context_recall：要点归因 ----------------


def test_recall_regression():
    for case in REGRESSION_SAMPLES["context_recall"]:
        if case.get("expect_error"):
            with pytest.raises(PromptParseError):
                parse_recall_claims(case["raw"])
        else:
            claims = parse_recall_claims(case["raw"])
            assert [c.attributed for c in claims] == case["expect_attributed"], case["name"]


# ---------------- JSON 提取容错 ----------------


def test_json_extraction_tolerance():
    """JSON 提取容错：代码块包裹 / 解释性前缀后缀。"""
    assert parse_precision_verdict('```json\n{"reason": "r", "verdict": 1}\n```').verdict == 1
    assert parse_precision_verdict('好的，判定结果：{"reason": "r", "verdict": 0}以上').verdict == 0


def test_parse_failure_on_garbage():
    """完全非 JSON 输出抛 PromptParseError（调用方据此重试/记 NaN）。"""
    with pytest.raises(PromptParseError):
        parse_statements("我无法完成这个任务")
