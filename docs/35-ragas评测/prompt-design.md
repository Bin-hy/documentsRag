# RAGAS 评测提示词与评审模型策略设计

> 文档版本：v1（2026-02）
> 关联模块：`internal/eval/`（现有 LLM-as-Judge）、`internal/llm/`（OpenAI 兼容客户端）、`internal/config/`（LLMConfig / EmbedderConfig）
> 目标读者：评测微服务（Python / uv / FastAPI）与 Go 后端两侧的实现者

---

## 0. 背景与总体架构

BinRag 现有 `internal/eval` 已内置一套 LLM-as-Judge（准确性 0-10 打分 + 忠实度二值判定，见 `internal/eval/judge.go`），采用全中文提示词、`temperature=0`、JSON 提取容错的实现方式，实测稳定。引入 RAGAS 后：

- **评测微服务**（独立 Python 服务）通过 HTTP 调用 Go 后端的问答/检索接口，收集 `question / answer / contexts`，再交给 RAGAS 计算四个标准指标。
- **RAGAS 默认提示词全部为英文**，直接用于中文语料会出现判定口径漂移（详见 §1）。因此本设计的核心是：**沿用 `judge.go` 已验证的中文化经验，为 RAGAS 四指标提供完整的中文 Prompt 适配方案**。
- 评审模型与 Embedding 均复用项目已有的 OpenAI 兼容配置，不引入新的供应商依赖。

```
┌─────────────┐  HTTP   ┌──────────────┐
│ 评测微服务    │ ──────▶ │  Go 后端      │
│ (uv+FastAPI)│ ◀────── │ /api/ask 等  │
│             │  Q/A+来源 │              │
│  ┌───────┐  │          └──────────────┘
│  │ RAGAS │──┼─▶ Judge LLM（OpenAI 兼容，temp=0）
│  │ 中文Prompt│─▶ Embedding（复用 embedder 配置）
│  └───────┘  │
└─────────────┘
```

---

## 1. RAGAS 四指标机制与中文化方案

### 1.1 Faithfulness（忠实度）

**内部机制（两步提示词）**

1. **陈述拆解（Statement Extraction）**：默认英文提示词 `LongFormAnswerPrompt`，要求模型把回答拆成原子陈述列表，例如 `["爱因斯坦出生于德国。", "他出生于1879年3月14日。"]`。
2. **逐条核验（NLI 判定）**：默认英文提示词 `NLIStatementPrompt`，给定检索上下文，对每条陈述输出 ` verdict: 1/0` 加理由。

最终分数 = 被支持的陈述数 / 总陈述数。

**中文语料的适配风险**

| 风险 | 说明 |
|---|---|
| 陈述拆解粒度失控 | 英文提示词的 few-shot 示例是英文句式，模型对中文长句可能拆不出原子陈述，或把整段当作一条陈述 → 分数失真（通常偏高） |
| NLI 判定口径漂移 | 英文提示词中 "verdict" 标签与中英文混排的输入组合时，部分国产模型（豆包、DeepSeek 非 thinking 模式）会输出中文"是/否"而非 `1/0`，导致解析失败 |
| 推理串扰 | 核验提示词要求 "Final verdict in order"，中文输入下模型容易把上下文内容误当作陈述内容 |

**中文化方案**

RAGAS ≥ 0.2 的指标暴露 `Prompt` 对象（`ragas.prompt.PydanticPrompt`），支持整段替换 `instruction` 与 `examples`。我们直接覆写两个提示词（不使用官方 `adapt_prompts` 自动翻译——自动翻译质量不可控，且示例仍是英文分布）。

**陈述拆解中文提示词草案（statement extraction）**

```text
## 角色
你是陈述拆解器。你的唯一任务：把一段中文回答拆成「原子陈述」——每条只包含一个事实点。

## 约束
- 输出格式：JSON 数组，元素为字符串，如 ["陈述1", "陈述2"]。不输出任何其他文字。
- 保留回答的原意，不补充、不推断。
- 无事实内容的客套话（如"希望对你有帮助"）不产生陈述。
- 若回答为空或不含任何事实，输出 []。

## 示例
<example id="1">
回答：BinRag 使用 Go 语言实现，向量存储采用 Qdrant，支持 PDF 与 Markdown 文档。
输出：["BinRag 使用 Go 语言实现。", "BinRag 的向量存储采用 Qdrant。", "BinRag 支持 PDF 文档。", "BinRag 支持 Markdown 文档。"]
</example>

<example id="2">
回答：抱歉，知识库中没有找到相关信息。
输出：[]
</example>

<example id="3">
回答：报销流程需要先提交申请，审批通过后在 3 个工作日内打款。希望对你有帮助！
输出：["报销流程需要先提交申请。", "审批通过后在 3 个工作日内打款。"]
</example>
```

**NLI 核验中文提示词草案**

```text
## 角色
你是忠实度核验员。给定「上下文」和若干「陈述」，逐条判断该陈述能否被上下文直接支持。

## 判定标准
- verdict=1：陈述与上下文一致，或可由上下文直接推出（不允许依赖外部知识）。
- verdict=0：上下文没有提及，或与上下文矛盾。

## 约束
- 输出格式：JSON 数组，与输入陈述一一对应：
  [{"statement": "...", "reason": "一句话理由", "verdict": 0或1}]
- verdict 必须是数字 0 或 1，不允许输出"是/否"、"true/false"。
- reason 用中文，不超过 30 字。

## 示例
<example id="1">
上下文：BinRag 的向量存储使用 Qdrant，距离度量默认为余弦相似度。
陈述：["BinRag 使用 Milvus 作为向量存储。"]
输出：[{"statement": "BinRag 使用 Milvus 作为向量存储。", "reason": "上下文明确为 Qdrant，与陈述矛盾", "verdict": 0}]
</example>

<example id="2">
上下文：年假审批通过后，系统会在 3 个工作日内自动打款补偿。
陈述：["年假审批通过后 3 个工作日内打款。"]
输出：[{"statement": "年假审批通过后 3 个工作日内打款。", "reason": "与上下文表述一致", "verdict": 1}]
</example>
```

> 与 `judge.go` 现有忠实度提示词的差异：RAGAS 版是逐条陈述核验并输出理由（可解释），现有版本只输出整体 `true/false`。两套可并存：RAGAS 用于离线评测，现有轻量版用于在线抽检。

### 1.2 Answer Relevancy（答案相关性）

**内部机制**：默认提示词 `ResponseRelevancyPrompt`——让模型**根据回答逆向生成 N 个问题**（默认 `n=3`，非严格模式），再用 embedding 计算「原问题」与「生成问题」的余弦相似度均值。严格模式（`strictness>1`）会对违反承诺（如回答"无法回答"但原题可答）的样本打 0 分。

**中文语料的适配风险**

| 风险 | 说明 |
|---|---|
| 逆向生成的问题句式偏英文 | 生成的问题带翻译腔，与原问题（用户口语化中文提问）分布不一致 → embedding 相似度系统性偏低，指标偏悲观 |
| 对 embedding 模型敏感 | 该指标的分值完全由 embedding 模型决定，中文 embedding 质量差时噪声大（见 §3） |

**中文化方案（逆向问题生成提示词草案）**

```text
## 角色
你是问题还原器。给定一段中文回答，生成 1 个「该回答最可能在回答什么问题」的中文问题。

## 约束
- 输出格式：JSON 对象 {"question": "..."}，不输出其他内容。
- 问题必须是自然的中文疑问句，像真实用户会提的问题（口语化优先，不要书面翻译腔）。
- 问题必须是具体可回答的，不要生成"什么是……"式的宽泛问题，除非回答本身就在下定义。
- 若回答是"知识库中没有相关信息"之类的拒答，输出 {"question": "", "noncommittal": true}。

## 示例
<example id="1">
回答：报销需要先在 OA 系统提交申请单，部门主管审批通过后，财务会在 3 个工作日内打款到工资卡。
输出：{"question": "报销的流程是怎样的，多久能到账？"}
</example>

<example id="2">
回答：Qdrant 是 BinRag 默认的向量数据库，支持余弦、点积和欧氏距离三种度量。
输出：{"question": "BinRag 用的向量数据库是什么，支持哪些距离度量？"}
</example>

<example id="3">
回答：抱歉，知识库中没有关于该主题的信息。
输出：{"question": "", "noncommittal": true}
</example>
```

配置建议：保持 `n=3`（生成 3 次取均值），`strictness=1` 起步，避免中文拒答样本被误伤；待数据积累后再评估是否开启严格模式。

### 1.3 Context Precision（上下文精确率）

**内部机制**：默认提示词 `ContextPrecisionPrompt`（LLM 模式）——对每个检索到的上下文，让模型判断「该上下文是否与得出标准答案（reference）相关」，输出 `verdict: 1/0`。然后计算带位置权重的 Average Precision：相关上下文排得越靠前，分数越高。

**中文语料的适配风险**

- 需要 `reference`（标准答案）；无 reference 时需走 LLM-free 变体（IDF-based）或降级（见 §4.3）。
- 英文 few-shot 示例的判定口径是"contains the answer"，中文场景下模型容易放宽为"话题相关" → 精确率虚高。必须在提示词中把判定标准钉死为「**对得出标准答案有用**」而非「与问题相关」。

**中文化方案（判定提示词草案）**

```text
## 角色
你是检索质量评审员。给定「问题」「标准答案」和一条「检索上下文」，判断该上下文是否对得出标准答案有用。

## 判定标准
- verdict=1：上下文包含了得出标准答案所需的关键事实，或提供了实质性支撑。
- verdict=0：上下文只是话题沾边、内容重复、或与标准答案无关。
- 注意：与问题话题相关 ≠ 有用。必须能支撑标准答案才算 1。

## 约束
- 输出格式：JSON 对象 {"reason": "一句话中文理由", "verdict": 0或1}，不输出其他内容。
- verdict 必须是数字 0 或 1。

## 示例
<example id="1">
问题：BinRag 默认的向量距离度量是什么？
标准答案：默认使用余弦相似度。
上下文：BinRag 的向量存储使用 Qdrant，距离度量默认为余弦相似度。
输出：{"reason": "上下文直接给出了余弦相似度这一答案", "verdict": 1}
</example>

<example id="2">
问题：BinRag 默认的向量距离度量是什么？
标准答案：默认使用余弦相似度。
上下文：Qdrant 是一个开源向量数据库，用 Rust 编写，支持 HNSW 索引。
输出：{"reason": "仅介绍 Qdrant，未涉及 BinRag 的默认度量配置", "verdict": 0}
</example>

<example id="3">
问题：请假审批要多久？
标准答案：部门主管审批 1 个工作日，HR 备案当天完成。
上下文：公司实行弹性工作制，上下班时间可在 8:00-10:00 之间灵活选择。
输出：{"reason": "讲的是弹性工作制，与请假审批时长无关", "verdict": 0}
</example>
```

### 1.4 Context Recall（上下文召回率）

**内部机制**：默认提示词 `ContextRecallPrompt`——把标准答案拆成原子陈述，逐条判断该陈述「能否由全部检索上下文支持」，分数 = 被支持的标准答案陈述数 / 标准答案陈述总数。衡量"该检索到的有没有检索到"。

**中文语料的适配风险**

- 同样需要 `reference`；缺失时只能降级（§4.3）。
- 风险同 faithfulness 的拆解/核验两步：中文标准答案如果写得长而复杂，拆解粒度直接影响分母 → **数据集编写规范要求 reference 为短句要点式**（§4.4）。

**中文化方案（提示词草案）**

```text
## 角色
你是召回核验员。给定「问题」「标准答案」和「全部检索上下文」，把标准答案拆成原子要点，逐条判断该要点能否被检索上下文支持。

## 约束
- 输出格式：JSON 数组，按要点顺序输出：
  [{"claim": "标准答案要点", "reason": "一句话中文理由", "attributed": 0或1}]
- attributed=1 表示要点能被上下文直接支持；0 表示上下文中没有依据。
- attributed 必须是数字 0 或 1。
- 只依据检索上下文判断，不使用任何外部知识。

## 示例
<example id="1">
问题：年假补偿的到账规则是什么？
标准答案：审批通过后 3 个工作日内打款，打款到工资卡。
上下文：年假审批通过后，系统会在 3 个工作日内自动打款补偿至员工工资卡。
输出：[
  {"claim": "审批通过后 3 个工作日内打款", "reason": "上下文明确支持", "attributed": 1},
  {"claim": "打款到工资卡", "reason": "上下文明确支持", "attributed": 1}
]
</example>

<example id="2">
问题：BinRag 支持哪些文档格式？
标准答案：支持 PDF、Markdown 和扫描件 OCR。
上下文：BinRag 支持 PDF 与 Markdown 文档的上传与解析。
输出：[
  {"claim": "支持 PDF", "reason": "上下文明确支持", "attributed": 1},
  {"claim": "支持 Markdown", "reason": "上下文明确支持", "attributed": 1},
  {"claim": "支持扫描件 OCR", "reason": "上下文未提及扫描件或 OCR", "attributed": 0}
]
</example>
```

### 1.5 中文化落地方式汇总

| 方式 | 适用 | 本项目选择 |
|---|---|---|
| `adapt_prompts(language="chinese")` 自动翻译 | 快速验证 | ❌ 仅作为 baseline 对照，不上生产 |
| 覆写指标的 `Prompt` 实例（自定义 instruction + 中文 few-shot） | 生产使用 | ✅ 全部四指标 |
| 评测微服务内集中管理提示词文件 | 版本化 | ✅ `prompts/*.md`，带 changelog，随微服务发版 |

**提示词治理要求**（沿用 `judge.go` 的既有约定）：

- 所有中文提示词**以文件形式存放于评测微服务仓库**，禁止硬编码进 Python 源码；文件头部带版本注释（`v1` 起，变更记 changelog）。
- 每个提示词必须配 ≥ 3 个回归测试样本（happy path / 边界 / 拒答），提示词或 judge 模型变更时全量回归。
- 输出一律强制「只输出 JSON」+ 微服务侧做 JSON 提取容错（提取首个 `{` 至末个 `}`，与 `generateJSON` 同款策略）。

---

## 2. 评审 LLM（Judge Model）配置策略

### 2.1 接入方式：复用 OpenAI 兼容配置

项目 `internal/llm/llm.go` 的客户端走标准 OpenAI 兼容 `/v1/chat/completions`，配置结构为 `LLMConfig{BaseURL, APIKey, Model, Temperature, MaxTokens, MaxRetries, QPS, Timeout}`（`internal/config/config.go`）。

评测微服务侧用 RAGAS 的 LangChain 包装器接入同一端点：

```python
# 配置示意（不写实现，仅说明映射关系）
from langchain_openai import ChatOpenAI
from ragas.llms import LangchainLLMWrapper

judge_llm = LangchainLLMWrapper(ChatOpenAI(
    base_url=cfg.llm.base_url,      # ← 复用 configs 中 llm.base_url
    api_key=cfg.llm.api_key,
    model=cfg.eval.judge_model,      # 评测专用模型，可与问答模型不同
    temperature=0,                   # 强制确定性
    max_tokens=2048,
    timeout=cfg.llm.timeout,
    max_retries=0,                   # 重试由微服务统一控制（见 §2.4），禁用底层隐式重试
))
```

**配置来源策略**：微服务增加 `eval` 配置段（`judge_model`、`judge_temperature`、`embedding_model` 等），默认值从 Go 后端的 `configs/config.yaml` 对齐；推荐 judge 模型显式指定而非复用问答模型——**评审模型与生成模型分离**可避免同源偏差（模型倾向给自己的输出打高分）。

### 2.2 确定性设置

| 参数 | 值 | 理由 |
|---|---|---|
| `temperature` | **0** | 与 `judge.go` 的 `zeroTemp` 一致；评分任务要求可复现 |
| `seed` | 固定值（若供应商支持，如 vLLM/DeepSeek） | 进一步消除抖动 |
| `top_p` | 不显式设置 | 与 temperature=0 叠加意义不大，部分国产模型同时设置会告警 |
| thinking 模式 | **关闭** | DeepSeek 等模型的思维链会显著拉长评测耗时且对 NLI 判定无收益；若必须开，需处理 `reasoning_content` 回传问题（`llm.go` 已有此坑的记录） |

**回归验证**：同一批 10 个样本连跑 3 次，四指标各样本分值应完全一致（temperature=0 下仍不一致则说明供应商端有采样，需换 seed 或换模型）。

### 2.3 结构化输出约束

分层降级（按供应商能力）：

1. **首选**：`response_format={"type": "json_object"}`（OpenAI/DeepSeek/vLLM 均支持）。注意：部分供应商要求消息文本中必须出现 "json" 字样——中文提示词中保留 "JSON" 字样即满足。
2. **次选**：提示词强约束「只输出 JSON，不要输出其他内容」（§1 所有草案均已包含）+ 微服务侧 JSON 提取容错（首个 `{` 至末个 `}`）。
3. **兜底**：`json-repair` 库修复尾部截断的 JSON（`max_tokens` 不足时的常见故障）。

RAGAS 的 `PydanticPrompt` 自带输出 schema 校验（pydantic model），解析失败会抛 `OutputParserException`——微服务需捕获并计入重试（§2.4）。

### 2.4 评分失败重试

**失败分类与策略**：

| 失败类型 | 检测方式 | 策略 |
|---|---|---|
| 网络/限流（429/5xx） | HTTP 状态码 | 指数退避重试，1s/2s/4s，上限 3 次（对齐 `llm.go` 的 `MaxRetries` 与退避公式） |
| JSON 解析失败 | `OutputParserException` | 原样重试 ≤ 2 次；仍失败则该样本该指标记 `NaN` 并记录原始输出（**不允许静默丢弃**，见 §5.4） |
| 判定输出格式合法但语义异常（如 verdict 出现 0/1 之外的值） | schema 校验 | 同解析失败处理 |
| 上下文超长 | 输入 token 估算 | 截断 `retrieved_contexts`（保留排名靠前的），记录 `truncated=true` 标记 |

**限流**：微服务侧按 `LLMConfig.QPS` 做全局限速（对齐 Go 侧 `rate.NewLimiter` 语义），避免评测压垮生产 LLM 配额。RAGAS 的 `RunConfig(max_workers=…)` 控制在 QPS 允许的范围内，建议 `max_workers=2` 起步。

---

## 3. Embedding 模型策略

### 3.1 哪些指标需要 Embedding

| 指标 | 需要 Embedding | 用途 |
|---|---|---|
| Faithfulness | ❌ | 纯 LLM 判定 |
| Answer Relevancy | ✅ **必需** | 原问题与逆生成问题的余弦相似度 |
| Context Precision | 可选 | LLM 模式不需要；LLM-free 模式需要 |
| Context Recall | ❌（默认） | LLM 模式不需要 |

### 3.2 复用项目 Embedding 配置

项目 `EmbedderConfig{Provider, BaseURL, APIKey, Model, Dimension, BatchSize, MaxRetries, QPS}` 同样是 OpenAI 兼容（`/v1/embeddings`）。微服务侧映射：

```python
from langchain_openai import OpenAIEmbeddings
from ragas.embeddings import LangchainEmbeddingsWrapper

embeddings = LangchainEmbeddingsWrapper(OpenAIEmbeddings(
    base_url=cfg.embedder.base_url,   # ← 复用 embedder.base_url
    api_key=cfg.embedder.api_key,
    model=cfg.embedder.model,          # 与索引进库所用模型保持一致
))
```

### 3.3 关键原则：评测 Embedding 必须与索引 Embedding 同源

**Answer Relevancy 的相似度在「评测 embedding 空间」中计算，而非索引空间**。但仍要求评测 embedding 与索引 embedding 使用**同一模型**，理由：

1. 若用不同模型，指标反映的语义相似度与系统实际检索行为脱节，定位问题时（§5）会产生误导。
2. 中文场景下 embedding 模型对相似度绝对值的影响极大（同一模型下两问相似度 0.75 可能对应另一模型的 0.55），**阈值标定必须在固定 embedding 模型下进行，换模型必须重新标定阈值**。
3. `BatchSize` 沿用 `EmbedderConfig.BatchSize`，避免超过供应商单批上限。

---

## 4. 评测数据集设计

### 4.1 SingleTurnSample 字段映射

```python
SingleTurnSample(
    user_input=question,          # 用户问题
    response=answer,              # Go 后端问答接口返回的回答
    retrieved_contexts=[...],     # 检索到的上下文文本列表（按排名顺序）
    reference=standard_answer,    # 标准答案（可缺失，见 §4.3）
)
```

**从 Go 后端响应到 RAGAS 的映射**：

| RAGAS 字段 | 来源 | 说明 |
|---|---|---|
| `user_input` | 数据集 `question` | 复用现有 `EvalSample.Question`（`internal/eval/dataset.go`），数据集格式不变 |
| `response` | 问答接口返回的 `answer` | 对应 `rag.RAGResult.Answer` |
| `retrieved_contexts` | 检索片段的**正文内容** | ⚠️ 见 §4.2 的缺口说明 |
| `reference` | 数据集 `answer` | 对应 `EvalSample.Answer`（现有字段，可空） |

现有 `EvalSample` 还有 `expected_ids`（Recall@K 用）与 `kb_id`，在 RAGAS 侧可作为 `sample.metadata` 保留，`kb_id` 在采集阶段传给问答接口。

### 4.2 ⚠️ 已知数据缺口：`Source` 不含正文

当前 `rag.Source` 结构（`internal/rag/context.go`）只有 `id / filename / heading / score / page_number` 等元数据，**不含片段正文**；`judge.go` 的忠实度判定也因此只传了文件名列表（这是一个已知弱化——只凭文件名判忠实度偏松）。

RAGAS 的 faithfulness / context_precision / context_recall 都必须拿到**上下文正文**，因此 Go 侧需要二选一：

- **方案 A（推荐）**：问答接口增加评测专用参数（如 `?include_contexts=true`），在响应中附带 `contexts: [{id, filename, heading, content}]`。对现有 `Source` 增加可选 `content` 字段（`json:"content,omitempty"`），正常问答路径不填充，不影响线上性能。
- **方案 B（零改动 Go 侧）**：微服务拿 `question` 再调一次检索接口（search/retrieve API），以相同 TopK 取回正文。缺点：与问答实际使用的上下文可能不一致（rerank/截断/token 预算在问答链路内部发生），评测的是"检索器输出"而非"生成器实际看到的上下文"。

**结论：采用方案 A**，并在报告中记录 `contexts_source=ask_api`，保证评测所见即生成所用。

### 4.3 reference 缺失时的指标降级

| 指标 | 需要 reference | 缺失时 |
|---|---|---|
| Faithfulness | ❌ | ✅ 正常计算 |
| Answer Relevancy | ❌ | ✅ 正常计算 |
| Context Precision | ✅（LLM 模式） | 降级：用 response 代替 reference 的变体（`LLMContextPrecisionWithoutReference`），语义变为"上下文对得出**该回答**是否有用"——注意此时该指标与 faithfulness 相关，不能独立反映检索质量，报告中需标注降级 |
| Context Recall | ✅ | **无法降级**，该样本跳过并记 `NaN` |

**降级策略总表**（微服务侧逐样本分派）：

```
reference 存在 → 跑全部四指标
reference 缺失 → faithfulness + answer_relevancy + context_precision(无参考变体)
                 context_recall 记 NaN，报告中标 "N/A（缺标准答案）"
```

报告层面：对每个指标输出 `coverage = 有效样本数 / 总样本数`，coverage < 50% 的指标在结论中降权呈现。

### 4.4 数据集编写规范（配合提示词的前提）

- `reference` 采用**短句要点式**（如"审批通过后 3 个工作日内打款，打款到工资卡"），避免长段落——§1.4 的召回核验以要点为分母，长段落拆解粒度不可控。
- 每条样本至少 1 个人工标注的 reference；无 reference 的样本应来自真实用户问题日志，作为「无监督补充集」单独标记。
- 建议规模：首发评测集 ≥ 50 条（30 条带 reference + 20 条真实无 reference），覆盖各知识库与各类问题类型（事实查询 / 流程操作 / 拒答场景 ≥ 5 条）。

---

## 5. 指标结果解读与阈值建议

### 5.1 健康区间（中文语料、judge=强模型、temperature=0 下的经验起点值）

| 指标 | 优秀 | 可接受 | 需排查 | 说明 |
|---|---|---|---|---|
| Faithfulness | ≥ 0.90 | 0.80–0.90 | < 0.80 | 幻觉红线，低于 0.80 不可上线 |
| Answer Relevancy | ≥ 0.85 | 0.75–0.85 | < 0.75 | 受 embedding 模型影响大，首次运行先标定基线而非直接套阈值 |
| Context Precision | ≥ 0.80 | 0.65–0.80 | < 0.65 | 低分通常指向检索噪声/排序问题 |
| Context Recall | ≥ 0.80 | 0.65–0.80 | < 0.65 | 低分通常指向漏检（分块、TopK、索引覆盖） |

> 以上阈值为**起点值**，不是绝对标准。正确姿势：首轮跑 50 条标定基线，阈值设为「基线均值 − 0.1」作为回归告警线，后续随优化逐步收紧。

### 5.2 定位问题：检索 vs 生成 决策矩阵

| Context Recall | Faithfulness | 诊断 | 排查方向 |
|---|---|---|---|
| 低 | 低 | **检索问题为主**：该检的没检到，模型只能编 | 检查分块策略、embedding 召回、TopK、知识库覆盖度 |
| 低 | 高 | 检索不全，但模型老实（敢说"不知道"） | 同上，且说明拒答提示词工作正常 |
| 高 | 低 | **生成问题为主**：上下文够用但回答编造/曲解 | 检查问答系统提示词、模型选择、上下文注入模板（`buildContext` 的 token 预算是否截断关键内容） |
| 高 | 高 | 链路健康 | 看 Answer Relevancy 与 Context Precision 做精修 |

辅助判据：

- **Context Precision 低 + Recall 高** → 检索"宁可错杀不可放过"，噪声多 → 开 reranker、调低 TopK、提高相似度阈值。
- **Context Precision 高 + Recall 低** → 检索太保守 → 增大 TopK、检查分块粒度过大导致关键片段被稀释。
- **Answer Relevancy 低但 Faithfulness 高** → 回答忠实但答非所问 → 检查问题改写（multi-query/decomposition 链路是否改丢原意）、路由是否选错知识库。

### 5.3 单样本下钻报告

微服务输出两级报告：

1. **汇总级**：四指标均值/中位数/P10 + coverage + 与上次的 diff（回归告警）。
2. **样本级**：每个低分样本（任一指标 < 0.6）输出完整材料——question / contexts（带 verdict）/ response / 各判定 reason。**判定理由必须入报告**，这是中文自定义提示词要求输出 `reason` 字段的核心价值：没有理由的低分样本无法复盘。

### 5.4 失败样本的统计口径

- JSON 解析重试后仍失败的样本，该指标记 `NaN` 并从均值中剔除，但**计入 `parse_failure_rate`** 指标单独上报。
- `parse_failure_rate > 5%` 视为提示词/模型故障（而非样本问题），阻塞本次评测结论，触发提示词回归排查。

---

## 6. 合成数据集生成提示词（可选增强）

当人工标注 reference 成本过高时，基于知识库已有文档块自动生成 question/reference 对。生成后**必须人工抽检 ≥ 20%** 才能入正式评测集。

**单文档块 QA 生成提示词草案**

```text
## 角色
你是评测数据集构造器。给定一段中文知识库文档片段，生成 1-3 个「该片段能够回答」的问答对。

## 约束
- 输出格式：JSON 数组：
  [{"question": "...", "reference": "...", "type": "事实查询|流程操作|条件判断"}]
- question：像真实用户会提的问题，口语化，不得在问题中直接复述文档原文的大段表述。
- reference：短句要点式（1-3 个要点），必须完全可由给定片段支持，不得引入片段外知识。
- 若片段是目录、页眉页脚、无信息量的碎片，输出 []。
- 三种 type 尽量均匀分布；不要三个问题问同一件事的不同说法。

## 示例
<example id="1">
片段：员工报销需先在 OA 系统提交申请单，部门主管 1 个工作日内完成审批，审批通过后财务在 3 个工作日内打款至工资卡。差旅报销需额外附发票原件。
输出：[
  {"question": "报销的钱多久能到账？", "reference": "部门主管 1 个工作日内审批，通过后财务 3 个工作日内打款至工资卡。", "type": "流程操作"},
  {"question": "差旅报销和普通报销有什么不一样？", "reference": "差旅报销需额外附发票原件。", "type": "条件判断"},
  {"question": "报销申请在哪个系统提交？", "reference": "在 OA 系统提交申请单。", "type": "事实查询"}
]
</example>

<example id="2">
片段：第 3 章 配置详解 .......... 15
输出：[]
</example>
```

**生成参数**：temperature=0.7（需要多样性，与评分场景相反）、每个片段生成 1 次；生成后用规则过滤（question 与片段的 embedding 相似度 > 0.5、reference 非空、type 合法），再进人工抽检。

**防泄漏注意**：合成数据集的 question 来自文档块，直接评测会系统性高估 Recall（问题与原文词面重合）。缓解：入集时记录 `source_chunk_id`，报告单独分组呈现合成集与真实集的指标，不混合计算总体均值。

---

## 7. 附：与现有 `internal/eval` 的关系

| 维度 | 现有 `internal/eval` | RAGAS 微服务 |
|---|---|---|
| 定位 | 在线/准在线轻量抽检、CI 冒烟 | 离线全量深度评测 |
| 指标 | Recall@K、准确性(0-10)、忠实度(二值) | 四指标（细粒度、带理由） |
| 数据要求 | `expected_ids` 标注成本高 | 仅 reference 即可跑三指标，缺失也能跑两指标 |
| 共存策略 | 保留不动 | 新增；两框架的忠实度口径不同（整体 vs 逐条陈述），报告不直接互比 |

评测微服务的提示词文件建议目录（示意）：

```
eval-service/
  prompts/
    faithfulness_statements.zh.md   # v1
    faithfulness_nli.zh.md          # v1
    answer_relevancy.zh.md          # v1
    context_precision.zh.md         # v1
    context_recall.zh.md            # v1
    synth_qa_gen.zh.md              # v1
    CHANGELOG.md
```
