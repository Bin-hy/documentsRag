package loader

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// audioParser 音频解析器：语音转写生成按时间戳分段的文本 Block（spec F4）
type audioParser struct {
	speech SpeechProvider
}

// NewAudioParser 创建音频解析器（speech 为 nil 时表示能力未配置）
func NewAudioParser(s SpeechProvider) Parser {
	return &audioParser{speech: s}
}

func (p *audioParser) SupportedExts() []string {
	return []string{".mp3", ".wav", ".m4a", ".flac", ".ogg", ".aac"}
}

func (p *audioParser) SupportedMIMEs() []string {
	return []string{
		"audio/mpeg", "audio/wav", "audio/x-wav", "audio/mp4",
		"audio/flac", "audio/ogg", "audio/aac",
	}
}

// CheckCapabilities 能力缺失检查（上传预检阶段调用，plan D3）
func (p *audioParser) CheckCapabilities() error {
	if p.speech == nil {
		return &ErrMediaCapabilityMissing{Capability: "speech"}
	}
	return nil
}

// MediaCategory 返回多媒体类别（支持列表分组用）
func (p *audioParser) MediaCategory() string { return "audio" }

// Parse 音频 → 转写分段 Blocks（BlockAudioSegment + 起止时间戳 metadata）
func (p *audioParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	if err := p.CheckCapabilities(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, &ErrParseFailed{Format: "audio", Cause: err}
	}

	segments, err := p.speech.Transcribe(ctx, data, SpeechOptions{Filename: opts.Filename})
	if err != nil {
		return nil, fmt.Errorf("语音转写失败: %w", err)
	}

	blocks := make([]Block, 0, len(segments))
	var durationMs int64
	for _, s := range segments {
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		blocks = append(blocks, Block{
			Type:    BlockAudioSegment,
			Content: s.Text,
			Metadata: map[string]any{
				"media_type": "audio",
				"start_ms":   s.StartMs,
				"end_ms":     s.EndMs,
				"source":     opts.Filename,
			},
		})
		if s.EndMs > durationMs {
			durationMs = s.EndMs
		}
	}

	if len(blocks) == 0 {
		return nil, &ErrNoReadableContent{Format: "audio", Readable: 0, MinChars: 1}
	}

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "audio",
				Extra: map[string]any{
					"media_type":  "audio",
					"duration_ms": durationMs,
					"source":      opts.Filename,
				},
			},
		},
	}, nil
}
