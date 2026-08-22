package loader

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // 注册 gif 解码器（DecodeConfig 用；bmp/webp 不在标准库，宽高读取失败时宽容省略）
	"image/jpeg"
	"image/png"
	"io"
	"strings"
)

// imageParser 图片解析器：视觉模型生成描述文本，产出结构化 Block（spec F3）
type imageParser struct {
	vision VisionProvider
}

// NewImageParser 创建图片解析器（vision 为 nil 时表示能力未配置）
func NewImageParser(v VisionProvider) Parser {
	return &imageParser{vision: v}
}

func (p *imageParser) SupportedExts() []string {
	return []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}
}

func (p *imageParser) SupportedMIMEs() []string {
	return []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp"}
}

// CheckCapabilities 能力缺失检查（上传预检阶段调用，plan D3）
func (p *imageParser) CheckCapabilities() error {
	if p.vision == nil {
		return &ErrMediaCapabilityMissing{Capability: "vision"}
	}
	return nil
}

// MediaCategory 返回多媒体类别（支持列表分组用）
func (p *imageParser) MediaCategory() string { return "image" }

// Parse 图片 → 视觉描述 Block（BlockImageDescription + metadata 宽高/来源）
func (p *imageParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	if err := p.CheckCapabilities(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, &ErrParseFailed{Format: "image", Cause: err}
	}

	// 宽高读取失败不阻断（webp/bmp 等标准库无法解码时省略尺寸）
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}

	text, err := p.vision.Describe(ctx, data, VisionOptions{Filename: opts.Filename})
	if err != nil {
		return nil, fmt.Errorf("图片视觉理解失败: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, &ErrNoReadableContent{Format: "image", Readable: 0, MinChars: 1}
	}

	blocks := []Block{{
		Type:    BlockImageDescription,
		Content: text,
		Metadata: map[string]any{
			"media_type": "image",
			"width":      width,
			"height":     height,
			"source":     opts.Filename,
		},
	}}

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "image",
				Extra: map[string]any{
					"media_type": "image",
					"width":      width,
					"height":     height,
					"source":     opts.Filename,
				},
			},
		},
	}, nil
}

// 确保 image/jpeg、image/png 包被引用（避免纯标准库导入被误删；实际由 image.DecodeConfig 的注册表使用）
var _ = jpeg.Decode
var _ = png.Decode
