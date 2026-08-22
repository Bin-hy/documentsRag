package chunker

import "strings"

// StrategyType 分块策略类型
type StrategyType int

const (
	StrategyFixed     StrategyType = iota // 固定大小
	StrategyRecursive                     // 递归字符
	StrategyHeading                       // Markdown 标题
)

// ParseStrategy 将配置字符串（yaml）映射为策略类型；未知值回退 recursive
func ParseStrategy(name string) StrategyType {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fixed":
		return StrategyFixed
	case "heading":
		return StrategyHeading
	case "recursive", "":
		return StrategyRecursive
	default:
		return StrategyRecursive
	}
}

// ChunkerConfig 分块配置
type ChunkerConfig struct {
	Strategy     StrategyType
	ChunkSize    int // 目标 chunk 大小（token 数），默认 512
	ChunkOverlap int // 重叠大小（token 数），默认 50
	HeadingLevel int // Markdown 标题分块的目标层级，默认 2
}

// WithDefaults 填充默认值
func (c ChunkerConfig) WithDefaults() ChunkerConfig {
	if c.ChunkSize <= 0 {
		c.ChunkSize = 512
	}
	if c.ChunkOverlap < 0 {
		c.ChunkOverlap = 50
	}
	if c.HeadingLevel <= 0 {
		c.HeadingLevel = 2
	}
	return c
}

// ChunkMeta 来源追溯信息
type ChunkMeta struct {
	DocFilename    string // 源文档文件名
	HeadingContext string // 所属标题上下文路径
	TokenCount     int    // 该 Chunk 的 token 数
	SourceType     string // 来源类型："video" | "audio" | "image" | ""（文本类为空）
	StartMs        int64  // 视频/音频定位起始时间戳（毫秒）
	EndMs          int64  // 定位结束时间戳（毫秒）
	PageNumber     int    // PDF 页码（从 1 开始，非 PDF 为 0）
	Heading        string // Markdown 当前块最近一级标题文本
	Anchor         string // 标题锚点（slugifyHeading 后的 heading）
}

// Chunk 分块输出
type Chunk struct {
	Content  string
	Index    int
	Metadata ChunkMeta
}
