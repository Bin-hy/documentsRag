package loader

import (
	"context"
	"io"
)

// BlockType 内容块类型
type BlockType int

const (
	BlockParagraph        BlockType = iota // 普通段落
	BlockHeading                           // 标题
	BlockListItem                          // 列表项
	BlockCode                              // 代码块
	BlockTable                             // 表格
	BlockImageDescription                  // 图片/视频帧视觉描述（多媒体，预留多模态检索标记）
	BlockAudioSegment                      // 音频转写分段（多媒体，含起止时间戳 metadata）
)

// Block 文档的最小结构单元
type Block struct {
	Type     BlockType
	Content  string
	Level    int            // 仅标题有效，1-6
	Metadata map[string]any // 扩展字段
}

// Document 加载器的输出
type Document struct {
	Blocks   []Block
	Metadata DocumentMeta
}

// DocumentMeta 文档级元数据
type DocumentMeta struct {
	Filename  string
	Format    string
	Size      int64
	Title     string
	PageCount int
	Extra     map[string]any
}

// FileInfo 调用方提供的文件元信息
type FileInfo struct {
	Filename string
	MIMEType string
	Size     int64
}

// ErrorMode 错误处理模式
type ErrorMode int

const (
	ModeTolerant ErrorMode = iota // 宽容模式（默认）
	ModeStrict                    // 严格模式
)

// LoadOptions 加载配置
type LoadOptions struct {
	Mode     ErrorMode
	Filename string // 来源文件名（多媒体 parser 写入 Block.Metadata["source"] / Extra）
}

// LoadResult 加载结果
type LoadResult struct {
	Document *Document
	Warnings []string
}

// Parser 格式解析器接口
type Parser interface {
	Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error)
	SupportedExts() []string
	SupportedMIMEs() []string
}

// Registry 解析器注册表接口
type Registry interface {
	Register(parser Parser)
	Resolve(info FileInfo) (Parser, error)
	// Support 判断单个文件当前是否可处理（格式识别 + 能力配置），返回结果与不支持原因。
	Support(info FileInfo) SupportResult
	// SupportedTypes 枚举当前注册表全部扩展名的支持状态（稳定按 ext 升序）。
	SupportedTypes() []SupportedType
}

// Loader 文档加载器主接口
type Loader interface {
	Load(ctx context.Context, reader io.Reader, info FileInfo, opts ...LoadOptions) (*LoadResult, error)
}
