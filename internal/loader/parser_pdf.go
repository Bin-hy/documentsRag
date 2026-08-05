package loader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type pdfParser struct{}

func NewPdfParser() Parser {
	return &pdfParser{}
}

func (p *pdfParser) SupportedExts() []string {
	return []string{".pdf"}
}

func (p *pdfParser) SupportedMIMEs() []string {
	return []string{"application/pdf"}
}

func (p *pdfParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "pdf", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "pdf"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	rs := bytes.NewReader(data)
	pageCount, err := api.PageCount(rs, conf)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "pdf", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "pdf"}},
			Warnings: []string{fmt.Sprintf("无法解析 PDF: %v", err)},
		}, nil
	}

	var blocks []Block
	var warnings []string

	for i := 1; i <= pageCount; i++ {
		rs.Reset(data)
		var pageBuf strings.Builder
		err := api.ExtractContent(rs, []string{fmt.Sprintf("%d", i)}, func(r io.Reader, pageNr int) error {
			b, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			pageBuf.Write(b)
			return nil
		}, conf)

		if err != nil {
			if opts.Mode == ModeStrict {
				return nil, &ErrParseFailed{Format: "pdf", Cause: err}
			}
			warnings = append(warnings, fmt.Sprintf("第 %d 页解析失败: %v", i, err))
			continue
		}

		content := strings.TrimSpace(pageBuf.String())
		if content != "" {
			blocks = append(blocks, Block{
				Type:    BlockParagraph,
				Content: content,
				Metadata: map[string]any{
					"page": i,
				},
			})
		}
	}

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format:    "pdf",
				PageCount: pageCount,
			},
		},
		Warnings: warnings,
	}, nil
}
