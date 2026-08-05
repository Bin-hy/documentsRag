package loader

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/fumiama/go-docx"
)

type docxParser struct{}

func NewDocxParser() Parser {
	return &docxParser{}
}

func (p *docxParser) SupportedExts() []string {
	return []string{".docx"}
}

func (p *docxParser) SupportedMIMEs() []string {
	return []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
}

func (p *docxParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "docx", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "docx"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	doc, err := docx.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "docx", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "docx"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	var blocks []Block
	var title string

	for _, item := range doc.Document.Body.Items {
		para, ok := item.(*docx.Paragraph)
		if !ok {
			continue
		}

		text := docxExtractParaText(para)
		if strings.TrimSpace(text) == "" {
			continue
		}

		level := docxGetHeadingLevel(para)
		if level > 0 {
			blocks = append(blocks, Block{
				Type:    BlockHeading,
				Content: text,
				Level:   level,
			})
			if level == 1 && title == "" {
				title = text
			}
		} else {
			blocks = append(blocks, Block{
				Type:    BlockParagraph,
				Content: text,
			})
		}
	}

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "docx",
				Title:  title,
			},
		},
	}, nil
}

func docxExtractParaText(para *docx.Paragraph) string {
	var sb strings.Builder
	for _, child := range para.Children {
		if run, ok := child.(*docx.Run); ok {
			for _, rc := range run.Children {
				if t, ok := rc.(*docx.Text); ok {
					sb.WriteString(t.Text)
				}
			}
		}
	}
	return sb.String()
}

func docxGetHeadingLevel(para *docx.Paragraph) int {
	if para.Properties == nil || para.Properties.Style == nil {
		return 0
	}
	style := para.Properties.Style.Val
	switch {
	case strings.EqualFold(style, "Heading1") || style == "1":
		return 1
	case strings.EqualFold(style, "Heading2") || style == "2":
		return 2
	case strings.EqualFold(style, "Heading3") || style == "3":
		return 3
	case strings.EqualFold(style, "Heading4") || style == "4":
		return 4
	case strings.EqualFold(style, "Heading5") || style == "5":
		return 5
	case strings.EqualFold(style, "Heading6") || style == "6":
		return 6
	}
	return 0
}
