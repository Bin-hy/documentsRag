"""T2 验证：配置模块可用，EVAL_ 前缀环境变量生效。"""

from __future__ import annotations

from ragas_eval.config import Settings


def test_settings_instantiate_with_required_fields():
    """必填项注入后可实例化（task.md T2 验证方式）。"""
    s = Settings(
        internal_token="t",
        binrag_api_key="k",
        judge_api_key="j",
        embed_api_key="e",
    )
    assert s.listen_port == 8090
    assert s.max_running_tasks == 2
    assert s.sample_concurrency == 4
    assert s.judge_rpm == 60
    assert s.task_deadline == 7200


def test_settings_env_prefix(monkeypatch):
    """EVAL_ 前缀环境变量覆盖生效。"""
    monkeypatch.setenv("EVAL_LISTEN_PORT", "19090")
    monkeypatch.setenv("EVAL_JUDGE_MODEL", "qwen-plus")
    monkeypatch.setenv("EVAL_INTERNAL_TOKEN", "env-token")
    monkeypatch.setenv("EVAL_BINRAG_API_KEY", "k")
    monkeypatch.setenv("EVAL_JUDGE_API_KEY", "j")
    monkeypatch.setenv("EVAL_EMBED_API_KEY", "e")
    s = Settings()
    assert s.listen_port == 19090
    assert s.judge_model == "qwen-plus"
    assert s.internal_token == "env-token"


def test_sample_concurrency_clamped():
    """样本并发钳制 [1, 8]（architect-design §4.3 上限 8）。"""
    s = Settings(
        internal_token="t", binrag_api_key="k", judge_api_key="j", embed_api_key="e",
        sample_concurrency=99,
    )
    assert s.sample_concurrency == 8
