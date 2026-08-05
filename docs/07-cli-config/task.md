# 命令行配置文件路径 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `cmd/server/main.go` | 新增 parseConfigFlag + main 调用处 + 启动日志打印配置路径 |
| 新建 | `cmd/server/main_test.go` | parseConfigFlag 单元测试 |

## T1: 实现 parseConfigFlag

**文件：** `cmd/server/main.go`
**依赖：** 无
**步骤：**
1. 新增 `parseConfigFlag(args []string) string`：`flag.NewFlagSet("binrag", flag.ContinueOnError)` + `SetOutput(io.Discard)` + `StringVar(&path, "c", ...)` 与 `StringVar(&path, "config", ...)` 指向同一变量 + `fs.Parse(args)`，返回 path
2. main 中把 `config.LoadConfig("")` 改为 `config.LoadConfig(parseConfigFlag(os.Args[1:]))`
3. 加载成功后日志打印实际配置路径（`cfgPath` 或「默认路径」）
4. import 增加 `flag`、`io`

**验证：** `go build ./cmd/server/...` 编译通过

## T2: parseConfigFlag 测试

**文件：** `cmd/server/main_test.go`
**依赖：** T1
**步骤：**
1. `-c a.yaml` → "a.yaml"
2. `--config b.yaml` → "b.yaml"
3. 无参数 `[]` → ""
4. 缺值 `["-c"]` → ""（解析错误被忽略）
5. 其他未知参数 `["-x"]` → ""（不影响）

**验证：** `go test ./cmd/server/ -v` 全部通过

## T3: 手动验证与全量

**文件：** 无新增
**依赖：** T1、T2
**步骤：**
1. `go run ./cmd/server -c configs/config.local.yaml` 观察启动日志打印该路径（配置加载到连接 Postgres 失败前的日志即可；或临时用 `-c /nonexistent.yaml` 观察报错退出）
2. `go build ./...`、`go vet ./...`、`go test ./...` 全绿

**验证：** 上述命令通过；`-c /nonexistent.yaml` 输出错误且退出码非 0

## 执行顺序

```
T1 → T2 → T3
```
