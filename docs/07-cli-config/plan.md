# 命令行配置文件路径 Plan

## 架构概览

改动集中在 `cmd/server/main.go`：新增一个纯函数解析命令行参数中的配置文件路径，把结果传给现有的 `config.LoadConfig(path)`。`LoadConfig` 已保证「path 非空则直接用」，因此优先级（flag > 环境变量 > 默认路径）由调用方自然达成，`internal/config` 无需改动。

## 核心接口

```go
// parseConfigFlag 从命令行参数中解析 -c / --config 指定的配置文件路径
// 两个别名指向同一变量；未指定或解析失败返回空串
func parseConfigFlag(args []string) string
```

## 模块设计

### cmd/server/main.go

**职责：** 解析配置路径并传入 LoadConfig
**对外接口：** `parseConfigFlag(args []string) string`
**依赖：** 标准库 flag / io

实现细节：
- 用 `flag.NewFlagSet("binrag", flag.ContinueOnError)` 自定义 FlagSet，避免污染全局 flag
- `fs.StringVar(&path, "c", "", ...)` 与 `fs.StringVar(&path, "config", "", ...)` 指向同一变量（flag 包将 `--config` 与 `-config` 等价处理）
- 输出丢弃（`fs.SetOutput(io.Discard)`），未知参数与解析错误静默忽略——本项目无其他 flag，无需报错
- main 中：`cfg, err := config.LoadConfig(parseConfigFlag(os.Args[1:]))`
- 启动日志在成功加载后打印实际配置文件路径（可观测，支撑 AC1）

## 文件组织

```
cmd/server/
├── main.go        — 修改：新增 parseConfigFlag + 调用处
└── main_test.go   — 新建：parseConfigFlag 参数解析测试
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| flag 实现 | 标准库 flag + 自定义 FlagSet | 零依赖；ContinueOnError + io.Discard 避免污染全局 flag 与报错输出 |
| 别名 | -c 与 --config 指向同一变量 | flag 包天然支持单双横线，两个名字都可用 |
| 优先级 | flag > 环境变量 > 默认 | LoadConfig(path) 已实现「非空优先」，调用方传值即达成 |
| 错误路径 | 交给 LoadConfig 返回错误 → main 打印退出 | 复用现有错误处理，不静默回退 |
