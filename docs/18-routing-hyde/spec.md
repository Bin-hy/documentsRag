# RAG 路由 + HyDE 假设文档 Spec

## 背景

BinRag 已实现阶段一（Multi-Query + RAG-Fusion）与阶段二（Decomposition + Step-Back）。当前所有查询默认走**同一策略路径**（除非用户显式开启某策略）——简单问题也承担完整流程成本，复杂问题无法自动选择增强策略；同时「查询-文档不对称」（短查询 vs 长文档语义空间差异）尚未处理。

`docs/15-rag优化计划.md` 阶段三（P2）：引入 **RAG 路由**（查询复杂度分流，按需选择策略路径）与 **HyDE**（假设文档嵌入，缓解查询-文档不对称）。

## 目标

- 查询入口自动分析复杂度（simple/medium/complex），分流到最合适的策略路径：简单直达低延迟、中等走多查询、复杂走分解
- 路由判定失败时回退到默认策略（不阻塞）
- HyDE 生成假设文档并用其向量检索真实文档，提升模糊/复杂查询的语义匹配
- 路由与 HyDE 作为可选策略（独立开关），与阶段一、二策略协同

## 功能需求

- F1: 复杂度判定——启用路由时，LLM 分析查询输出 `{complexity: simple|medium|complex, strategy, reasoning}`，作为入口分流依据
- F2: 策略分流——simple → 直接检索（现有常规路径）；medium → Multi-Query + RAG-Fusion；complex → Decomposition（可选叠加 Step-Back）；根据 strategy 决定具体执行路径
- F3: 路由降级——LLM 判定失败（超时/解析错）时回退到默认策略（可配置，默认 multi_query），问答不中断
- F4: HyDE 生成——启用 HyDE 时，LLM 生成假设文档（详细、即使不确定），作为检索查询
- F5: HyDE 向量检索——假设文档经 embedding 得到向量，用该向量检索真实文档（替换原查询向量；原查询仍作为一路，与 HyDE 结果 RRF 融合）
- F6: HyDE 跳过简单查询——配置 `hyde_skip_simple`（默认 true），简单查询不启用 HyDE（节省成本）
- F7: 与路由协同——路由判定为 medium/complex 时 HyDE 可叠加；HyDE 作为检索增强层，不影响路由的策略分流
- F8: 可观测——日志记录路由判定结果（complexity/strategy/reasoning）、HyDE 假设文档与检索路数，供评估

## 非功能需求

- N1: 配置驱动——`routing_enabled`、`routing_fallback`（默认 multi_query）、`hyde_enabled`、`hyde_skip_simple`（默认 true）均可配置，默认关闭（保持现状）
- N2: 成本权衡——路由新增 1 次 LLM 调用；HyDE 新增 1 次 LLM + 1 次 Embedding 调用（延迟 +200-500ms）；仅在启用时产生
- N3: 兼容——未启用时行为与现状完全一致；引用来源、SSE 事件流、历史记录结构不变
- N4: Embedding 配额——HyDE 依赖 Embedding API（当前 429 配额问题需先解决；配额不足时 HyDE 应降级为原查询向量，不中断）
- N5: 可观测——日志记录判定、降级、HyDE 生成与检索耗时

## 不做的事

- 不做路由结果缓存（同形查询复用）——本阶段不做缓存，留待性能优化
- 不做多数据源路由（SQL/图/Web 搜索）——当前只有向量知识库
- 不改阶段一/二策略实现（仅编排层调用）
- 不做 HyDE 假设文档缓存
- 不做前端路由可视化/调试界面

## 验收标准

- AC1: 启用路由后，日志显示复杂度判定结果与 strategy（验证：日志含 `complexity` 与 `strategy`）
- AC2: simple 问题走直接检索（低延迟路径）、medium 走多查询、complex 走分解（验证：单测断言各档位调用对应路径）
- AC3: 路由判定失败时回退到配置的默认策略，问答不中断（验证：mock LLM 失败，观察降级与正常回答）
- AC4: 启用 HyDE 后，日志显示假设文档生成与双路检索（HyDE 向量 + 原查询）（验证：日志含 HyDE 标记与检索路数）
- AC5: HyDE 结果与原查询结果 RRF 融合，引用来源正常（验证：单测断言融合与 Sources）
- AC6: `hyde_skip_simple=true` 时简单查询不生成假设文档（验证：单测断言简单查询无 HyDE 调用）
- AC7: Embedding 失败时 HyDE 降级为原查询向量，问答不中断（验证：mock embedder 失败，观察降级）
- AC8: 未启用路由/HyDE 时行为与现状一致（验证：现有测试不回归；关闭配置跑 eval 基线）
- AC9: `go build ./...`、`go test ./...`、`go vet ./...` 全部通过
