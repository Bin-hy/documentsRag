# 文档分块器 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/chunker/types.go` | Chunk、ChunkMeta、ChunkerConfig、StrategyType 类型定义 |
| 新建 | `internal/chunker/tokenizer.go` | Tokenizer 接口 + DefaultTokenizer 实现 |
| 新建 | `internal/chunker/strategy.go` | Strategy 接口定义 |
| 新建 | `internal/chunker/strategy_fixed.go` | FixedSizeStrategy 实现 |
| 新建 | `internal/chunker/strategy_recursive.go` | RecursiveStrategy 实现 |
| 新建 | `internal/chunker/strategy_heading.go` | HeadingStrategy 实现 |
| 新建 | `internal/chunker/chunker.go` | Chunker 接口 + defaultChunker 实现 + NewChunker |
| 新建 | `internal/chunker/chunker_test.go` | 全策略测试 |

## T1: 类型定义

**文件：** `internal/chunker/types.go`
**依赖：** 无
**步骤：**
1. 创建 `internal/chunker/` 目录
2. 定义 `StrategyType` 枚举（StrategyFixed、StrategyRecursive、StrategyHeading）
3. 定义 `ChunkerConfig` 结构体（Strategy、ChunkSize、ChunkOverlap、HeadingLevel）
4. 为 ChunkerConfig 提供 `WithDefaults()` 方法，填充默认值（512/50/2）
5. 定义 `ChunkMeta` 结构体（DocFilename、HeadingContext、TokenCount）
6. 定义 `Chunk` 结构体（Content、Index、Metadata）

**验证：** `go build ./internal/chunker/...` 编译通过

## T2: Tokenizer 实现

**文件：** `internal/chunker/tokenizer.go`
**依赖：** T1
**步骤：**
1. 定义 `Tokenizer` 接口，包含 `Count(text string) int` 方法
2. 定义 `DefaultTokenizer` 结构体
3. 实现 `Count`：遍历 rune，中文字符（unicode.Is(unicode.Han, r)）计 2，标点计 1，连续 ASCII 非空白字符按空格分词后每词计 1
4. 实现 `NewDefaultTokenizer() Tokenizer`

**验证：** 编写单元测试，"你好world" 应计为 2×2 + 1 = 5 token

## T3: Strategy 接口

**文件：** `internal/chunker/strategy.go`
**依赖：** T1、T2
**步骤：**
1. 定义 `Strategy` 接口：`Split(text string, config ChunkerConfig, tokenizer Tokenizer) []string`

**验证：** `go build ./internal/chunker/...` 编译通过

## T4: FixedSizeStrategy

**文件：** `internal/chunker/strategy_fixed.go`
**依赖：** T3
**步骤：**
1. 定义 `fixedSizeStrategy` 结构体
2. 实现 `Split`：
   - 将文本按 rune 遍历，累积到 builder 中
   - 每累积一定量字符后调用 tokenizer.Count 检查是否超过 ChunkSize
   - 超过时回退到最近的空白字符（空格/换行）位置切断
   - 下一个 chunk 从当前切断点回退 overlap token 对应的字符数处开始
3. 实现 `NewFixedSizeStrategy() Strategy`

**验证：** 单元测试输入 1000 token 文本 + ChunkSize=200，验证每个 chunk ≤ 200 token

## T5: RecursiveStrategy

**文件：** `internal/chunker/strategy_recursive.go`
**依赖：** T3
**步骤：**
1. 定义 `recursiveStrategy` 结构体
2. 定义分隔符列表：`["\n\n", "\n", "。", "！", "？", ".", "!", "?", " ", ""]`
3. 实现 `Split`：
   - 调用内部递归函数 `splitRecursive(text, separators, config, tokenizer)`
   - 用当前最高优先级分隔符 strings.Split 拆分文本
   - 逐段检查 token 数：≤ ChunkSize 的段保留；> ChunkSize 的段用下一级分隔符递归拆
   - 最后对所有段做合并：相邻小段合并到不超过 ChunkSize 为止
   - Overlap：合并完成后，相邻 chunk 间附加 overlap 部分
4. 实现 `NewRecursiveStrategy() Strategy`

**验证：** 单元测试输入含多个段落（\n\n 分隔）的长文本，验证优先在段落边界切分，不在词中间断开

## T6: HeadingStrategy

**文件：** `internal/chunker/strategy_heading.go`
**依赖：** T3、T5
**步骤：**
1. 定义 `headingStrategy` 结构体，内持 `fallback Strategy`（RecursiveStrategy）
2. 实现 `SplitByBlocks` 方法（非 Strategy 接口方法，Chunker 直接调用）：
   - 输入 `[]loader.Block`、config、tokenizer
   - 输出 `[]headingSection`（每个 section 包含 Content、HeadingContext）
   - 扫描 Block 列表，遇到 Level ≤ HeadingLevel 的 Heading 开始新 section
   - 维护标题栈构建 HeadingContext
   - 对每个 section 检查 token 数，超长时调用 fallback.Split 降级拆分
3. 实现 `NewHeadingStrategy() *headingStrategy`

**验证：** 单元测试输入含 h1/h2/h3 和段落的 Block 列表，按 h2 切分后验证每个 section 的 HeadingContext 正确

## T7: Chunker 实现

**文件：** `internal/chunker/chunker.go`
**依赖：** T4、T5、T6
**步骤：**
1. 定义 `Chunker` 接口：`Chunk(doc *loader.Document, config ChunkerConfig) []Chunk`
2. 定义 `defaultChunker` 结构体（tokenizer、strategies map、headingStrategy）
3. 实现 `NewChunker(tokenizer Tokenizer) Chunker`：
   - tokenizer 为 nil 时使用 DefaultTokenizer
   - 注册 FixedSizeStrategy 和 RecursiveStrategy 到 strategies map
   - 构造 headingStrategy
4. 实现 `RegisterStrategy(t StrategyType, s Strategy)` 方法
5. 实现 `Chunk`：
   - 调用 config.WithDefaults() 填充默认值
   - 如果策略是 StrategyHeading：调用 headingStrategy.SplitByBlocks
   - 否则：将 Blocks 拼接为文本（blocksToText 辅助函数），调用对应 Strategy.Split
   - 组装 []Chunk：填充 Content、Index、Metadata（DocFilename、HeadingContext、TokenCount）
6. 实现 `blocksToText`：Heading → "## " 前缀（按 Level 加 # 数量），ListItem → "- " 前缀，其余直接拼接，用 "\n\n" 连接 Block

**验证：** `go build ./internal/chunker/...` 编译通过

## T8: 集成测试

**文件：** `internal/chunker/chunker_test.go`
**依赖：** T7
**步骤：**
1. 测试固定大小策略：输入大文档，验证每个 chunk token 数 ≤ ChunkSize
2. 测试 overlap：ChunkOverlap=50，验证相邻 chunk 有约 50 token 重叠
3. 测试递归字符策略：输入多段落文本，验证优先段落边界切分
4. 测试 Markdown 标题策略：输入含 h1/h2 + 段落的 Document，验证按 h2 切分、HeadingContext 正确
5. 测试来源追溯：验证每个 Chunk 的 Index 递增、DocFilename 正确
6. 测试自定义 Tokenizer：注入 mock tokenizer，验证按自定义计数切分
7. 测试自定义策略注册：注册 mock strategy，通过 config 选择并验证被调用

**验证：** `go test ./internal/chunker/... -v` 全部通过

## 执行顺序

```
T1 → T2 → T3 → T4（与 T5 可并行）
                  ↘
                   T5 → T6
                        ↗
                 T7（依赖 T4、T5、T6）
                  ↓
                 T8
```
