# 文档加载器 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] 项目可编译（验证：`go build ./...` 无错误）
- [ ] 所有 7 种格式解析器已实现并注册（验证：NewLoader() 后 Resolve 每种格式均返回对应 Parser）

## 格式加载验证（AC1）

- [ ] TXT 文件加载后输出多个 BlockParagraph（验证：输入含 3 段文本，输出 3 个 Block）
- [ ] Markdown 加载后标题层级正确（验证：输入 h1/h2/h3，输出 Block 的 Level 分别为 1/2/3）（AC3）
- [ ] Markdown 加载后段落、列表、代码块类型正确（验证：检查 BlockType 枚举值）
- [ ] PDF 加载后每页生成内容块（验证：输入 2 页 PDF，输出至少 2 个 Block）
- [ ] DOCX 加载后段落和标题正确区分（验证：含 Heading1 样式的段落输出为 BlockHeading）
- [ ] CSV 加载后表头和数据行分离（验证：3 行 CSV 输出 1 个 Heading + 2 个 Table Block）
- [ ] Excel 加载后按 Sheet 组织（验证：2 个 Sheet 各输出一个 Heading Block）
- [ ] HTML 加载后正确提取结构（验证：含 h1/p/ul 的 HTML 输出对应类型 Block）

## 输入接口（AC2）

- [ ] 使用 bytes.Reader 作为输入源可正常工作（验证：所有测试均使用内存数据源，无文件路径依赖）

## 元数据提取（F4）

- [ ] 输出的 DocumentMeta 包含 Filename、Format、Size 字段（验证：加载后检查字段非空）
- [ ] Markdown 文件提取首个 h1 作为 Title（验证：输入含 `# 标题` 的 MD，Meta.Title == "标题"）
- [ ] PDF 文件包含 PageCount（验证：2 页 PDF 加载后 Meta.PageCount == 2）

## 格式自动识别（F5）

- [ ] 根据扩展名自动选择 Parser（验证：传入 FileInfo{Filename: "a.md"} 自动使用 Markdown Parser）
- [ ] 扩展名缺失时降级 MIME 匹配（验证：传入 FileInfo{Filename: "a", MIMEType: "text/markdown"} 正常工作）

## 错误处理（AC4、AC6）

- [ ] 未知格式返回 ErrUnsupportedFormat（验证：传入 .xyz 文件，errors.As 断言成功）
- [ ] 宽容模式下损坏输入返回部分内容 + Warnings（验证：传入损坏 PDF，result.Document 非 nil 且 Warnings 非空）
- [ ] 严格模式下损坏输入返回 error（验证：传入损坏 PDF + ModeStrict，返回 error 非 nil）

## 可扩展性（AC5）

- [ ] 注册自定义 Parser 后可被使用（验证：实现一个 mock Parser 支持 ".custom"，注册后 Load 该格式成功）

## 编译与测试

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./internal/loader/... -v` 全部通过（AC7）
- [ ] 测试不依赖外部文件（所有输入为内存构造的 bytes.Reader）

## 端到端场景

- [ ] 场景 1：完整加载流程 — 调用 `NewLoader()`，传入一个 Markdown 文件的 bytes.Reader + FileInfo，获得包含正确标题层级和段落的 Document 对象
- [ ] 场景 2：格式不支持 — 传入 FileInfo{Filename: "data.unknown"}，返回 ErrUnsupportedFormat，错误信息包含文件名
- [ ] 场景 3：宽容加载损坏文件 — 传入部分损坏的输入 + 默认选项，返回已解析的内容和警告列表
