package loader

import (
	"path/filepath"
	"strings"
)

type defaultRegistry struct {
	extMap  map[string]Parser
	mimeMap map[string]Parser
}

// NewRegistry 创建解析器注册表
func NewRegistry() Registry {
	return &defaultRegistry{
		extMap:  make(map[string]Parser),
		mimeMap: make(map[string]Parser),
	}
}

func (r *defaultRegistry) Register(parser Parser) {
	for _, ext := range parser.SupportedExts() {
		r.extMap[strings.ToLower(ext)] = parser
	}
	for _, mime := range parser.SupportedMIMEs() {
		r.mimeMap[strings.ToLower(mime)] = parser
	}
}

func (r *defaultRegistry) Resolve(info FileInfo) (Parser, error) {
	ext := strings.ToLower(filepath.Ext(info.Filename))
	if ext != "" {
		if p, ok := r.extMap[ext]; ok {
			return p, nil
		}
	}

	if info.MIMEType != "" {
		mime := strings.ToLower(info.MIMEType)
		if p, ok := r.mimeMap[mime]; ok {
			return p, nil
		}
	}

	return nil, &ErrUnsupportedFormat{
		Filename: info.Filename,
		MIMEType: info.MIMEType,
	}
}
