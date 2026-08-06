# 策略可选化配置 Spec

## 背景

BinRag 已完成阶段一~三的策略：Multi-Query、RAG-Fusion、Decomposition、Step-Back、HyDE、RAG 路由。这些策略各有成本与收益权衡（延迟、API 配额、召回质量），但当前**只能通过全局 config.yaml 的散开关控制**——无法按知识库差异化、无法单次请求覆盖、前端无设置入口、非法组合无校验。

`docs/15-rag优化计划.md` 阶段四（P2）：提供**面向用户的策略可选化配置**——统一策略配置结构、三级覆盖（全局/知识库级/请求级）、约束校验、前端设置 UI。

## 目标

- 策略配置统一封装（替代散落的 `rag.*` 开关语义），用户可自主抉择各策略开关
- 三级覆盖：全局 config.yaml → 知识库级（DB strategy JSON）→ 单次请求（API strategy 字段），优先级请求 > 知识库 > 全局
- 非法策略组合在配置加载/请求校验时被拒绝并提示
- 前端知识库详情页提供策略设置 UI（开关组）

## 功能需求

- F1: 策略配置结构——`strategy` 统一封装，含：`query`（single/multi）、`fusion`（rrf/none）、`decomposition`（off/parallel/sequential）、`step_back`（off/on）、`hyde`（off/on）、`routing`（off/auto）；与现有 `rag.*` 开关等价（向后兼容，现有配置不破坏）
- F2: 全局配置——config.yaml 的 `rag.strategy` 段定义全局默认策略（保守默认：multi_query 开、分解/路由关，与阶段一~三一致）
- F3: 知识库级覆盖——knowledge_bases 表新增 `strategy` JSON 列，知识库可设置自己的策略集；查询时读取并覆盖全局
- F4: 请求级覆盖——问答 API 请求体支持可选 `strategy` 字段（单次请求覆盖全局与知识库级）
- F5: 优先级——请求级 > 知识库级 > 全局；未设置的层级回退下一级
- F6: 约束校验——非法组合（如 `query=single` + `fusion=rrf` 无多路可融合；`routing=auto` + `decomposition=parallel` 冲突）在配置加载/请求校验时拒绝并返回明确错误
- F7: 前端设置 UI——知识库详情页提供策略设置区（查询模式/分解/回退/HyDE/路由开关组），保存到知识库 strategy；聊天页可选请求级策略（高级设置）
- F8: 引擎接入——rag.Engine 的 Ask/StreamAsk 按「合并后的策略」决定走哪条路径（策略解析在 engine 入口，替代现有直接读 `e.cfg.*On()` 的逻辑）

## 非功能需求

- N1: 兼容——未配置 strategy 时行为与现状完全一致（全局默认 = 阶段一~三默认开关）
- N2: 校验时机——非法组合在配置加载（全局）与请求校验（API）两处拒绝，不进入引擎
- N3: 可观测——日志记录最终生效策略（合并后），供评估与排障
- N4: 数据库迁移——knowledge_bases 加列用轻量迁移（CREATE TABLE IF NOT EXISTS 不含新列时 ALTER TABLE ADD COLUMN IF NOT EXISTS）
- N5: 前端兼容——现有 KbDetailView 布局不破坏，策略设置区为新增区块

## 不做的事

- 不做策略 A/B 测试自动化（人工切换对比，eval 已有）
- 不做每个策略的深度参数配置（如 multi_query_count 保持全局，不进 strategy 结构）
- 不做缓存/记忆用户偏好（每次请求显式或按知识库）
- 不做权限控制（任何有 Key 的用户可改知识库策略）
- 不改阶段一~三策略实现（仅编排层读取合并后的策略）

## 验收标准

- AC1: config.yaml 配置 `rag.strategy` 后，问答按该策略路径执行（验证：日志显示生效策略）
- AC2: 知识库设置 strategy 后，问答对该 kb 按知识库策略执行，其他 kb 不受影响（验证：两 kb 对比日志）
- AC3: 请求体带 `strategy` 时覆盖知识库与全局（验证：同 kb 不同请求不同路径）
- AC4: 未设置 strategy 的知识库/请求回退全局默认，行为与阶段一~三一致（验证：现有测试不回归）
- AC5: 非法组合（query=single + fusion=rrf）被拒绝并返回明确错误（验证：配置加载/请求校验单测）
- AC6: 前端知识库详情页可设置并保存策略（验证：浏览器操作，保存后 GET 知识库返回 strategy）
- AC7: `go build ./...`、`go test ./...`、`go vet ./...` 全部通过
