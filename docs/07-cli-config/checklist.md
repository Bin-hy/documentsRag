# 命令行配置文件路径 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] `parseConfigFlag` 已实现且可被调用（验证：`go build ./cmd/server/...` 编译通过）
- [ ] 单元测试覆盖 -c / --config / 未指定 / 缺值 / 未知参数（验证：`go test ./cmd/server/` 通过）

## 集成

- [ ] main 把解析结果传给 LoadConfig，优先级 flag > 环境变量 > 默认（验证：T2 测试 + 代码路径审查）
- [ ] 启动日志打印实际使用的配置路径（验证：运行 `go run ./cmd/server -c <path>` 观察日志）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...`）
- [ ] 全部测试通过（验证：`go test ./...`）
- [ ] 静态检查无告警（验证：`go vet ./...`）

## 端到端场景

- [ ] 场景 1：`go run ./cmd/server -c configs/config.local.yaml` → 日志显示使用该路径（验证：命令输出）
- [ ] 场景 2：`go run ./cmd/server --config configs/config.local.yaml` → 与 -c 等价（验证：命令输出）
- [ ] 场景 3：`go run ./cmd/server -c /nonexistent.yaml` → 报错且退出码非 0，不静默回退默认配置（验证：命令输出与 `echo $?`）

## 与 spec 验收标准对照

- [ ] AC1 → 场景 1；AC2 → 场景 2；AC3 → T2 无参数用例 + 现有启动行为；AC4 → 场景 3；AC5 → 编译与测试节
