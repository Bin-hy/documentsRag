package loader

import (
	"context"
	"io"
)

type defaultLoader struct {
	registry Registry
}

// NewLoader 创建文档加载器，自动注册所有内置解析器
func NewLoader() Loader {
	return &defaultLoader{registry: NewDefaultRegistry()}
}

// NewDefaultRegistry 创建注册了全部内置解析器的注册表（供 API 层扩展名校验复用）
func NewDefaultRegistry() Registry {
	r := NewRegistry()
	r.Register(NewTxtParser())
	r.Register(NewMarkdownParser())
	r.Register(NewPdfParser())
	r.Register(NewDocxParser())
	r.Register(NewCsvParser())
	r.Register(NewExcelParser())
	r.Register(NewHtmlParser())
	return r
}

// NewLoaderWithRegistry 使用自定义注册表创建加载器
func NewLoaderWithRegistry(r Registry) Loader {
	return &defaultLoader{registry: r}
}

func (l *defaultLoader) Load(ctx context.Context, reader io.Reader, info FileInfo, opts ...LoadOptions) (*LoadResult, error) {
	opt := LoadOptions{Mode: ModeTolerant}
	if len(opts) > 0 {
		opt = opts[0]
	}

	parser, err := l.registry.Resolve(info)
	if err != nil {
		return nil, err
	}

	result, err := parser.Parse(ctx, reader, opt)
	if err != nil {
		return nil, err
	}

	if result.Document != nil {
		result.Document.Metadata.Filename = info.Filename
		result.Document.Metadata.Size = info.Size
	}

	return result, nil
}
