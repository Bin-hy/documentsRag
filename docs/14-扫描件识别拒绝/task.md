# 扫描版 PDF 与无效内容识别拒绝 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/loader/validate.go` | 可读字符统计 + 判定 + ErrNoReadableContent |
| 修改 | `internal/loader/errors.go` | 新增 ErrNoReadableContent 类型 |
| 新建 | `internal/loader/validate_test.go` | 判定单元测试 |
| 修改 | `internal/config/config.go` | 新增 LoaderConfig + Config.Loader |
| 修改 | `configs/config.yaml` | 追加 loader: 配置段 |
| 修改 | `internal/pipeline/pipeline.go` | Ingest 调 ValidateReadable，签名加 LoaderConfig |
| 修改 | `internal/pipeline/pipeline_test.go` | 适配新签名 + 拒绝用例 |
| 修改 | `internal/app/app.go` | NewPipeline 传 LoaderConfig |
| 修改 | `internal/api/handler_doc.go` | 上传预检 |
| 修改 | `internal/api/api_test.go` | 扫描件上传 400 用例 |

## T1: loader 判定核心

**文件：** `internal/loader/validate.go`、`internal/loader/errors.go`、`internal/loader/validate_test.go`
**依赖：** 无
**步骤：**
1. `errors.go` 新增 `ErrNoReadableContent{Format string; Readable int; MinChars int}`，Error() 文案：「文档无可读文本（扫描件或内容为空），可读字符 X/最低 Y，不支持解析入库」
2. `validate.go` 实现 `ReadableCharCount(text string) int`：统计可读字符（isprintable 且非空白、排除 `\uFFFD`），中英文/数字/标点均计入，PDF 图像指令行（`q ... cm` 等）按普通可打印字符计入但量少
3. `validate.go` 实现 `ValidateReadable(doc *Document, minChars int) error`：汇总所有 Blocks 的 Content 可读字符数；minChars<=0 时跳过判定返回 nil；低于阈值返回 `*ErrNoReadableContent`
4. `validate_test.go`：正常中文文本计数正确；纯乱码/图像指令计数低于阈值返回错误；空 Blocks 返回错误；minChars=0 跳过判定

**验证：** `go test ./internal/loader/ -run TestValidate -v` 通过

## T2: 配置项

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** T1（ErrNoReadableContent 类型）
**步骤：**
1. `config.go` 新增 `LoaderConfig{MinReadableChars int \`yaml:"min_readable_chars"\`}`；`Config` 增加 `Loader LoaderConfig \`yaml:"loader"\``
2. applyDefaults：`MinReadableChars` 默认 20（当前值为 0 时）
3. config.yaml 追加 `loader:` 段（含注释：min_readable_chars 全文最低可读字符数，默认 20，0 禁用）

**验证：** `go build ./internal/config/...` 通过；`go test ./internal/config/`（如有）通过

## T3: pipeline 拒绝

**文件：** `internal/pipeline/pipeline.go`、`internal/pipeline/pipeline_test.go`
**依赖：** T1、T2
**步骤：**
1. `defaultPipeline` 增加 `loaderConfig loader.LoaderConfig` 字段
2. `NewPipeline` 增加参数 `lc loader.LoaderConfig`（放在 cfg 之后或 bm25 之前，顺序与调用方同步）
3. `Ingest`：Load 成功后调用 `loader.ValidateReadable(result.Document, p.loaderConfig.MinReadableChars)`；错误直接返回（上抛给 worker 置 failed）
4. 空 Blocks 分支（`result.Document == nil || len(Blocks)==0`）改为返回 `&loader.ErrNoReadableContent{...}`（不再静默 nil,nil）
5. pipeline_test：适配 `NewPipeline` 新签名；新增用例——loader 返回无可读文本时 Ingest 返回 ErrNoReadableContent 且不调用 embedder

**验证：** `go build ./...` + `go test ./internal/pipeline/` 通过

## T4: app 装配与 worker 验证

**文件：** `internal/app/app.go`、`internal/task/worker_test.go`
**依赖：** T3
**步骤：**
1. `app.go` NewPipeline 调用传 `cfg.Loader`
2. 检查 worker 错误处理（worker.go:145-154 已把 Ingest 错误写入 ErrorMessage）——若 worker_test 有「空结果成功」断言需调整（现在空文档返回 ErrNoReadableContent 而非 nil）
3. worker_test 若存在「空文档任务成功」用例，改为断言 failed

**验证：** `go build ./...` + `go test ./internal/app/ ./internal/task/` 通过

## T5: API 上传预检

**文件：** `internal/api/handler_doc.go`、`internal/api/api_test.go`
**依赖：** T1
**步骤：**
1. `UploadDocument`：`registry.Resolve` 后（扩展名校验处）、`SaveUploadedFile` 之前，读取文件流解析一次（`ModeStrict`）→ `loader.ValidateReadable(doc, h.cfg.Loader.MinReadableChars)` → 失败返回 `CodeBadRequest` + 明确 message（「无可读文本」），不保存文件不建任务
2. 预检需把 reader 重置或重新读取（用 `file.Open()` 或保存到临时 buffer 供预检与保存共用）
3. handler 结构增加 `loaderConfig loader.LoaderConfig` 字段（router 装配传入 deps.Config.Loader）
4. api_test：新增用例——上传乱码 PDF/空 TXT 返回 400 且不创建文档/任务；正常文件仍 200

**验证：** `go build ./...` + `go test ./internal/api/` 通过

## T6: 全量验证 + 端到端冒烟

**文件：** 无新增
**依赖：** T1-T5
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿
2. 手工冒烟：用现有扫描 PDF（地下空间合同）上传 → 期望 400 拒绝；用文本 TXT 上传 → 成功入库
3. 前端验证失败原因展示（用户本机：上传乱码文件 → 任务列表显示失败原因）

**验证：** 上述命令全通过；扫描件上传被拒、正常文件成功

## 执行顺序

```
T1 → T2 → T3 → T4 → T5 → T6
```

T1 是核心（判定+错误类型），T2 依赖 T1 的类型、T3 依赖 T1/T2、T4 依赖 T3、T5 依赖 T1（可并行于 T3/T4）、T6 收尾。
