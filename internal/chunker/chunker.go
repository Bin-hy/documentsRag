package chunker

import (
	"strings"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// Chunker 分块器主接口
type Chunker interface {
	Chunk(doc *loader.Document, config ChunkerConfig) []Chunk
	RegisterStrategy(t StrategyType, s Strategy)
}

type defaultChunker struct {
	tokenizer       Tokenizer
	strategies      map[StrategyType]Strategy
	headingStrategy *headingStrategy
}

// NewChunker 创建分块器，tokenizer 为 nil 时使用 DefaultTokenizer
func NewChunker(tokenizer Tokenizer) Chunker {
	if tokenizer == nil {
		tokenizer = NewDefaultTokenizer()
	}

	return &defaultChunker{
		tokenizer: tokenizer,
		strategies: map[StrategyType]Strategy{
			StrategyFixed:     NewFixedSizeStrategy(),
			StrategyRecursive: NewRecursiveStrategy(),
		},
		headingStrategy: NewHeadingStrategy(),
	}
}

func (c *defaultChunker) RegisterStrategy(t StrategyType, s Strategy) {
	c.strategies[t] = s
}

func (c *defaultChunker) Chunk(doc *loader.Document, config ChunkerConfig) []Chunk {
	config = config.WithDefaults()

	if doc == nil || len(doc.Blocks) == 0 {
		return nil
	}

	// 多媒体 Document：按块切分（每 Image/Audio Block 一个 chunk，时间戳 1:1，spec F7）
	if isMediaDoc(doc.Blocks) {
		return c.chunkMediaBlocks(doc)
	}

	// PDF Document：按页分块（block 带 page metadata，spec AC7）
	if isPagedDoc(doc.Blocks) {
		return c.chunkPagedBlocks(doc, config)
	}

	var rawChunks []rawChunk

	if config.Strategy == StrategyHeading {
		sections := c.headingStrategy.SplitByBlocks(doc.Blocks, config, c.tokenizer)
		for _, sec := range sections {
			rawChunks = append(rawChunks, rawChunk{
				content:        sec.Content,
				headingContext: sec.HeadingContext,
				heading:        sec.Heading,
				anchor:         sec.Anchor,
			})
		}
	} else {
		strategy, ok := c.strategies[config.Strategy]
		if !ok {
			strategy = c.strategies[StrategyRecursive]
		}

		text := blocksToText(doc.Blocks)
		headingCtx := extractFirstHeadingContext(doc.Blocks)
		parts := strategy.Split(text, config, c.tokenizer)
		for _, part := range parts {
			rawChunks = append(rawChunks, rawChunk{
				content:        part,
				headingContext: headingCtx,
			})
		}
	}

	// 组装最终 Chunk
	chunks := make([]Chunk, 0, len(rawChunks))
	for i, rc := range rawChunks {
		chunks = append(chunks, Chunk{
			Content: rc.content,
			Index:   i,
			Metadata: ChunkMeta{
				DocFilename:    doc.Metadata.Filename,
				HeadingContext: rc.headingContext,
				TokenCount:     c.tokenizer.Count(rc.content),
				Heading:        rc.heading,
				Anchor:         rc.anchor,
				PageNumber:     rc.pageNumber,
			},
		})
	}

	return chunks
}

type rawChunk struct {
	content        string
	headingContext string
	heading        string
	anchor         string
	pageNumber     int
}

func blocksToText(blocks []loader.Block) string {
	var parts []string
	for _, b := range blocks {
		parts = append(parts, blockToText(b))
	}
	return strings.Join(parts, "\n\n")
}

// isMediaDoc 判断 Document 是否含多媒体 block（ImageDescription / AudioSegment）
func isMediaDoc(blocks []loader.Block) bool {
	for _, b := range blocks {
		switch b.Type {
		case loader.BlockImageDescription, loader.BlockAudioSegment:
			return true
		}
	}
	return false
}

// chunkMediaBlocks 多媒体按块切分：每个 Image/Audio Block 独立成一个 chunk，
// 时间戳取自 block metadata（spec F7：时间戳作为视频定位锚点 1:1 贯通）
func (c *defaultChunker) chunkMediaBlocks(doc *loader.Document) []Chunk {
	// 来源类型优先按文档格式判定：视频文档的音轨转写 chunk 也归为 video（否则会被误判成 audio）
	docType := doc.Metadata.Format
	chunks := make([]Chunk, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if strings.TrimSpace(b.Content) == "" {
			continue
		}
		sourceType := docType
		if sourceType == "" {
			sourceType = mediaSourceType(b) // 兜底：文档格式未设置时按 block 推断
		}
		startMs, endMs := mediaTimeRange(b)
		chunks = append(chunks, Chunk{
			Content: b.Content,
			Index:   len(chunks),
			Metadata: ChunkMeta{
				DocFilename: doc.Metadata.Filename,
				TokenCount:  c.tokenizer.Count(b.Content),
				SourceType:  sourceType,
				StartMs:     startMs,
				EndMs:       endMs,
			},
		})
	}
	return chunks
}

// isPagedDoc 判断文档是否按页产出（PDF：block 带 page metadata）
func isPagedDoc(blocks []loader.Block) bool {
	for _, b := range blocks {
		if _, ok := b.Metadata["page"]; ok {
			return true
		}
	}
	return false
}

// chunkPagedBlocks PDF 按页分块：同页 block 合并为一个 chunk，PageNumber=page；
// 单页超长时用 recursive 切分，子 chunk 沿用同一 PageNumber。
func (c *defaultChunker) chunkPagedBlocks(doc *loader.Document, config ChunkerConfig) []Chunk {
	// 按页码分组（保持文档原有顺序）
	type pageGroup struct {
		page   int
		blocks []loader.Block
	}
	var groups []pageGroup
	indexByPage := map[int]int{}
	for _, b := range doc.Blocks {
		page, _ := b.Metadata["page"].(int)
		if page <= 0 {
			page = 1
		}
		idx, ok := indexByPage[page]
		if !ok {
			idx = len(groups)
			indexByPage[page] = idx
			groups = append(groups, pageGroup{page: page})
		}
		groups[idx].blocks = append(groups[idx].blocks, b)
	}

	fallback := NewRecursiveStrategy()
	var chunks []Chunk
	for _, g := range groups {
		text := strings.TrimSpace(blocksToText(g.blocks))
		if text == "" {
			continue
		}
		parts := []string{text}
		if c.tokenizer.Count(text) > config.ChunkSize {
			parts = fallback.Split(text, config, c.tokenizer)
		}
		for _, part := range parts {
			chunks = append(chunks, Chunk{
				Content: part,
				Index:   len(chunks),
				Metadata: ChunkMeta{
					DocFilename: doc.Metadata.Filename,
					TokenCount:  c.tokenizer.Count(part),
					PageNumber:  g.page,
				},
			})
		}
	}
	return chunks
}

// mediaSourceType 从 block 类型与 metadata 推导来源类型
func mediaSourceType(b loader.Block) string {
	switch b.Type {
	case loader.BlockAudioSegment:
		return "audio"
	case loader.BlockImageDescription:
		if mt, _ := b.Metadata["media_type"].(string); mt == "video_frame" {
			return "video"
		}
		return "image"
	}
	return ""
}

// mediaTimeRange 从 block metadata 读起止时间戳（毫秒）
func mediaTimeRange(b loader.Block) (int64, int64) {
	start, _ := b.Metadata["start_ms"].(int64)
	end, _ := b.Metadata["end_ms"].(int64)
	return start, end
}

func extractFirstHeadingContext(blocks []loader.Block) string {
	var stack []string
	for _, b := range blocks {
		if b.Type == loader.BlockHeading {
			for len(stack) >= b.Level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, b.Content)
		}
	}
	if len(stack) > 0 {
		return strings.Join(stack, " > ")
	}
	return ""
}

// slugifyHeading 将标题文本转为 HTML 锚点（与前端 MarkdownViewer 规则一致）：
// 去首尾空白，移除 Markdown 特殊字符，内部连续空白替换为 '-'；中文原样保留（HTML5 id 允许中文）。
func slugifyHeading(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '#', '*', '_', '`', '[', ']', '(', ')', '>':
			continue
		case ' ', '\t', '\n', '\r':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}
