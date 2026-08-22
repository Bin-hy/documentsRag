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
// 多媒体 parser 默认以 nil 能力注册：未配置 multimedia.vision/speech 时，
// 对应类型文件在 CheckCapabilities 阶段即被明确拒绝（spec F7/AC5）；
// app 装配层在配置就绪后用真实 provider 覆盖注册同名扩展名。
func NewDefaultRegistry() Registry {
	r := NewRegistry()
	r.Register(NewTxtParser())
	r.Register(NewMarkdownParser())
	r.Register(NewPdfParser())
	r.Register(NewDocxParser())
	r.Register(NewCsvParser())
	r.Register(NewExcelParser())
	r.Register(NewHtmlParser())
	// 多媒体（nil 能力兜底，配置后由装配层覆盖）
	r.Register(NewImageParser(nil))
	r.Register(NewAudioParser(nil))
	r.Register(NewVideoParser(nil, nil, nil, nil, nil, FrameStrategyConfig{}))
	return r
}

// NewLoaderWithRegistry 使用自定义注册表创建加载器
func NewLoaderWithRegistry(r Registry) Loader {
	return &defaultLoader{registry: r}
}

func (l *defaultLoader) Load(ctx context.Context, reader io.Reader, info FileInfo, opts ...LoadOptions) (*LoadResult, error) {
	opt := LoadOptions{Mode: ModeTolerant, Filename: info.Filename}
	if len(opts) > 0 {
		opt = opts[0]
		if opt.Filename == "" {
			opt.Filename = info.Filename
		}
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
