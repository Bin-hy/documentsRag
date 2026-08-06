# 策略可选化配置 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] StrategyConfig 结构与默认值已实现（验证：`go build ./internal/config/...` 通过）
- [ ] store strategy 列已迁移且 SQL 更新（验证：`go build ./internal/store/...` + `go test ./internal/store/` 通过）
- [ ] ResolveStrategy / ValidateStrategy 已实现（验证：`go test ./internal/rag/ -run TestStrategy -v` 通过）
- [ ] engine 生效策略接入（验证：`go build ./internal/rag/...` 通过）
- [ ] API 请求级/知识库级字段已接入（验证：`go test ./internal/api/` 通过）
- [ ] 前端策略设置 UI 已实现（验证：`cd frontend && npm run build` 通过）

## 合并与校验（单测断言）
- [ ] 请求级覆盖知识库级、知识库级覆盖全局（验证：TestStrategy 合并优先级用例）
- [ ] 空字段继承低层级、默认值兜底（验证：TestStrategy 默认值用例）
- [ ] query=single + fusion=rrf 被拒绝（验证：TestStrategy 非法组合用例）
- [ ] routing=auto + decomposition≠off 被拒绝（验证：TestStrategy 非法组合用例）
- [ ] routing=auto + step_back=on 被拒绝（验证：TestStrategy 非法组合用例）

## 行为层（单测/集成断言）
- [ ] 请求带 strategy=query:single → 走单查询路径（验证：api_test 请求级覆盖用例）
- [ ] 知识库带 strategy → 该 kb 问答按策略执行（验证：api_test kb 级用例）
- [ ] 未提供 kb/请求 strategy 时行为与现状一致（验证：现有 engine/api 测试不回归）

## 集成
- [ ] app 装配传递全局 strategy（验证：`go build ./...` 通过）
- [ ] 前端 createKb/updateKb 支持 strategy（验证：`npm run build` + 类型检查）
- [ ] 前端 chat 请求体支持 strategy（验证：`npm run build`）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警
- [ ] `cd frontend && npm run build` + `npm test` 通过

## 端到端场景
- [ ] 场景 1（知识库级）：创建带 strategy 的知识库 → 问答日志显示按该策略路径执行（验证：真实环境观察日志）
- [ ] 场景 2（请求级覆盖）：请求体带 strategy 覆盖 kb 策略 → 日志显示请求策略生效（验证：真实环境观察日志）
- [ ] 场景 3（前端保存）：KbDetailView 设置策略保存 → GET 知识库返回 strategy（验证：用户本机浏览器）

## Spec 验收标准映射
- [ ] AC1（全局 rag.strategy 生效）→ 行为层 + 端到端场景 1（全局默认路径）
- [ ] AC2（知识库级生效）→ 行为层第 2 项 + 端到端场景 1
- [ ] AC3（请求级覆盖）→ 行为层第 1 项 + 端到端场景 2
- [ ] AC4（未设置回退全局=现状）→ 行为层第 3 项
- [ ] AC5（非法组合拒绝）→ 合并与校验 3-5 项
- [ ] AC6（前端设置保存）→ 端到端场景 3
- [ ] AC7（build/test/vet）→ 编译与测试
