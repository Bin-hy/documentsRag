# 扫描版 PDF 与无效内容识别拒绝 Plan

## 架构概览

在现有入库链路（Load → Chunk → Embed → Store）中新增一个**可读文本判定**环节，分三层实现：

1. **loader 层（判定核心）**：新增「可读文本校验」能力——解析结果（Blocks）汇总可读字符数，与阈值比较判定是否「无可读文本」。输出明确错误类型 `ErrNoReadableContent`。
2. **pipeline 层（入库拒绝）**：`Ingest` 在 Load 后立即校验，无可读文本则返回该错误 → worker 将任务置为 failed，error_message 携带原因 → 不写向量/BM25。
3. **API 层（上传预检）**：`UploadDocument` 对 PDF（及全部格式）做快速预检——实际解析一次但**不嵌入向量**，仅统计文本量，判定扫描件则返回 400 拒绝，不创建入库任务。

判定规则（N1 阈值可配置）：
- 全文可读字符数 < `MinReadableChars`（默认 20）→ 无可读文本
- PDF 额外按页判定：可读字符数 < 每页下限（默认 0，即不启用按页）——默认仅用全文阈值，保持简单

## 核心数据结构

### 新错误类型（loader 包）
```go
// ErrNoReadableContent 文档无可读文本（扫描件/空文件/纯乱码）
type ErrNoReadableContent struct {
    Format     string // 文件格式（pdf/txt/...）
    Readable   int    // 实际可读字符数
    MinChars   int    // 阈值
}
func (e *ErrNoReadableContent) Error() string {
    // "文档无可读文本（扫描件或内容为空），可读字符 0/最低 20，不支持解析入库"
}
```

### 判定配置（config 新增 LoaderConfig）
```go
type LoaderConfig struct {
    MinReadableChars int `yaml:"min_readable_chars"` // 全文最低可读字符数，默认 20；0 表示不启用判定
}
```
`Config` 增加 `Loader LoaderConfig \`yaml:"loader"\``；config.yaml 追加 `loader:` 段。

### 可读文本统计（loader 包）
```go
// ReadableCharCount 统计内容中的可读字符（排除空白/控制字符/常见乱码标记）
func ReadableCharCount(text string) int
```
判定依据：`isprintable` 且非空白、非 `\uFFFD` 替换符；PDF 图像指令（`q ... cm /Im.. Do` 行）因含大量命令字符但无真实文字，计入但整体字符少。

## 模块设计

### loader 校验（internal/loader/）
**职责：** 判定与错误定义。
- 新建 `validate.go`：`ReadableCharCount`、`ValidateReadable(doc *Document, cfg LoaderConfig) error`（Blocks 汇总文本量，低于阈值返回 `ErrNoReadableContent`）
- `errors.go`：新增 `ErrNoReadableContent` 类型
- **不修改**各 Parser（解析行为不变，校验在 Loader.Load 之后统一做）

### pipeline（internal/pipeline/pipeline.go）
**职责：** 入库前拒绝。
- `defaultPipeline` 增加 `loaderConfig LoaderConfig` 字段；`NewPipeline` 增加参数
- `Ingest`：Load 后调用 `loader.ValidateReadable(result.Document, p.loaderConfig)`；返回错误直接上抛（worker 捕获置 failed）
- 空 Blocks 分支（现返回 nil,nil）改为返回 `ErrNoReadableContent`（语义一致且带原因）

### worker（internal/task/worker.go）
**职责：** 错误 → 任务 failed。
- 现有逻辑已把 `Ingest` 错误写入 task.ErrorMessage；验证 `ErrNoReadableContent.Error()` 文案透出即可，无需改动（若 worker 有「空结果按成功处理」逻辑则调整）

### API 上传预检（internal/api/handler_doc.go）
**职责：** 保存前拒绝扫描件。
- `UploadDocument`：文件校验后、保存磁盘前，用 `registry.Resolve` 的 Parser 解析（`ModeStrict`）→ `ValidateReadable` → 判定失败返回 `CodeBadRequest`（400）与明确 message；不创建任务/不保存文件
- 注意：预检会重复解析一次（入库时还会再解析）——N2 权衡：仅对 PDF 与 txt 等轻量格式预检，或全部格式预检（解析成本可接受，避免保存后才发现）

### 前端（frontend/src/）
**职责：** 展示失败原因。
- 现有 `KbDetailView.vue` 文档列表已显示 Status=failed 与任务错误详情（`fetchTask` 拉取 error_message）——**验证现有展示即可，通常无需改动**；若失败原因不展示则补一行

### 配置（configs/config.yaml）
**职责：** 暴露阈值。
- 追加 `loader:` 段注释说明（min_readable_chars 默认 20）

## 模块交互

```
上传 PDF
  → handler_doc.UploadDocument
      → registry.Resolve(.pdf) → parser.Parse(ModeStrict) → 文本量统计
      → ValidateReadable 失败 → 返回 400「扫描件/无可读文本」→ 不保存文件、不建任务
      → 通过 → 保存文件 + 建任务
入库 worker
  → pipeline.Ingest
      → loader.Load → ValidateReadable（再次校验，防绕过预检）
      → 失败 → worker 置任务 failed + error_message
      → 通过 → Chunk → Embed → Store（照旧）
```

## 文件组织

```
internal/loader/
├── validate.go       — 新增：ReadableCharCount / ValidateReadable / ErrNoReadableContent 判定
├── errors.go         — 新增 ErrNoReadableContent 类型
├── validate_test.go  — 新增：乱码/空/正常文本判定用例
internal/pipeline/
├── pipeline.go       — 修改：加 loaderConfig，Ingest 调 ValidateReadable
├── pipeline_test.go  — 修改：适配新签名 + 无可读文本拒绝用例
internal/config/
├── config.go         — 修改：新增 LoaderConfig 与 Config.Loader
configs/config.yaml   — 修改：追加 loader: 段
internal/api/
├── handler_doc.go    — 修改：UploadDocument 预检
├── api_test.go       — 修改：扫描件上传 400 用例
internal/task/
├── worker.go         — 检查（通常无需改）
frontend/src/views/KbDetailView.vue — 检查失败原因展示（通常无需改）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 判定方式 | 可读字符数阈值（全文 < 20 字符） | 用户选定；轻量无依赖；扫描件文本量趋近 0，区分度高 |
| 阈值默认值 | MinReadableChars=20 | 正常文档动辄数百字符；扫描件 0-5 字符；20 是安全分界 |
| 判定位置 | loader 校验函数 + pipeline 调用 + API 预检 | 双层防线：预检拦截早、pipeline 兜底防绕过；逻辑收敛在 loader 一处 |
| 是否 OCR | 否 | 用户明确本次不做 OCR，仅识别拒绝 |
| 预检格式范围 | 全部格式统一预检 | 解析成本可接受（入库本来就要解析），避免保存后才发现 |
| 空 Blocks 语义 | 返回 ErrNoReadableContent | 与「无可读文本」统一，避免静默成功 |
| worker 是否改 | 通常不改 | 现有错误上抛已置 failed；仅验证文案透出 |
| 前端是否改 | 通常不改 | 失败原因展示已具备；验证后按需微调 |
