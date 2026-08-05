package loader

import (
	"context"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

type excelParser struct{}

func NewExcelParser() Parser {
	return &excelParser{}
}

func (p *excelParser) SupportedExts() []string {
	return []string{".xlsx", ".xls"}
}

func (p *excelParser) SupportedMIMEs() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
	}
}

func (p *excelParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "excel", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "excel"}},
			Warnings: []string{err.Error()},
		}, nil
	}
	defer f.Close()

	var blocks []Block

	for _, sheet := range f.GetSheetList() {
		blocks = append(blocks, Block{
			Type:    BlockHeading,
			Content: sheet,
			Level:   1,
		})

		rows, err := f.GetRows(sheet)
		if err != nil {
			if opts.Mode == ModeStrict {
				return nil, &ErrParseFailed{Format: "excel", Cause: err}
			}
			continue
		}

		for _, row := range rows {
			content := strings.Join(row, "\t")
			if strings.TrimSpace(content) == "" {
				continue
			}
			blocks = append(blocks, Block{
				Type:    BlockTable,
				Content: content,
			})
		}
	}

	return &LoadResult{
		Document: &Document{
			Blocks:   blocks,
			Metadata: DocumentMeta{Format: "excel"},
		},
	}, nil
}
