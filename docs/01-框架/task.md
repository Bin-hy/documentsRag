# 文档加载器 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `go.mod` | Go module 初始化 |
| 新建 | `internal/loader/types.go` | 核心类型定义（Block、Document、FileInfo、LoadOptions 等） |
| 新建 | `internal/loader/parser.go` | Parser 接口定义 |
| 新建 | `internal/loader/errors.go` | 错误类型定义 |
| 新建 | `internal/loader/registry.go` | Registry 接口 + defaultRegistry 实现 |
| 新建 | `internal/loader/loader.go` | Loader 接口 + defaultLoader 实现 + NewLoader 工厂 |
| 新建 | `internal/loader/parsers/txt.go` | TXT 解析器 |
| 新建 | `internal/loader/parsers/markdown.go` | Markdown 解析器 |
| 新建 | `internal/loader/parsers/pdf.go` | PDF 解析器 |
| 新建 | `internal/loader/parsers/docx.go` | DOCX 解析器 |
| 新建 | `internal/loader/parsers/csv.go` | CSV 解析器 |
| 新建 | `internal/loader/parsers/excel.go` | Excel 解析器 |
| 新建 | `internal/loader/parsers/html.go` | HTML 解析器 |
| 新建 | `internal/loader/loader_test.go` | 全格式集成测试 |

## T1: 项目初始化

**文件：** `go.mod`、目录结构
**依赖：** 无
**步骤：**
1. 执行 `go mod init github.com/Bin-hy/bin-rag`
2. 创建目录 `internal/loader/parsers/`
3. 添加依赖：goldmark、excelize、go-docx、pdfcpu、golang.org/x/net

**验证：** `go mod tidy` 无报错

## T2: 核心类型定义

**文件：** `internal/loader/types.go`
**依赖：** T1
**步骤：**
1. 定义 `BlockType` 常量枚举（BlockParagraph、BlockHeading、BlockListItem、BlockCode、BlockTable）
2. 定义 `Block` 结构体（Type、Content、Level、Metadata）
3. 定义 `Document` 结构体（Blocks、Metadata）
4. 定义 `DocumentMeta` 结构体（Filename、Format、Size、Title、PageCount、Extra）
5. 定义 `FileInfo` 结构体（Filename、MIMEType、Size）
6. 定义 `ErrorMode` 枚举（ModeTolerant、ModeStrict）
7. 定义 `LoadOptions` 结构体（Mode）
8. 定义 `LoadResult` 结构体（Document、Warnings）

**验证：** `go build ./internal/loader/...` 编译通过

## T3: Parser 接口定义

**文件：** `internal/loader/parser.go`
**依赖：** T2
**步骤：**
1. 定义 `Parser` 接口，包含三个方法：
   - `Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error)`
   - `SupportedExts() []string`
   - `SupportedMIMEs() []string`

**验证：** `go build ./internal/loader/...` 编译通过

## T4: 错误定义

**文件：** `internal/loader/errors.go`
**依赖：** T2
**步骤：**
1. 定义 `ErrUnsupportedFormat` 错误类型，包含 Filename 和 MIMEType 字段
2. 实现 `Error() string` 方法，输出清晰的错误信息
3. 定义 `ErrParseFailed` 错误类型，包含 Format 和 Cause 字段

**验证：** `go build ./internal/loader/...` 编译通过

## T5: Registry 实现

**文件：** `internal/loader/registry.go`
**依赖：** T3、T4
**步骤：**
1. 定义 `Registry` 接口（Register、Resolve）
2. 定义 `defaultRegistry` 结构体，内含 `extMap map[string]Parser` 和 `mimeMap map[string]Parser`
3. 实现 `NewRegistry() Registry` 工厂函数
4. 实现 `Register`：遍历 Parser 的 SupportedExts 和 SupportedMIMEs，写入对应 map
5. 实现 `Resolve`：从 FileInfo.Filename 提取扩展名查 extMap，无结果则查 mimeMap，都无返回 ErrUnsupportedFormat

**验证：** `go build ./internal/loader/...` 编译通过

## T6: Loader 实现

**文件：** `internal/loader/loader.go`
**依赖：** T5
**步骤：**
1. 定义 `Loader` 接口（Load 方法）
2. 定义 `defaultLoader` 结构体，持有 `registry Registry`
3. 实现 `Load`：合并默认 opts → 调用 registry.Resolve → 调用 parser.Parse → 返回结果
4. 实现 `NewLoader() Loader` 工厂函数（内部创建 Registry 并注册所有内置 Parser）

**验证：** `go build ./internal/loader/...` 编译通过

## T7: TXT 解析器

**文件：** `internal/loader/parsers/txt.go`
**依赖：** T3
**步骤：**
1. 定义 `txtParser` 结构体
2. 实现 `SupportedExts` 返回 `[".txt"]`
3. 实现 `SupportedMIMEs` 返回 `["text/plain"]`
4. 实现 `Parse`：用 bufio.Scanner 按行读取，连续非空行合并为一个 BlockParagraph，空行作为段落分隔
5. 填充 DocumentMeta（Filename、Format="txt"、Size）
6. 实现 `NewTxtParser() Parser` 构造函数

**验证：** 编写单元测试，输入含多段落的文本 bytes.Reader，验证输出 Block 数量和类型正确

## T8: Markdown 解析器

**文件：** `internal/loader/parsers/markdown.go`
**依赖：** T3
**步骤：**
1. 定义 `markdownParser` 结构体
2. 实现 `SupportedExts` 返回 `[".md", ".markdown"]`
3. 实现 `SupportedMIMEs` 返回 `["text/markdown"]`
4. 实现 `Parse`：
   - 读取全部内容到 []byte
   - 用 goldmark 解析为 AST
   - 遍历 AST 节点：Heading → BlockHeading（带 Level），Paragraph → BlockParagraph，List → 逐项 BlockListItem，FencedCodeBlock → BlockCode
5. 提取第一个 h1 作为 Title
6. 实现 `NewMarkdownParser() Parser`

**验证：** 单元测试输入含 h1/h2/段落/列表/代码块的 Markdown，验证各 Block 类型和 Level 正确

## T9: PDF 解析器

**文件：** `internal/loader/parsers/pdf.go`
**依赖：** T3
**步骤：**
1. 定义 `pdfParser` 结构体
2. 实现 `SupportedExts` 返回 `[".pdf"]`
3. 实现 `SupportedMIMEs` 返回 `["application/pdf"]`
4. 实现 `Parse`：
   - 将 reader 内容读入内存（PDF 库需要 io.ReadSeeker）
   - 用 PDF 库逐页提取文本
   - 每页文本作为一个 BlockParagraph（后续可按段落再拆）
   - 宽容模式下单页解析失败记录 warning 并跳过
5. 填充 PageCount
6. 实现 `NewPdfParser() Parser`

**验证：** 单元测试输入一个简单 PDF 的字节内容，验证能提取文本（可用测试辅助函数生成最小 PDF）

## T10: DOCX 解析器

**文件：** `internal/loader/parsers/docx.go`
**依赖：** T3
**步骤：**
1. 定义 `docxParser` 结构体
2. 实现 `SupportedExts` 返回 `[".docx"]`
3. 实现 `SupportedMIMEs` 返回 `["application/vnd.openxmlformats-officedocument.wordprocessingml.document"]`
4. 实现 `Parse`：
   - 读取 reader 到 bytes（docx 库需要完整数据）
   - 用 go-docx 打开并遍历段落
   - 根据段落样式（Heading1/2/3）生成 BlockHeading，普通段落生成 BlockParagraph
5. 实现 `NewDocxParser() Parser`

**验证：** 单元测试输入一个最小 DOCX（ZIP 结构），验证段落提取正确

## T11: CSV 解析器

**文件：** `internal/loader/parsers/csv.go`
**依赖：** T3
**步骤：**
1. 定义 `csvParser` 结构体
2. 实现 `SupportedExts` 返回 `[".csv"]`
3. 实现 `SupportedMIMEs` 返回 `["text/csv"]`
4. 实现 `Parse`：
   - 用 encoding/csv 读取所有行
   - 第一行作为表头，生成一个 BlockHeading（Level=1，内容为列名逗号连接）
   - 其余每行生成 BlockTable 类型的 Block（内容为各字段 tab 连接）
5. 实现 `NewCsvParser() Parser`

**验证：** 单元测试输入 3 行 CSV 字符串，验证输出 1 个表头 Block + 2 个数据 Block

## T12: Excel 解析器

**文件：** `internal/loader/parsers/excel.go`
**依赖：** T3
**步骤：**
1. 定义 `excelParser` 结构体
2. 实现 `SupportedExts` 返回 `[".xlsx", ".xls"]`
3. 实现 `SupportedMIMEs` 返回 `["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"]`
4. 实现 `Parse`：
   - 用 excelize 从 reader 打开工作簿
   - 遍历所有 Sheet，每个 Sheet 名生成一个 BlockHeading（Level=1）
   - 每行数据生成一个 BlockTable Block
5. 实现 `NewExcelParser() Parser`

**验证：** 单元测试用 excelize 在内存中创建最小 xlsx，再传给解析器验证输出

## T13: HTML 解析器

**文件：** `internal/loader/parsers/html.go`
**依赖：** T3
**步骤：**
1. 定义 `htmlParser` 结构体
2. 实现 `SupportedExts` 返回 `[".html", ".htm"]`
3. 实现 `SupportedMIMEs` 返回 `["text/html"]`
4. 实现 `Parse`：
   - 用 golang.org/x/net/html 解析为 DOM 树
   - 递归遍历节点：h1-h6 → BlockHeading，p → BlockParagraph，li → BlockListItem，pre/code → BlockCode
   - 跳过 script/style/nav/footer 标签
5. 实现 `NewHtmlParser() Parser`

**验证：** 单元测试输入含标题/段落/列表的 HTML 字符串，验证 Block 类型正确

## T14: 集成测试

**文件：** `internal/loader/loader_test.go`
**依赖：** T6 - T13
**步骤：**
1. 测试 `NewLoader()` 构造后所有内置格式可用
2. 测试每种格式通过 `Loader.Load` 正常加载（使用内存数据源）
3. 测试未知格式返回 `ErrUnsupportedFormat`
4. 测试宽容模式下损坏输入返回部分内容 + Warnings
5. 测试严格模式下损坏输入返回 error
6. 测试自定义 Parser 注册后可被 Resolve

**验证：** `go test ./internal/loader/... -v` 全部通过

## 执行顺序

```
T1 → T2 → T3（与 T4 可并行）
            ↘
             T4
            ↗
      T5 → T6

T7 ~ T13（互相独立，可并行，均依赖 T3）

T14（依赖 T6 ~ T13 全部完成）
```
