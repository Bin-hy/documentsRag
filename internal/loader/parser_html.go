package loader

import (
	"context"
	"io"
	"strings"

	"golang.org/x/net/html"
)

type htmlParser struct{}

func NewHtmlParser() Parser {
	return &htmlParser{}
}

func (p *htmlParser) SupportedExts() []string {
	return []string{".html", ".htm"}
}

func (p *htmlParser) SupportedMIMEs() []string {
	return []string{"text/html"}
}

func (p *htmlParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	doc, err := html.Parse(reader)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "html", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "html"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	var blocks []Block
	var title string
	htmlWalkNode(doc, &blocks, &title)

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "html",
				Title:  title,
			},
		},
	}, nil
}

var htmlSkipTags = map[string]bool{
	"script": true, "style": true, "nav": true, "footer": true, "head": true,
}

func htmlWalkNode(n *html.Node, blocks *[]Block, title *string) {
	if n.Type == html.ElementNode {
		if htmlSkipTags[n.Data] {
			return
		}

		switch n.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := int(n.Data[1] - '0')
			content := htmlTextContent(n)
			if content != "" {
				*blocks = append(*blocks, Block{
					Type:    BlockHeading,
					Content: content,
					Level:   level,
				})
				if level == 1 && *title == "" {
					*title = content
				}
			}
			return
		case "p":
			content := htmlTextContent(n)
			if content != "" {
				*blocks = append(*blocks, Block{
					Type:    BlockParagraph,
					Content: content,
				})
			}
			return
		case "li":
			content := htmlTextContent(n)
			if content != "" {
				*blocks = append(*blocks, Block{
					Type:    BlockListItem,
					Content: content,
				})
			}
			return
		case "pre", "code":
			content := htmlTextContent(n)
			if content != "" {
				*blocks = append(*blocks, Block{
					Type:    BlockCode,
					Content: content,
				})
			}
			return
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		htmlWalkNode(c, blocks, title)
	}
}

func htmlTextContent(n *html.Node) string {
	var sb strings.Builder
	htmlCollectText(n, &sb)
	return strings.TrimSpace(sb.String())
}

func htmlCollectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		htmlCollectText(c, sb)
	}
}
