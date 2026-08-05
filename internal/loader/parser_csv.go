package loader

import (
	"context"
	"encoding/csv"
	"io"
	"strings"
)

type csvParser struct{}

func NewCsvParser() Parser {
	return &csvParser{}
}

func (p *csvParser) SupportedExts() []string {
	return []string{".csv"}
}

func (p *csvParser) SupportedMIMEs() []string {
	return []string{"text/csv"}
}

func (p *csvParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	r := csv.NewReader(reader)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		if opts.Mode == ModeStrict {
			return nil, &ErrParseFailed{Format: "csv", Cause: err}
		}
		return &LoadResult{
			Document: &Document{Metadata: DocumentMeta{Format: "csv"}},
			Warnings: []string{err.Error()},
		}, nil
	}

	var blocks []Block

	if len(records) > 0 {
		blocks = append(blocks, Block{
			Type:    BlockHeading,
			Content: strings.Join(records[0], ", "),
			Level:   1,
		})

		for _, row := range records[1:] {
			blocks = append(blocks, Block{
				Type:    BlockTable,
				Content: strings.Join(row, "\t"),
			})
		}
	}

	return &LoadResult{
		Document: &Document{
			Blocks:   blocks,
			Metadata: DocumentMeta{Format: "csv"},
		},
	}, nil
}
