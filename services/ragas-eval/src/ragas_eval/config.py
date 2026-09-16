"""配置模块（T2）：pydantic-settings 定义全部运行配置。

所有配置项均支持 `EVAL_` 前缀环境变量覆盖（如 EVAL_LISTEN_PORT=8090），
对齐 architect-design §7.1 的 compose 环境变量约定与 spec N1「配置驱动」。
"""

from pydantic import field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# 支持评测的四个 RAGAS 指标（合法值集合，任务提交时校验，未知指标 400）
ALL_METRICS: tuple[str, ...] = (
    "faithfulness",
    "answer_relevancy",
    "context_precision",
    "context_recall",
)


class Settings(BaseSettings):
    """评测服务运行配置。

    必填项（无默认值，启动/注入时必须提供）：
    - internal_token：Go 代理与 Python 服务共享的内部令牌
    - binrag_api_key：采集器回调 Go chat 的评测专用 API Key
    - judge_api_key / embed_api_key：模型供应商密钥
    """

    model_config = SettingsConfigDict(
        env_prefix="EVAL_",  # 全部环境变量带 EVAL_ 前缀
        env_file=".env",
        extra="ignore",
    )

    # ---------- 服务监听 ----------
    listen_port: int = 8090  # 仅内网监听，compose 不发布宿主端口

    # ---------- BinRag Go 后端（采集器回调目标） ----------
    binrag_base_url: str = "http://localhost:8085"  # Go 后端内网地址
    binrag_api_key: str = ""  # 评测专用 API Key（系统级，可独立吊销）

    # ---------- 服务间共享内部令牌（Go 代理注入 X-Eval-Internal-Token） ----------
    internal_token: str = ""  # 为空时所有非豁免端点一律 401（纵深防御，不容忍裸奔）

    # ---------- 评审模型（Judge LLM，OpenAI 兼容端点） ----------
    judge_base_url: str = "https://api.openai.com/v1"
    judge_api_key: str = ""
    judge_model: str = "gpt-4o-mini"  # 默认评审模型，可与生成模型分离避免同源偏差
    judge_timeout: float = 60.0  # 单次评审调用超时（秒）
    judge_max_tokens: int = 2048  # 评审输出上限（JSON 判定结果，无需太长）

    # ---------- Embedding（answer_relevancy 用，必须与索引进库同源） ----------
    embed_base_url: str = "https://api.openai.com/v1"
    embed_api_key: str = ""
    embed_model: str = "text-embedding-3-small"
    embed_batch_size: int = 64  # 单批上限，对齐 Go 侧 EmbedderConfig.BatchSize

    # ---------- 存储 ----------
    db_path: str = "./data/eval.db"  # SQLite 文件路径（compose 挂卷持久化）

    # ---------- 并发限额（三层，architect-design §4.3） ----------
    max_running_tasks: int = 2  # 任务级：同时运行的评测任务数，超出排队
    max_queue_size: int = 16  # 排队上限，满载时提交返回 429
    sample_concurrency: int = 4  # 样本级采集并发（默认 4，上限 8）
    judge_rpm: int = 60  # Judge LLM 全局限速（每分钟请求数令牌桶）

    # ---------- 执行参数 ----------
    eval_batch_size: int = 8  # ragas.evaluate 批大小（样本级评测并发的粒度）
    task_deadline: int = 7200  # 任务级硬截止（秒，默认 2h），到期迁移 failed
    sample_soft_timeout: int = 300  # 单样本采集+评测软超时（秒），超时记样本失败
    retention_days: int = 0  # 报告保留天数，0 = 永久保留（预留，防 SQLite 膨胀）

    # ---------- 数据集上传 ----------
    max_dataset_bytes: int = 10 * 1024 * 1024  # 上传大小上限 10MB

    @field_validator("sample_concurrency")
    @classmethod
    def _clamp_sample_concurrency(cls, v: int) -> int:
        """样本采集并发钳制到 [1, 8]（architect-design §4.3：默认 4，上限 8）。"""
        return max(1, min(v, 8))


def get_settings() -> Settings:
    """构造配置实例（独立函数便于测试替换 / 依赖注入）。"""
    return Settings()
