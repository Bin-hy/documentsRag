# Multi-Query 多查询 + RAG-Fusion 融合 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 配置项已实现（验证：`go build ./internal/config/...` 通过；`multi_query_enabled/count/concurrency` 存在且默认值正确）
- [ ] FuseMultiQuery 融合函数已实现（验证：`go test ./internal/retriever/ -run TestFuseMultiQuery -v` 全绿）
- [ ] SearchMulti 多路检索已实现（验证：`go build ./internal/retriever/...` 通过）
- [ ] 多查询模板与引擎编排已实现（验证：`go test ./internal/rag/ -v` 通过）

## 融合行为（单测断言）
- [ ] 多路结果有交集时，交集文档 RRF 分数更高排前（验证：TestFuseMultiQuery 用例 A）
- [ ] 三路无交集时按各自 rank 融合排序（验证：用例 B）
- [ ] topK 截断生效（验证：用例 C）
- [ ] 融合结果无重复文档 ID（验证：用例 D）

## 多查询路径（engine 测试断言）
- [ ] 启用多查询时调用 SearchMulti 且回答正常（验证：TestAsk_MultiQueryEnabled）
- [ ] 变体生成失败降级单查询，问答不报错（验证：TestAsk_MultiQueryFallback）
- [ ] 关闭多查询时走现有单查询路径，SearchMulti 不被调用（验证：TestAsk_MultiQueryDisabled）
- [ ] 多路检索单路失败被忽略，其余路正常（验证：SearchMulti 单测或集成断言）

## 集成
- [ ] 融合结果继续走重排序与上下文组装，引用来源正常（验证：启用多查询后问答 Sources 非空且来自融合结果）
- [ ] 关闭多查询时行为与现状完全一致（验证：现有 eval 基线不回归 + 现有 engine 测试全绿）
- [ ] 多路检索并行执行（并发上限生效）（验证：SearchMulti 日志或单测断言并发）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警

## 端到端场景
- [ ] 场景 1（真实环境冒烟）：开启 `multi_query_enabled: true` → 问答日志出现「多查询生成完成」与「多路检索完成」→ 回答正常（验证：真实 LLM 环境观察日志）
- [ ] 场景 2（关闭回归）：关闭配置 → 问答日志为单查询路径（「Query 改写完成」或直接检索）→ 行为与之前一致（验证：日志对比）
- [ ] 场景 3（降级）：LLM 生成变体异常 → 日志出现「多查询生成失败，降级单查询」→ 问答仍返回（验证：构造异常或停 LLM 观察）

## Spec 验收标准映射
- [ ] AC1（日志显示变体生成与多路检索）→ 端到端场景 1
- [ ] AC2（融合去重排序）→ 融合行为 1-4 项
- [ ] AC3（关闭时零变化）→ 集成第 2 项 + 端到端场景 2
- [ ] AC4（生成失败降级）→ 多查询路径第 2 项 + 端到端场景 3
- [ ] AC5（重排兼容）→ 集成第 1 项
- [ ] AC6（关闭零行为变化）→ 集成第 2 项
- [ ] AC7（build/test/vet）→ 编译与测试
