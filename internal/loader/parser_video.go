package loader

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// videoParser 视频解析器：拆流 → 双抽帧策略（fixed/scene）→ VLM + 独立 ASR（spec F1-F6）
// 视觉轨与音频轨独立：未配置 speech 时跳过音轨转写并记录 warning，不阻断。
type videoParser struct {
	vision    VisionProvider
	speech    SpeechProvider // 可为 nil（音轨降级）
	strategy  FrameStrategy  // 抽帧策略（fixed/scene）；nil = 配置错误（scene 缺 embedding）
	prober    MediaProber    // 媒体探测
	extractor AudioExtractor // 音频流提取
	cfg       FrameStrategyConfig
}

// NewVideoParser 创建视频解析器。
// strategy 为 nil 时 CheckCapabilities 报配置错误（scene 未配置 vision_embedding）。
func NewVideoParser(v VisionProvider, s SpeechProvider, strategy FrameStrategy, prober MediaProber, extractor AudioExtractor, cfg FrameStrategyConfig) Parser {
	return &videoParser{vision: v, speech: s, strategy: strategy, prober: prober, extractor: extractor, cfg: cfg}
}

func (p *videoParser) SupportedExts() []string {
	return []string{".mp4", ".avi", ".mkv", ".mov", ".webm"}
}

func (p *videoParser) SupportedMIMEs() []string {
	return []string{"video/mp4", "video/x-msvideo", "video/x-matroska", "video/quicktime", "video/webm"}
}

// CheckCapabilities 能力缺失检查（视频至少需要 vision；scene 还需 vision_embedding）
func (p *videoParser) CheckCapabilities() error {
	if p.vision == nil {
		return &ErrMediaCapabilityMissing{Capability: "vision"}
	}
	if p.strategy == nil {
		return fmt.Errorf("multimedia.video.frame_strategy=scene 需要配置 multimedia.video.vision_embedding")
	}
	return nil
}

// MediaCategory 返回多媒体类别（支持列表分组用）
func (p *videoParser) MediaCategory() string { return "video" }

// Parse 视频 → Image Blocks（时间戳）+ Audio Blocks（原时间戳）
func (p *videoParser) Parse(ctx context.Context, reader io.Reader, opts LoadOptions) (*LoadResult, error) {
	if err := p.CheckCapabilities(); err != nil {
		return nil, err
	}

	// ffmpeg 需要文件输入：落盘服务端临时文件（防注入，plan D5）
	tmp, err := os.CreateTemp("", "binrag-video-*.mp4")
	if err != nil {
		return nil, &ErrParseFailed{Format: "video", Cause: err}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		return nil, &ErrParseFailed{Format: "video", Cause: err}
	}
	if err := tmp.Close(); err != nil {
		return nil, &ErrParseFailed{Format: "video", Cause: err}
	}

	// 媒体探测
	media, err := p.prober.Probe(ctx, tmpPath)
	if err != nil {
		return nil, fmt.Errorf("媒体探测失败: %w", err)
	}

	var warnings []string
	var blocks []Block

	// 视频流：抽帧策略 → 逐帧 VLM
	frames, err := p.strategy.SampleFrames(ctx, tmpPath, p.cfg)
	if err != nil {
		return nil, fmt.Errorf("视频抽帧失败: %w", err)
	}
	intervalMs := int64(p.cfg.IntervalSec) * 1000
	if intervalMs <= 0 {
		intervalMs = 10000
	}
	for i, f := range frames {
		text, err := p.vision.Describe(ctx, f.Data, VisionOptions{Filename: opts.Filename})
		if err != nil {
			return nil, fmt.Errorf("视频帧视觉理解失败（t=%dms）: %w", f.TimeMs, err)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		startMs := f.TimeMs
		endMs := startMs + intervalMs
		if i+1 < len(frames) {
			endMs = frames[i+1].TimeMs // 下一帧（或场景）边界
		}
		if endMs <= startMs {
			endMs = startMs + intervalMs
		}
		blocks = append(blocks, Block{
			Type:    BlockImageDescription,
			Content: text,
			Metadata: map[string]any{
				"media_type":   "video_frame",
				"timestamp_ms": f.TimeMs,
				"frame_index":  i,
				"start_ms":     startMs,
				"end_ms":       endMs,
				"source":       opts.Filename,
			},
		})
	}

	// 音频流：独立拆流 → ASR（保留原始时间戳）
	if media.HasAudio {
		if p.speech != nil {
			audioPath, err := p.extractor.Extract(ctx, tmpPath)
			if err != nil {
				warnings = append(warnings, "音轨拆流失败（已跳过）: "+err.Error())
			} else if audioPath != "" {
				defer os.Remove(audioPath)
				audioData, err := os.ReadFile(audioPath)
				if err != nil {
					warnings = append(warnings, "音轨读取失败（已跳过）: "+err.Error())
				} else {
					segments, err := p.speech.Transcribe(ctx, audioData, SpeechOptions{Filename: opts.Filename})
					if err != nil {
						warnings = append(warnings, "音轨转写失败（已跳过）: "+err.Error())
					} else {
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
						}
					}
				}
			}
		} else {
			warnings = append(warnings, "multimedia.speech 未配置，跳过视频音轨转写")
		}
	}

	if len(blocks) == 0 {
		return nil, &ErrNoReadableContent{Format: "video", Readable: 0, MinChars: 1}
	}

	return &LoadResult{
		Document: &Document{
			Blocks: blocks,
			Metadata: DocumentMeta{
				Format: "video",
				Extra: map[string]any{
					"media_type":  "video",
					"duration_ms": media.DurationMs,
					"video_codec": media.VideoCodec,
					"audio_codec": media.AudioCodec,
					"has_audio":   media.HasAudio,
					"width":       media.Width,
					"height":      media.Height,
					"frame_count": len(frames),
					"source":      opts.Filename,
				},
			},
		},
		Warnings: warnings,
	}, nil
}
