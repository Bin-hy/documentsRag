# RAG 优化计划

> 本文档是 BinRag 项目检索与生成质量优化**计划**（非实现）。每个优化点按依赖与优先级分阶段，后续**逐个通过 `/mew-spec` 流程**实现（spec → plan → task → checklist）。
> 参考输入：`docs/phase3/docs-rag/`（学习笔记.md / 落地化的RAG系统优化策略.md / 项目设计.md / Rag的13种分块策略.md）。

## 背景

BinRag 已具备完整基础管线：多格式文档解析、递归分块、混合检索（向量 + BM25 + RRF）、Cross-Encoder 重排序、Query 改写（单查询）、SSE 流式问答、评估框架（Recall@K / LLM-as-Judge）。基础功能完成，但**检索与生成质量仍停留在「基础可用」**：

- 查询理解仅单次改写，未覆盖多视角召回
- 复杂复合问题不做分解，检索深度不足
- 所有查询走同一路径，无复杂度感知与策略路由
- 无假设文档（HyDE）、回退（Step-Back）等检索前增强

## 现状盘点

| 能力 | 现状 | 位置 |
|------|------|------|
| Query 改写（单查询） | ✅ 已有 | `internal/rag/engine.go` rewriteQuery |
| 混合检索（向量+BM25+RRF） | ✅ 已有 | `internal/retriever/` FuseRRF |
| Cross-Encoder 重排序 | ✅ 已有 | `internal/reranker/` |
| 评估框架（Recall@K/LLM-as-Judge） | ✅ 已有 | `internal/eval/` cmd/eval |
| SSE 流式问答 | ✅ 已有 | `internal/api/handler_chat.go` |
| **Multi-Query 多查询** | ❌ 缺失 | 待新增 |
| **RAG-Fusion（RRF 融合多路）** | ❌ 缺失 | 复用 FuseRRF 扩展 |
| **Decomposition 问题分解** | ❌ 缺失 | 待新增 |
| **Step-Back 回退查询** | ❌ 缺失 | 待新增 |
| **HyDE 假设文档嵌入** | ❌ 缺失 | 待新增 |
| **RAG 路由（复杂度分流）** | ❌ 缺失 | 待新增 |

## 目标架构

```
用户查询
  → Router（复杂度分析：simple / medium / complex + 策略选择 + fallback）
      ├─ simple  → 直接检索（现有路径，低延迟）
      ├─ medium  → Multi-Query 多视角检索 → RAG-Fusion(RRF) 融合
      └─ complex → Decomposition 子问题分解 → 各子问题检索 → 综合回答
  可选增强：Step-Back（抽象回退）/ HyDE（假设文档嵌入）
  → 混合检索（向量+BM25+RRF，现有）→ 重排序（现有）→ 生成（现有）
```

架构原则：
- **Router 是入口决策层**，策略是内层可选增强——策略先行实现，路由最后编排
- 所有策略复用现有 `retriever` / `FuseRRF` / `reranker` / `llm`，不重复实现
- 每个策略**可独立开关**（配置驱动），便于 A/B 对比
- 效果验证统一走 `cmd/eval`（Recall@K 与 LLM 指标），对比基线

---

## 阶段一：Multi-Query 多查询 + RAG-Fusion 融合（P0）

### Multi-Query 多查询

**目标**：一个问题生成多个视角的查询变体，并行检索提升召回率，覆盖用户问题的不同表达。

**方案**：
- 扩展现有 `rewriteQuery`：新增 `multiQuery(ctx, question, n)` —— LLM 输出 N 个查询变体（JSON 数组，固定低温度）
- 变体与原文构成 N+1 路查询，并行 `retriever.Search`
- 结果去重（按 chunk ID）合并

**复用**：LLM 客户端（现有）、retriever（现有）、模板机制（prompt.go 模板模式）

### RAG-Fusion（RRF 融合）

**目标**：多路查询结果科学融合排序，而非简单拼接——让跨查询重复命中的文档靠前。

**方案**：
- 复用现有 `FuseRRF(vectorResults, bm25Results, ...)` 的思路，扩展为**多路查询级 RRF**：每路查询的检索结果作为一个「排名列表」，按 `1/(K+rank)` 累计分数
- 融合后取 Top-K，再走现有重排序

**依赖**：阶段一 Multi-Query 提供多路结果；`retriever` 需暴露多路融合入口或新增 `SearchMulti`

**验证**：`cmd/eval` 对比基线（单查询 vs Multi-Query+RAG-Fusion）的 Recall@1/3/5；预期 Recall 提升 10%+（构造含同义表达的数据集）

**配置**：`rag.multi_query_enabled`、`rag.multi_query_count`（默认 3）、`rag.rrf_k`

---

## 阶段二：Decomposition 问题分解 + Step-Back 回退（P1）

### Decomposition 问题分解

**目标**：复杂复合问题拆成多个子问题，逐个检索、综合回答，避免单次检索遗漏关键信息。

**方案**：
- 前置判定：LLM 判断问题是否需要分解（simple 事实查询/定义类 → 跳过）
- 分解：输出子问题列表（支持**并行**与**顺序**两种模式——顺序模式子问题答案作为后续输入）
- 执行：每个子问题走阶段一的检索路径（Multi-Query 可选叠加）
- 综合：LLM 结合所有子问题答案与原始问题生成最终回答

**权衡**（文档标注）：子问题综合新增 1 轮 LLM 生成，成本与延迟增加（约 +1 次 LLM 调用）；仅在判定为复杂时启用

**依赖**：阶段一多路检索基建；`rag.Engine` 需支持多轮检索（子问题 → 答案 → 综合）

**验证**：构造复合问题评估集（含对比分析/多步骤类问题），`cmd/eval` 扩展「综合回答质量」指标（LLM-as-Judge 对比直接 RAG vs 分解 RAG）

**配置**：`rag.decomposition_enabled`、`rag.decomposition_mode`（parallel/sequential）、`rag.decomposition_max_sub`（默认 5）

### Step-Back 回退查询

**目标**：对需要高层概念/多步推理的问题，先生成抽象回退问题检索广泛上下文，再结合原问题精答。

**方案**：
- 判定：仅对 medium/complex 类问题启用（与 Decomposition 互斥或可叠加，默认互斥）
- 生成回退问题（抽象化 prompt）→ 用回退问题检索 → 结合回退上下文与原问题检索结果共同生成

**依赖**：LLM + retriever；独立模板 `stepback`

**验证**：时间序列/趋势类问题评估集，对比直接 RAG

**配置**：`rag.step_back_enabled`

---

## 阶段三：RAG 路由 + HyDE 假设文档（P2）

### RAG 路由（复杂度分流）

**目标**：查询入口做复杂度分析与策略分流，简单问题低延迟直达，复杂问题走增强路径——避免所有查询都承担增强策略的成本。

**方案**：
- LLM 分析查询复杂度输出 JSON：`{complexity: simple|medium|complex, strategy: direct|multi_query|decomposition, reasoning}`
- 分流：
  - simple → 直接检索（现有路径）
  - medium → Multi-Query + RAG-Fusion
  - complex → Decomposition（可选叠加 Step-Back）
- **fallback 机制**：LLM 路由判定失败（超时/解析错）→ 默认走 multi_query（中等成本兜底），不阻塞
- 路由结果可缓存（同形查询复用）

**依赖**：阶段一、二策略就绪；`rag.Engine` 编排层改造（prepare 阶段插入 Router）

**验证**：路由判定准确率（构造 simple/medium/complex 分类集）+ 端到端对比（延迟分布 + Recall + 回答质量）；eval 扩展「路由准确率」指标

**配置**：`rag.routing_enabled`、`rag.routing_fallback`（默认 multi_query）

### HyDE 假设文档嵌入

**目标**：解决「查询-文档不对称」（短查询 vs 长文档语义空间差异）——先让 LLM 生成假设文档，用其向量检索真实文档。

**方案**：
- LLM 生成假设文档（即使不确定也要详细，prompt 复用你的 HyDE 模板）
- 假设文档 embedding（需 Embedding API 配额充足——**注意当前 429 配额问题需先解决**）
- 用假设文档向量做检索（替换或补充原查询向量）

**权衡**：+1 次 LLM 调用 + +1 次 Embedding 调用（延迟 +200-500ms）；建议仅对复杂/模糊查询启用，简单查询跳过

**依赖**：LLM + embedder；与路由的 strategy 联动（HyDE 作为可选增强层）

**验证**：模糊查询评估集，对比「直接查询向量 vs HyDE 向量」的 Recall@K

**配置**：`rag.hyde_enabled`、`rag.hyde_skip_simple`（默认 true）

---

## 阶段四：策略可选化配置（用户可抉择，P2）

### 目标

前面阶段引入的优化策略（Multi-Query / RAG-Fusion / Decomposition / Step-Back / HyDE / 路由）各有成本与收益权衡（延迟、API 配额、召回质量）。本阶段提供**面向用户的策略可选化配置**：用户可决定对某个知识库（或全局）启用哪些策略、禁用哪些——例如「用多 query 改写」还是「单 query」，从而在不同场景（追求速度 vs 追求深度）间自主抉择。

### 方案

- **配置层级**：三层可选化——① 全局配置（config.yaml，现有 `rag.*` 开关扩展）② 知识库级覆盖（知识库可设置自己的策略集）③ 单次请求覆盖（API 参数，问答请求可选 strategy）
- **策略项清单**（每项独立开关）：
  - `strategy.query`：`single`（单查询，基线）/ `multi`（多查询 Multi-Query）
  - `strategy.fusion`：`rrf`（RAG-Fusion 融合，多查询时启用）/ `none`
  - `strategy.decomposition`：`off` / `parallel` / `sequential`
  - `strategy.step_back`：`off` / `on`
  - `strategy.hyde`：`off` / `on`
  - `strategy.routing`：`off`（全量走单一策略）/ `auto`（启用路由自动分流）
- **默认值**：与阶段一~三的配置开关一致（保守默认：多查询开、分解/路由关），用户可覆盖
- **前端/接口透出**：知识库详情页提供「检索策略」设置（可选下拉/开关组）；问答 API 请求体支持 `strategy` 覆盖字段；不强制要求 UI，先提供配置与 API 能力
- **约束校验**：非法组合（如 `query=single` + `fusion=rrf` 无多路可融合）在配置加载/请求校验时拒绝并提示

### 依赖

阶段一~三全部策略已实现且可独立开关；`rag.Engine` 的编排层支持「按配置选择策略路径」；`config` 支持知识库级覆盖的数据结构；`handler_chat` 支持请求体 strategy 字段。

### 验证

- 配置层：全局/知识库级/请求级三种覆盖优先级正确（单测）
- 行为层：同一问题在不同 strategy 配置下走不同路径（日志断言检索路数/是否分解）
- 效果层：`cmd/eval` 支持传入 strategy 参数，对比「单查询基线 vs 多查询」等组合的 Recall@K 与回答质量
- 约束：非法组合被拒绝（单测 + API 实测）

### 配置

新增 `rag.strategy` 结构（含上述策略项）；`knowledge_bases` 表可选加 `strategy` JSON 列；问答请求体 `strategy` 字段（可选）。

---

## 执行顺序与依赖

```
阶段一（P0）：Multi-Query + RAG-Fusion ──┐
                                          ├→ 阶段二（P1）：Decomposition + Step-Back ──→ 阶段三（P2）：RAG 路由 + HyDE ──→ 阶段四（P2）：策略可选化配置
阶段三的路由依赖阶段一/二策略已就绪       ┘
阶段四依赖阶段一~三策略全部就绪（可选化需先有可选对象）
```

| 阶段 | 优化点 | 优先级 | 前置依赖 | 验证方式 |
|------|--------|--------|---------|---------|
| 一 | Multi-Query 多查询 | P0 | 现有 LLM/retriever/FuseRRF | eval Recall@K 对比基线 |
| 一 | RAG-Fusion RRF 融合 | P0 | Multi-Query | eval Recall@K + 排序质量 |
| 二 | Decomposition 分解 | P1 | 阶段一 | 复合问题集 LLM-as-Judge |
| 二 | Step-Back 回退 | P1 | LLM/retriever | 趋势/高层问题集 |
| 三 | RAG 路由 | P2 | 阶段一+二 | 路由准确率 + 端到端 |
| 三 | HyDE 假设文档 | P2 | LLM/embedder（需配额） | 模糊查询集 Recall@K |
| 四 | 策略可选化配置 | P2 | 阶段一~三 | 配置优先级单测 + 同问题多策略对比 |

## 不做的事（YAGNI）

- 不做 RAG-Fusion 之外的复杂融合（如加权多路）——RRF 已足够
- 不做微调 Embedding 模型（需标注数据，收益不明确）
- 不做多向量表示索引（标题/摘要/关键句多向量）——与 Multi-Query 作用重叠，优先级低
- 不做 SQL/图数据库查询构建（Text-to-SQL/Cypher）——当前无结构化数据源需求
- 不做 RAPTOR 树结构 / 树遍历检索（大规模文档时再评估）

## 后续执行

每个优化点独立走 `/mew-spec` 流程（spec → plan → task → checklist），从本计划对应的阶段章节作为输入。建议顺序：阶段一 → 阶段二 → 阶段三 → 阶段四，每个阶段完成即用 `cmd/eval` 验证效果后再进入下一阶段。阶段四（策略可选化配置）必须在阶段一~三完成后实施——可选化需先有「可选对象」。
