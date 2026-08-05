package chunker

import (
	"fmt"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

type headingSection struct {
	Content        string
	HeadingContext string
}

type headingStrategy struct {
	fallback Strategy
}

func NewHeadingStrategy() *headingStrategy {
	return &headingStrategy{
		fallback: NewRecursiveStrategy(),
	}
}

// SplitByBlocks 按标题层级切分 Block 列表
func (s *headingStrategy) SplitByBlocks(blocks []loader.Block, config ChunkerConfig, tokenizer Tokenizer) []headingSection {
	if len(blocks) == 0 {
		return nil
	}

	targetLevel := config.HeadingLevel
	var sections []headingSection
	var currentContent strings.Builder
	headingStack := make([]string, 0)

	flushSection := func() {
		content := strings.TrimSpace(currentContent.String())
		if content != "" {
			ctx := strings.Join(headingStack, " > ")
			section := headingSection{
				Content:        content,
				HeadingContext: ctx,
			}

			// 超长降级
			if tokenizer.Count(content) > config.ChunkSize {
				subChunks := s.fallback.Split(content, config, tokenizer)
				for _, sub := range subChunks {
					sections = append(sections, headingSection{
						Content:        sub,
						HeadingContext: ctx,
					})
				}
			} else {
				sections = append(sections, section)
			}
		}
		currentContent.Reset()
	}

	for _, block := range blocks {
		if block.Type == loader.BlockHeading && block.Level <= targetLevel {
			flushSection()
			updateHeadingStack(&headingStack, block.Content, block.Level)
			currentContent.WriteString(blockToText(block))
			currentContent.WriteString("\n\n")
		} else {
			currentContent.WriteString(blockToText(block))
			currentContent.WriteString("\n\n")
		}
	}

	flushSection()
	return sections
}

func updateHeadingStack(stack *[]string, title string, level int) {
	// 保留层级小于当前的标题
	for len(*stack) >= level {
		*stack = (*stack)[:len(*stack)-1]
	}
	*stack = append(*stack, title)
}

func blockToText(block loader.Block) string {
	switch block.Type {
	case loader.BlockHeading:
		prefix := strings.Repeat("#", block.Level)
		return fmt.Sprintf("%s %s", prefix, block.Content)
	case loader.BlockListItem:
		return "- " + block.Content
	case loader.BlockCode:
		return "```\n" + block.Content + "\n```"
	default:
		return block.Content
	}
}
