"""四套中文评审提示词（T5）：落地 prompt-design §1 草案，整段覆写 RAGAS Prompt。

治理要求（prompt-design §1.5）：
- PROMPT_VERSION 常量版本化管理，提示词变更必须升版本并过回归样本
- 输出一律「只输出 JSON」，微服务侧做 JSON 提取容错（首个 { 至末个 }）
- 每套提示词配 ≥3 个回归测试样本（happy path / 边界 / 拒答）

zh-v2 变更（T22 联调修复）：
- ragas 0.3.9 的 PydanticPrompt.to_string 不对 instruction 做 str.format
  （输入以 JSON 形式追加在指令之后），因此提示词不含 {占位符} 尾段；
- 输出格式说明与 few-shot 示例全部改为 ragas 各指标 output_model 期望的
  对象包裹 shape（statements / classifications 键），修复裸数组导致的
  OutputParserException；解析函数保留对旧裸数组输出的兼容解包。
"""

from __future__ import annotations

import json
import re
from typing import Any

from pydantic import AliasChoices, BaseModel, Field

# 中文提示词版本（config_snapshot 三要素之一，变更必须升级）
# zh-v2：输出 shape 对齐 ragas 0.3.9 output_model（对象包裹），修复解析失败
PROMPT_VERSION = "zh-v2"

# ======================================================================
# 提示词正文（prompt-design §1.1–§1.4 草案中文化，含中文 few-shot 示例）
# 输出 shape 严格对齐 ragas 0.3.9 各指标 output_model（联调实测确认）：
# - 陈述拆解：{"statements": [str, ...]}
# - NLI 核验：{"statements": [{"statement","reason","verdict"}]}
# - 逆向问题：{"question": str, "noncommittal": int(0/1)}
# - 精确率判定：{"reason": str, "verdict": int}（原本即匹配）
# - 召回核验：{"classifications": [{"statement","reason","attributed"}]}
# ======================================================================

# faithfulness 第一步：陈述拆解（StatementGeneratorPrompt 覆写）
FAITHFULNESS_STATEMENT_PROMPT = """\
## 角色
你是陈述拆解器。你的唯一任务：把一段中文回答拆成「原子陈述」——每条只包含一个事实点。

## 输入说明
输入为 JSON 对象，含 question（用户问题）与 answer（待拆解的回答）两个字段。

## 约束
- 输出格式：JSON 对象 {"statements": ["陈述1", "陈述2"]}。不输出任何其他文字。
- 保留回答的原意，不补充、不推断。
- 无事实内容的客套话（如"希望对你有帮助"）不产生陈述。
- 若回答为空或不含任何事实，输出 {"statements": []}。

## 示例
<example id="1">
输入：{"question": "BinRag 用什么实现，支持哪些文档？", "answer": "BinRag 使用 Go 语言实现，向量存储采用 Qdrant，支持 PDF 与 Markdown 文档。"}
输出：{"statements": ["BinRag 使用 Go 语言实现。", "BinRag 的向量存储采用 Qdrant。", "BinRag 支持 PDF 文档。", "BinRag 支持 Markdown 文档。"]}
</example>

<example id="2">
输入：{"question": "差旅补贴标准是多少？", "answer": "抱歉，知识库中没有找到相关信息。"}
输出：{"statements": []}
</example>

<example id="3">
输入：{"question": "报销流程是什么？", "answer": "报销流程需要先提交申请，审批通过后在 3 个工作日内打款。希望对你有帮助！"}
输出：{"statements": ["报销流程需要先提交申请。", "审批通过后在 3 个工作日内打款。"]}
</example>
"""

# faithfulness 第二步：NLI 逐条核验（NLIStatementPrompt 覆写）
FAITHFULNESS_NLI_PROMPT = """\
## 角色
你是忠实度核验员。给定「上下文」和若干「陈述」，逐条判断该陈述能否被上下文直接支持。

## 输入说明
输入为 JSON 对象，含 context（检索上下文）与 statements（待核验的陈述列表）两个字段。

## 判定标准
- verdict=1：陈述与上下文一致，或可由上下文直接推出（不允许依赖外部知识）。
- verdict=0：上下文没有提及，或与上下文矛盾。

## 约束
- 输出格式：JSON 对象，statements 数组与输入陈述一一对应：
  {"statements": [{"statement": "...", "reason": "一句话理由", "verdict": 0或1}]}
- verdict 必须是数字 0 或 1，不允许输出"是/否"、"true/false"。
- reason 用中文，不超过 30 字。

## 示例
<example id="1">
输入：{"context": "BinRag 的向量存储使用 Qdrant，距离度量默认为余弦相似度。", "statements": ["BinRag 使用 Milvus 作为向量存储。"]}
输出：{"statements": [{"statement": "BinRag 使用 Milvus 作为向量存储。", "reason": "上下文明确为 Qdrant，与陈述矛盾", "verdict": 0}]}
</example>

<example id="2">
输入：{"context": "年假审批通过后，系统会在 3 个工作日内自动打款补偿。", "statements": ["年假审批通过后 3 个工作日内打款。"]}
输出：{"statements": [{"statement": "年假审批通过后 3 个工作日内打款。", "reason": "与上下文表述一致", "verdict": 1}]}
</example>
"""

# answer_relevancy：逆向问题生成（ResponseRelevancePrompt 覆写）
ANSWER_RELEVANCY_PROMPT = """\
## 角色
你是问题还原器。给定一段中文回答，生成 1 个「该回答最可能在回答什么问题」的中文问题。

## 输入说明
输入为 JSON 对象，含 response（待还原的回答）字段。

## 约束
- 输出格式：JSON 对象 {"question": "...", "noncommittal": 0}，不输出其他内容。
- 问题必须是自然的中文疑问句，像真实用户会提的问题（口语化优先，不要书面翻译腔）。
- 问题必须是具体可回答的，不要生成"什么是……"式的宽泛问题，除非回答本身就在下定义。
- noncommittal 必须是数字 0 或 1：正常回答输出 0；若回答是"知识库中没有相关信息"之类的拒答，输出 {"question": "", "noncommittal": 1}。

## 示例
<example id="1">
输入：{"response": "报销需要先在 OA 系统提交申请单，部门主管审批通过后，财务会在 3 个工作日内打款到工资卡。"}
输出：{"question": "报销的流程是怎样的，多久能到账？", "noncommittal": 0}
</example>

<example id="2">
输入：{"response": "Qdrant 是 BinRag 默认的向量数据库，支持余弦、点积和欧氏距离三种度量。"}
输出：{"question": "BinRag 用的向量数据库是什么，支持哪些距离度量？", "noncommittal": 0}
</example>

<example id="3">
输入：{"response": "抱歉，知识库中没有关于该主题的信息。"}
输出：{"question": "", "noncommittal": 1}
</example>
"""

# context_precision：上下文对得出标准答案是否有用（ContextPrecisionPrompt 覆写）
# 输出模型 {"reason": str, "verdict": int} 与原草案一致，无需改 shape
CONTEXT_PRECISION_PROMPT = """\
## 角色
你是检索质量评审员。给定「问题」「标准答案」和一条「检索上下文」，判断该上下文是否对得出标准答案有用。

## 输入说明
输入为 JSON 对象，含 question（问题）、answer（标准答案）、context（检索上下文）三个字段。

## 判定标准
- verdict=1：上下文包含了得出标准答案所需的关键事实，或提供了实质性支撑。
- verdict=0：上下文只是话题沾边、内容重复、或与标准答案无关。
- 注意：与问题话题相关 ≠ 有用。必须能支撑标准答案才算 1。

## 约束
- 输出格式：JSON 对象 {"reason": "一句话中文理由", "verdict": 0或1}，不输出其他内容。
- verdict 必须是数字 0 或 1。

## 示例
<example id="1">
输入：{"question": "BinRag 默认的向量距离度量是什么？", "answer": "默认使用余弦相似度。", "context": "BinRag 的向量存储使用 Qdrant，距离度量默认为余弦相似度。"}
输出：{"reason": "上下文直接给出了余弦相似度这一答案", "verdict": 1}
</example>

<example id="2">
输入：{"question": "BinRag 默认的向量距离度量是什么？", "answer": "默认使用余弦相似度。", "context": "Qdrant 是一个开源向量数据库，用 Rust 编写，支持 HNSW 索引。"}
输出：{"reason": "仅介绍 Qdrant，未涉及 BinRag 的默认度量配置", "verdict": 0}
</example>

<example id="3">
输入：{"question": "请假审批要多久？", "answer": "部门主管审批 1 个工作日，HR 备案当天完成。", "context": "公司实行弹性工作制，上下班时间可在 8:00-10:00 之间灵活选择。"}
输出：{"reason": "讲的是弹性工作制，与请假审批时长无关", "verdict": 0}
</example>
"""

# context_recall：标准答案要点能否被检索上下文支持（ContextRecallClassificationPrompt 覆写）
CONTEXT_RECALL_PROMPT = """\
## 角色
你是召回核验员。给定「问题」「标准答案」和「全部检索上下文」，把标准答案拆成原子要点，逐条判断该要点能否被检索上下文支持。

## 输入说明
输入为 JSON 对象，含 question（问题）、answer（标准答案）、context（全部检索上下文）三个字段。

## 约束
- 输出格式：JSON 对象，classifications 数组按要点顺序输出：
  {"classifications": [{"statement": "标准答案要点", "reason": "一句话中文理由", "attributed": 0或1}]}
- attributed=1 表示要点能被上下文直接支持；0 表示上下文中没有依据。
- attributed 必须是数字 0 或 1。
- 只依据检索上下文判断，不使用任何外部知识。

## 示例
<example id="1">
输入：{"question": "年假补偿的到账规则是什么？", "answer": "审批通过后 3 个工作日内打款，打款到工资卡。", "context": "年假审批通过后，系统会在 3 个工作日内自动打款补偿至员工工资卡。"}
输出：{"classifications": [
  {"statement": "审批通过后 3 个工作日内打款", "reason": "上下文明确支持", "attributed": 1},
  {"statement": "打款到工资卡", "reason": "上下文明确支持", "attributed": 1}
]}
</example>

<example id="2">
输入：{"question": "BinRag 支持哪些文档格式？", "answer": "支持 PDF、Markdown 和扫描件 OCR。", "context": "BinRag 支持 PDF 与 Markdown 文档的上传与解析。"}
输出：{"classifications": [
  {"statement": "支持 PDF", "reason": "上下文明确支持", "attributed": 1},
  {"statement": "支持 Markdown", "reason": "上下文明确支持", "attributed": 1},
  {"statement": "支持扫描件 OCR", "reason": "上下文未提及扫描件或 OCR", "attributed": 0}
]}
</example>
"""


# ======================================================================
# 输出 schema（pydantic 模型，配合解析容错）
# ======================================================================

# 各提示词输出的「对象包裹」键名（对齐 ragas 0.3.9 output_model）
_KEY_STATEMENTS = "statements"  # faithfulness 两步共用
_KEY_CLASSIFICATIONS = "classifications"  # context_recall


class StatementsOutput(BaseModel):
    """faithfulness 第一步输出（对象包裹 shape）。"""

    statements: list[str] = Field(default_factory=list)


class NLIVerdict(BaseModel):
    """faithfulness NLI 单条判定。"""

    statement: str
    reason: str = ""
    verdict: int = Field(ge=0, le=1)  # 强制数字 0/1，其他值视为解析失败


class NLIVerdictsOutput(BaseModel):
    """faithfulness 第二步输出（对象包裹 shape）。"""

    statements: list[NLIVerdict] = Field(default_factory=list)


class RelevancyQuestion(BaseModel):
    """answer_relevancy 逆向生成的问题。"""

    question: str
    # ragas 0.3.9 output_model 的 noncommittal 是 int（0/1），非 bool
    noncommittal: int = Field(default=0, ge=0, le=1)  # 拒答标记（strictness>1 时打 0 分依据）


class PrecisionVerdict(BaseModel):
    """context_precision 单条上下文判定。"""

    reason: str = ""
    verdict: int = Field(ge=0, le=1)


class RecallClaim(BaseModel):
    """context_recall 单个要点判定（字段名对齐 output_model：statement 而非 claim）。

    validation_alias 兼容 zh-v1 提示词时代的旧字段名 claim。
    """

    statement: str = Field(validation_alias=AliasChoices("statement", "claim"))
    reason: str = ""
    attributed: int = Field(ge=0, le=1)


class RecallClassificationsOutput(BaseModel):
    """context_recall 输出（对象包裹 shape）。"""

    classifications: list[RecallClaim] = Field(default_factory=list)


class PromptParseError(ValueError):
    """提示词输出解析失败（调用方据此重试，≤2 次后记 NaN）。"""


# ======================================================================
# JSON 提取容错与解析函数
# ======================================================================


def extract_json_text(raw: str) -> str:
    """JSON 提取容错：提取首个 {/[ 至末个 }/]（与 Go 侧 generateJSON 同款策略）。

    模型常见毛病：JSON 外包裹 ```json 代码块或解释性文字，先剥掉再解析。
    """
    text = raw.strip()
    # 剥 markdown 代码块围栏
    fence = re.search(r"```(?:json)?\s*(.*?)```", text, re.DOTALL)
    if fence:
        text = fence.group(1).strip()
    # 取首个括号到末个括号之间的内容
    starts = [i for i in (text.find("{"), text.find("[")) if i >= 0]
    if not starts:
        raise PromptParseError(f"输出中未找到 JSON 片段: {raw[:120]}")
    start = min(starts)
    end = max(text.rfind("}"), text.rfind("]"))
    if end <= start:
        raise PromptParseError(f"JSON 片段不完整: {raw[:120]}")
    return text[start : end + 1]


def _loads(raw: str) -> Any:
    """三层降级解析：直接 json.loads → 括号提取容错 → json-repair 兜底（若安装）。"""
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        pass
    extracted = extract_json_text(raw)
    try:
        return json.loads(extracted)
    except json.JSONDecodeError:
        pass
    # 第三层：json-repair 修复尾部截断（可选依赖，未安装时抛出解析失败）
    try:
        from json_repair import repair_json  # type: ignore[import-not-found]

        repaired = repair_json(extracted)
        return json.loads(repaired)
    except ImportError as exc:
        raise PromptParseError(f"JSON 解析失败且 json-repair 不可用: {raw[:120]}") from exc
    except Exception as exc:
        raise PromptParseError(f"JSON 解析失败: {raw[:120]}") from exc


def _unwrap(data: Any, key: str) -> Any:
    """兼容解包：dict 取包裹键（ragas output_model shape），list 按原样（旧裸数组）。

    zh-v1 提示词曾让模型输出裸数组，zh-v2 改为对象包裹；解析两侧都兼容，
    避免旧输出/模型偶发裸数组时直接解析失败。
    """
    if isinstance(data, dict):
        if key not in data:
            raise PromptParseError(f"输出对象缺少 {key!r} 键: {list(data.keys())}")
        return data[key]
    return data


def parse_statements(raw: str) -> list[str]:
    """解析陈述拆解输出：{"statements": [...]} 对象包裹（兼容旧裸数组）。"""
    data = _unwrap(_loads(raw), _KEY_STATEMENTS)
    if not isinstance(data, list) or not all(isinstance(s, str) for s in data):
        raise PromptParseError(f"陈述拆解输出不是字符串数组: {raw[:120]}")
    return data


def parse_nli_verdicts(raw: str) -> list[NLIVerdict]:
    """解析 NLI 核验输出：{"statements": [{statement, reason, verdict}]}（兼容旧裸数组）。"""
    data = _unwrap(_loads(raw), _KEY_STATEMENTS)
    if not isinstance(data, list):
        raise PromptParseError(f"NLI 输出不是数组: {raw[:120]}")
    try:
        return [NLIVerdict(**item) for item in data]
    except Exception as exc:
        raise PromptParseError(f"NLI 输出 schema 校验失败: {exc}") from exc


def parse_relevancy_question(raw: str) -> RelevancyQuestion:
    """解析逆向问题生成输出：{question, noncommittal(int 0/1)}。"""
    data = _loads(raw)
    if not isinstance(data, dict):
        raise PromptParseError(f"问题生成输出不是对象: {raw[:120]}")
    try:
        return RelevancyQuestion(**data)
    except Exception as exc:
        raise PromptParseError(f"问题生成输出 schema 校验失败: {exc}") from exc


def parse_precision_verdict(raw: str) -> PrecisionVerdict:
    """解析 context_precision 判定输出：{reason, verdict}。"""
    data = _loads(raw)
    if not isinstance(data, dict):
        raise PromptParseError(f"精确率判定输出不是对象: {raw[:120]}")
    try:
        return PrecisionVerdict(**data)
    except Exception as exc:
        raise PromptParseError(f"精确率判定输出 schema 校验失败: {exc}") from exc


def parse_recall_claims(raw: str) -> list[RecallClaim]:
    """解析召回核验输出：{"classifications": [{statement, reason, attributed}]}（兼容旧裸数组）。"""
    data = _unwrap(_loads(raw), _KEY_CLASSIFICATIONS)
    if not isinstance(data, list):
        raise PromptParseError(f"召回核验输出不是数组: {raw[:120]}")
    try:
        return [RecallClaim(**item) for item in data]
    except Exception as exc:
        raise PromptParseError(f"召回核验输出 schema 校验失败: {exc}") from exc


# ======================================================================
# 回归测试样本（每套提示词 ≥3 个：happy path / 边界 / 拒答）
# zh-v2 起主用例为对象包裹 shape；每套保留 1 个旧裸数组用例验证兼容解包
# ======================================================================

REGRESSION_SAMPLES: dict[str, list[dict[str, Any]]] = {
    # faithfulness 第一步：陈述拆解
    "faithfulness_statements": [
        {
            "name": "多事实回答拆成原子陈述（对象包裹）",
            "raw": '{"statements": ["BinRag 使用 Go 语言实现。", "BinRag 的向量存储采用 Qdrant。", "BinRag 支持 PDF 文档。", "BinRag 支持 Markdown 文档。"]}',
            "expect": [
                "BinRag 使用 Go 语言实现。",
                "BinRag 的向量存储采用 Qdrant。",
                "BinRag 支持 PDF 文档。",
                "BinRag 支持 Markdown 文档。",
            ],
        },
        {
            "name": "拒答无事实内容输出空数组（对象包裹）",
            "raw": '{"statements": []}',
            "expect": [],
        },
        {
            "name": "客套话不产生陈述且容忍代码块包裹（旧裸数组兼容）",
            "raw": '```json\n["报销流程需要先提交申请。", "审批通过后在 3 个工作日内打款。"]\n```',
            "expect": ["报销流程需要先提交申请。", "审批通过后在 3 个工作日内打款。"],
        },
    ],
    # faithfulness 第二步：NLI 核验
    "faithfulness_nli": [
        {
            "name": "陈述与上下文矛盾判 0（对象包裹）",
            "raw": '{"statements": [{"statement": "BinRag 使用 Milvus 作为向量存储。", "reason": "上下文明确为 Qdrant，与陈述矛盾", "verdict": 0}]}',
            "expect_verdicts": [0],
        },
        {
            "name": "陈述与上下文一致判 1（旧裸数组兼容）",
            "raw": '[{"statement": "年假审批通过后 3 个工作日内打款。", "reason": "与上下文表述一致", "verdict": 1}]',
            "expect_verdicts": [1],
        },
        {
            "name": "verdict 必须是数字 0/1（字符串 true 判解析失败）",
            "raw": '{"statements": [{"statement": "x", "reason": "y", "verdict": "true"}]}',
            "expect_error": True,
        },
    ],
    # answer_relevancy：逆向问题生成
    "answer_relevancy": [
        {
            "name": "流程类回答还原口语化问题（noncommittal int）",
            "raw": '{"question": "报销的流程是怎样的，多久能到账？", "noncommittal": 0}',
            "expect_question": "报销的流程是怎样的，多久能到账？",
            "expect_noncommittal": 0,
        },
        {
            "name": "事实类回答还原具体问题",
            "raw": '{"question": "BinRag 用的向量数据库是什么，支持哪些距离度量？", "noncommittal": 0}',
            "expect_question": "BinRag 用的向量数据库是什么，支持哪些距离度量？",
            "expect_noncommittal": 0,
        },
        {
            "name": "拒答标记 noncommittal=1",
            "raw": '{"question": "", "noncommittal": 1}',
            "expect_question": "",
            "expect_noncommittal": 1,
        },
    ],
    # context_precision：上下文有用性判定（shape 不变）
    "context_precision": [
        {
            "name": "上下文直接支撑标准答案判 1",
            "raw": '{"reason": "上下文直接给出了余弦相似度这一答案", "verdict": 1}',
            "expect_verdict": 1,
        },
        {
            "name": "仅话题沾边判 0",
            "raw": '{"reason": "仅介绍 Qdrant，未涉及 BinRag 的默认度量配置", "verdict": 0}',
            "expect_verdict": 0,
        },
        {
            "name": "无关上下文判 0 且容忍解释性前缀",
            "raw": '判定结果如下：{"reason": "讲的是弹性工作制，与请假审批时长无关", "verdict": 0}',
            "expect_verdict": 0,
        },
    ],
    # context_recall：要点归因判定（字段名 statement，classifications 包裹）
    "context_recall": [
        {
            "name": "全部要点被支持（classifications 对象包裹）",
            "raw": '{"classifications": [{"statement": "审批通过后 3 个工作日内打款", "reason": "上下文明确支持", "attributed": 1}, {"statement": "打款到工资卡", "reason": "上下文明确支持", "attributed": 1}]}',
            "expect_attributed": [1, 1],
        },
        {
            "name": "部分要点缺失（召回不全）",
            "raw": '{"classifications": [{"statement": "支持 PDF", "reason": "上下文明确支持", "attributed": 1}, {"statement": "支持 Markdown", "reason": "上下文明确支持", "attributed": 1}, {"statement": "支持扫描件 OCR", "reason": "上下文未提及扫描件或 OCR", "attributed": 0}]}',
            "expect_attributed": [1, 1, 0],
        },
        {
            "name": "旧裸数组 + claim 字段名兼容（zh-v1 输出）",
            "raw": '[{"claim": "支持 PDF", "reason": "上下文明确支持", "attributed": 1}]',
            "expect_attributed": [1],
            "legacy_claim_key": True,
        },
        {
            "name": "attributed 越界值判解析失败",
            "raw": '{"classifications": [{"statement": "x", "reason": "y", "attributed": 2}]}',
            "expect_error": True,
        },
    ],
}


# ======================================================================
# RAGAS Prompt 实例覆写
# ======================================================================


def override_prompt_instruction(prompt_obj: Any, instruction: str) -> None:
    """把 ragas Prompt 实例的 instruction 整段替换为中文版本。

    采用鸭子类型：只要求对象暴露 instruction 属性（ragas 0.3 的
    PydanticPrompt 满足）。注意 ragas 0.3.9 的 to_string 不对 instruction
    做 str.format——输入以 JSON 形式追加在指令之后，因此中文模板
    不含 {占位符} 尾段，示例里的「输入」也采用 JSON 形态以与真实
    调用时的输入呈现一致。英文 few-shot examples 一并清空避免分布漂移。
    """
    if hasattr(prompt_obj, "instruction"):
        prompt_obj.instruction = instruction
    if hasattr(prompt_obj, "examples"):
        prompt_obj.examples = []


def apply_chinese_prompts(metrics: list[Any]) -> None:
    """对 ragas 指标实例就地覆写中文提示词（按指标类型分派）。

    属性名按 ragas 0.3 的指标实现（0.2 的旧名作为回退候选）；
    版本升级后若属性名变更，只需在此函数适配（runner 是唯一调用方）。
    """

    def _first_attr(obj: Any, names: tuple[str, ...]) -> Any:
        for n in names:
            if getattr(obj, n, None) is not None:
                return getattr(obj, n)
        return None

    for metric in metrics:
        cls_name = type(metric).__name__
        if cls_name.startswith("Faithfulness"):
            # 两步：陈述拆解 + NLI 核验（0.3: statement_generator_prompt /
            # nli_statements_prompt；0.2: statement_prompt / nli_prompt）
            p = _first_attr(metric, ("statement_generator_prompt", "statement_prompt"))
            if p is not None:
                override_prompt_instruction(p, FAITHFULNESS_STATEMENT_PROMPT)
            p = _first_attr(metric, ("nli_statements_prompt", "nli_prompt"))
            if p is not None:
                override_prompt_instruction(p, FAITHFULNESS_NLI_PROMPT)
        elif cls_name.startswith("AnswerRelevancy") or cls_name.startswith("ResponseRelevancy"):
            p = _first_attr(metric, ("response_relevance_prompt",))
            if p is not None:
                override_prompt_instruction(p, ANSWER_RELEVANCY_PROMPT)
        elif cls_name.startswith("LLMContextPrecision"):
            p = _first_attr(metric, ("context_precision_prompt",))
            if p is not None:
                override_prompt_instruction(p, CONTEXT_PRECISION_PROMPT)
        elif cls_name.startswith("ContextRecall") or cls_name.startswith("LLMContextRecall"):
            p = _first_attr(metric, ("context_recall_prompt",))
            if p is not None:
                override_prompt_instruction(p, CONTEXT_RECALL_PROMPT)
