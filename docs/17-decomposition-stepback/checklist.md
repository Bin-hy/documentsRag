# Decomposition 问题分解 + Step-Back 回退 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 配置项已实现（验证：`go build ./internal/config/...` 通过；decomposition/step_back 字段与默认值正确）
- [ ] prompt 模板已实现（验证：`go build ./internal/rag/...` 通过；4 个模板加载）
- [ ] 判定与子问题生成已实现（验证：decompose.go 编译通过）
- [ ] 检索与综合已实现（验证：decompose.go 编译通过）
- [ ] engine 入口分支已接入（验证：`go test ./internal/rag/ -v` 通过）

## 判定行为（单测断言）
- [ ] 复杂问题判定为需要分解（验证：TestAsk_DecompositionParallel 断言走分解）
- [ ] 简单问题判定为不分解，走常规路径（验证：TestAsk_DecompositionSkip）
- [ ] Step-Back 判定为需要回退时生成回退问题（验证：TestAsk_StepBack）
- [ ] 判定失败降级常规路径，问答不中断（验证：TestAsk_DecompositionFallback）

## 分解流程（单测断言）
- [ ] 并行模式子问题独立并发检索（验证：TestAsk_DecompositionParallel，Sources 来自各子问题）
- [ ] 顺序模式前序子问题上下文拼入后续（验证：TestAsk_DecompositionSequential 断言检索顺序）
- [ ] 综合回答基于所有子问题检索上下文，引用来源覆盖各子问题（验证：TestAsk_DecompositionParallel Sources 非空且来自多子问题）

## 互斥与集成
- [ ] 两策略同时启用时仅 Decomposition 生效（验证：TestAsk_StrategiesMutualExclusion，StepBack 判定未被调用）
- [ ] Step-Back 回答结合回退上下文与原问题检索（验证：TestAsk_StepBack 断言消息含两类上下文）
- [ ] 未启用时行为与现状一致（验证：现有 engine 测试全绿，无回归）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警

## 端到端场景
- [ ] 场景 1（真实环境冒烟）：开启 decomposition_enabled → 复合问题问答日志出现「分解为 N 个子问题」→ 回答正常（验证：真实 LLM 环境观察日志）
- [ ] 场景 2（Step-Back 冒烟）：开启 step_back_enabled → 趋势类问题日志出现「回退问题」→ 回答正常（验证：真实 LLM 环境观察日志）
- [ ] 场景 3（关闭回归）：两策略默认关 → 问答行为与之前一致（验证：日志无判定/子问题记录，走常规路径）

## Spec 验收标准映射
- [ ] AC1（日志子问题列表+逐检索）→ 端到端场景 1
- [ ] AC2（并行/顺序模式差异）→ 分解流程第 1/2 项
- [ ] AC3（综合回答引用覆盖）→ 分解流程第 3 项
- [ ] AC4（简单问题跳过）→ 判定行为第 2 项
- [ ] AC5（回退问题检索日志）→ 端到端场景 2
- [ ] AC6（回退+原问题合并）→ 互斥与集成第 2 项
- [ ] AC7（互斥）→ 互斥与集成第 1 项
- [ ] AC8（降级）→ 判定行为第 4 项
- [ ] AC9（零变化）→ 互斥与集成第 3 项 + 端到端场景 3
- [ ] AC10（build/test/vet）→ 编译与测试
