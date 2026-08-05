package loader

import (
	"bufio"
	"context"
	"io"
	"strings"
)

type txtParser struct{}

func NewTxtParser() Parser {
	return &txtParser{}
}

func (p *txtParser) SupportedExts() []string {
	return []string{".txt"}
}

func (p *txtParser) SupportedMIMEs() []string {
	return []string{"text/plain"}
}

func (p *txtParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	var blocks []Block
	var current strings.Builder

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if current.Len() > 0 {
				blocks = append(blocks, Block{
					Type:    BlockParagraph,
					Content: strings.TrimSpace(current.String()),
				})
				current.Reset()
			}
		} else {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
		}
	}

	if current.Len() > 0 {
		blocks = append(blocks, Block{
			Type:    BlockParagraph,
			Content: strings.TrimSpace(current.String()),
		})
	}

	if err := scanner.Err(); err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "txt", Cause: err}
		}
		return &LoadResult{
			Document: &Document{
				Blocks:   blocks,
				Metadata: DocumentMeta{Format: "txt"},
			},
			Warnings: []string{err.Error()},
		}, nil
	}

	return &LoadResult{
		Document: &Document{
			Blocks:   blocks,
			Metadata: DocumentMeta{Format: "txt"},
		},
	}, nil
}
