# WebUI 配置管理界面 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] ConfigManager 已实现（验证：`go test ./internal/config/ -run TestConfigManager -v` 全绿）
- [ ] RuntimeComponents 重建已实现（验证：`go build ./internal/app/...` 通过）
- [ ] engine 请求级快照已实现（验证：`go test ./internal/rag/` 通过）
- [ ] API GET/PUT /config 已接入（验证：`go test ./internal/api/` 通过）
- [ ] 前端配置页已实现（验证：`cd frontend && npm run build` 通过）

## 配置读取（单测/接口断言）
- [ ] GET /config 返回分组视图，可修改组含当前值（验证：TestConfigGet）
- [ ] 只读组（Postgres/VectorStore/Server）展示当前值 + 需重启标记（验证：接口响应字段检查）

## 配置修改与热重载
- [ ] bootstrap key PUT 成功 → 视图更新、配置文件写入（验证：TestConfigPutBootstrap + 文件检查）
- [ ] 普通 key PUT 返回 403（验证：TestConfigPutForbidden）
- [ ] 非法配置（temperature=5）PUT 返回 400（验证：TestConfigPutInvalid）
- [ ] 修改 RAG strategy 后新请求按新策略执行（验证：冒烟日志「生效策略」变化）

## 快照与原子性（关键需求）
- [ ] 请求级快照——重载前后，已执行请求用旧配置、新请求用新配置（验证：慢请求期间 PUT，观察两请求分别用新旧配置）
- [ ] 原子替换——并发 Get 始终拿到完整快照（验证：ConfigManager 并发测试 + race 检测通过）
- [ ] 失败回滚——rebuild 失败不替换，服务保持旧配置可用（验证：TestConfigManager rebuild 失败用例 + 构造无效 base_url 冒烟）

## 集成
- [ ] app 装配持 components atomic.Pointer + cfgMgr（验证：`go build ./...` + app 测试）
- [ ] 未提供请求快照时行为与现状一致（验证：现有 rag/api 测试不回归）
- [ ] 前端 /settings 路由 + 侧边栏入口可用（验证：`npm run build` + 浏览器）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新增用例）
- [ ] `go vet ./...` 无告警
- [ ] `cd frontend && npm run build` + `npm test` 通过

## 端到端场景
- [ ] 场景 1（配置修改生效）：bootstrap key 修改 LLM 温度 → 后续问答按新配置执行（验证：真实环境日志）
- [ ] 场景 2（只读展示）：启动级配置不可编辑、标记需重启（验证：浏览器 SettingsView 只读态）
- [ ] 场景 3（权限）：普通 key 打开配置页修改 → 403（验证：curl/浏览器）
- [ ] 场景 4（快照一致性）：慢请求期间改配置 → 两请求新旧配置各完成（验证：真实环境观察日志）

## Spec 验收标准映射
- [ ] AC1（页面分组展示）→ 配置读取 + 前端
- [ ] AC2（修改生效）→ 配置修改第 1 项 + 端到端场景 1
- [ ] AC3（strategy 联动）→ 配置修改第 4 项
- [ ] AC4（只读标记）→ 配置读取第 2 项 + 端到端场景 2
- [ ] AC5（非法拒绝）→ 配置修改第 3 项
- [ ] AC6（失败回滚）→ 快照与原子性第 3 项
- [ ] AC7（普通 key 403）→ 配置修改第 2 项 + 端到端场景 3
- [ ] AC8（请求级快照）→ 快照与原子性第 1 项 + 端到端场景 4
- [ ] AC9（原子替换）→ 快照与原子性第 2 项
- [ ] AC10（build/test/vet）→ 编译与测试
