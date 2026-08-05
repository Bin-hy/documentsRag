package loader

import (
	"bytes"
	"context"
	"io"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type markdownParser struct{}

func NewMarkdownParser() Parser {
	return &markdownParser{}
}

func (p *markdownParser) SupportedExts() []string {
	return []string{".md", ".markdown"}
}

func (p *markdownParser) SupportedMIMEs() []string {
	return []string{"text/markdown"}
}

func (p *markdownParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "markdown", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "markdown"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(data))

	var blocks []Block
	var title string

	ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Heading:
			content := mdExtractText(n, data)
			blocks = append(blocks, Block{
				Type:    BlockHeading,
				Content: content,
				Level:   n.Level,
			})
			if n.Level == 1 && title == "" {
				title = content
			}
			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			if n.Parent().Kind() != ast.KindListItem {
				content := mdExtractText(n, data)
				if content != "" {
					blocks = append(blocks, Block{
						Type:    BlockParagraph,
						Content: content,
					})
				}
			}
			return ast.WalkSkipChildren, nil

		case *ast.ListItem:
			content := mdExtractText(n, data)
			blocks = append(blocks, Block{
				Type:    BlockListItem,
				Content: content,
			})
			return ast.WalkSkipChildren, nil

		case *ast.FencedCodeBlock:
			var buf bytes.Buffer
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				line := lines.At(i)
				buf.Write(line.Value(data))
			}
			blocks = append(blocks, Block{
				Type:    BlockCode,
				Content: buf.String(),
			})
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "markdown",
				Title:  title,
			},
		},
	}, nil
}

func mdExtractText(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
			if t.SoftLineBreak() {
				buf.WriteByte('\n')
			}
		} else {
			buf.WriteString(mdExtractText(child, source))
		}
	}
	return buf.String()
}
