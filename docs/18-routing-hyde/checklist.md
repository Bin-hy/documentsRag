# RAG 路由 + HyDE 假设文档 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 配置项已实现（验证：`go build ./internal/config/...` 通过；routing/hyde 字段与默认值正确）
- [ ] SearchByVector 已实现（验证：`go build ./internal/retriever/...` 通过）
- [ ] routing/hyde 模板已实现（验证：`go build ./internal/rag/...` 通过）
- [ ] routeQuery / hydeSearch 已实现（验证：routing.go 编译通过）
- [ ] engine 路由分支与 embedder 注入已接入（验证：`go build ./...` 通过，app.go 传 emb）

## 路由行为（单测断言）
- [ ] simple/direct → 常规路径（Search 被调、SearchMulti 未调）（验证：TestAsk_RoutingDirect）
- [ ] medium/multi_query → SearchMulti 被调（验证：TestAsk_RoutingMultiQuery）
- [ ] complex/decomposition → 分解路径（验证：TestAsk_RoutingDecomposition）
- [ ] 判定失败 → 回退默认 multi_query（验证：TestAsk_RoutingFallback）

## HyDE 行为（单测断言）
- [ ] 启用 HyDE → 假设文档生成 + SearchByVector + 融合（验证：TestAsk_HyDE）
- [ ] skip_simple=true 时简单查询跳过 HyDE（验证：TestAsk_HyDESkipSimple，SearchByVector 未调）
- [ ] Embedding 失败 → 降级原查询，问答不中断（验证：TestAsk_HyDEEmbedFail）

## 集成
- [ ] NewEngine 签名变更后 app.go 两处装配正确（验证：`go build ./...` + `go test ./internal/app/`）
- [ ] 未启用路由/HyDE 时行为与现状一致（验证：现有 engine 测试全绿，无回归）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警

## 端到端场景
- [ ] 场景 1（路由冒烟）：开启 routing_enabled → 问答日志出现复杂度判定与 strategy 分流 → 回答正常（验证：真实 LLM 环境观察日志）
- [ ] 场景 2（HyDE 冒烟）：开启 hyde_enabled → 日志出现假设文档生成与双路检索（需 Embedding 配额恢复）（验证：真实环境观察）
- [ ] 场景 3（关闭回归）：两策略默认关 → 问答行为与之前一致（验证：日志无路由/HyDE 记录）

## Spec 验收标准映射
- [ ] AC1（日志复杂度+strategy）→ 端到端场景 1
- [ ] AC2（三档分流）→ 路由行为 1-3 项
- [ ] AC3（判定失败回退）→ 路由行为第 4 项
- [ ] AC4（HyDE 假设文档+双路检索）→ 端到端场景 2
- [ ] AC5（HyDE 融合+引用）→ HyDE 行为第 1 项
- [ ] AC6（skip_simple）→ HyDE 行为第 2 项
- [ ] AC7（Embedding 失败降级）→ HyDE 行为第 3 项
- [ ] AC8（零变化）→ 集成第 2 项 + 端到端场景 3
- [ ] AC9（build/test/vet）→ 编译与测试
