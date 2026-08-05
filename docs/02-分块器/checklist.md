# 文档分块器 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] 项目可编译（验证：`go build ./...` 无错误）
- [ ] 3 种分块策略均已实现并注册（验证：NewChunker() 后使用每种策略均不 panic）

## 固定大小分块（AC1、AC2）

- [ ] 每个 Chunk 的 token 数不超过 ChunkSize（验证：输入长文本 + ChunkSize=200，逐个检查）
- [ ] 相邻 Chunk 存在约 overlap token 的重叠（验证：ChunkOverlap=50，检查相邻 chunk 尾首重叠内容）

## 递归字符分块（AC3）

- [ ] 优先在段落边界（\n\n）处切分（验证：输入含 \n\n 分隔的多段落，切分点在段落间）
- [ ] 不在词中间断开（验证：检查每个 chunk 的首尾不包含被截断的中英文词）
- [ ] 每个 Chunk token 数不超过 ChunkSize（验证：逐个检查）

## Markdown 标题分块（AC4）

- [ ] 按 h2 切分时每个 Chunk 包含一个完整的 h2 节内容（验证：输入含 3 个 h2 节，输出 3 个 Chunk）
- [ ] Chunk 元数据包含标题上下文路径（验证：HeadingContext 为 "主标题 > 二级标题" 格式）
- [ ] 超长节降级拆分后每个 Chunk 仍不超过 ChunkSize（验证：输入超长 h2 节，子 chunk token ≤ ChunkSize）

## 来源追溯（AC5）

- [ ] 每个 Chunk 携带源文档文件名（验证：Metadata.DocFilename == Document.Metadata.Filename）
- [ ] 每个 Chunk 的 Index 从 0 递增（验证：检查 Index 序列为 0,1,2,...）
- [ ] HeadingContext 非空（Markdown 标题策略下）（验证：检查 HeadingContext 不为空字符串）

## Tokenizer 可替换（AC6）

- [ ] 注入自定义 Tokenizer 后按自定义计数切分（验证：mock tokenizer 每字符计 1 token，ChunkSize=10，10 字符文本输出 1 chunk）

## 策略可扩展（AC7）

- [ ] 注册自定义策略后可被使用（验证：注册 mock strategy，config 选该策略，验证 mock 被调用）

## 编译与测试

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./internal/chunker/... -v` 全部通过（AC8）
- [ ] 测试使用 mock Tokenizer 验证逻辑正确

## 端到端场景

- [ ] 场景 1：完整分块流程 — NewChunker(nil) + RecursiveStrategy + 含 h1/h2/段落的 Document → 输出 Chunk 列表，每个 Chunk 内容非空、token 数合理、Index 递增
- [ ] 场景 2：标题分块 — 含 3 个 h2 节的 Document + StrategyHeading → 输出 3 个 Chunk，各 HeadingContext 正确
- [ ] 场景 3：超长降级 — 一个 h2 节内容超 ChunkSize → 自动降级拆分为多个 Chunk，每个 ≤ ChunkSize
