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

	var rawChunks []rawChunk

	if config.Strategy == StrategyHeading {
		sections := c.headingStrategy.SplitByBlocks(doc.Blocks, config, c.tokenizer)
		for _, sec := range sections {
			rawChunks = append(rawChunks, rawChunk{
				content:        sec.Content,
				headingContext: sec.HeadingContext,
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
			},
		})
	}

	return chunks
}

type rawChunk struct {
	content        string
	headingContext string
}

func blocksToText(blocks []loader.Block) string {
	var parts []string
	for _, b := range blocks {
		parts = append(parts, blockToText(b))
	}
	return strings.Join(parts, "\n\n")
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
