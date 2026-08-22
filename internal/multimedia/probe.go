package multimedia

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// mediaProber ffprobe 媒体信息探测（spec F1/AC1，plan D2）
type mediaProber struct {
	run commandRunner
}

// NewMediaProber 创建媒体探测器
func NewMediaProber() loader.MediaProber {
	return &mediaProber{run: runCommand}
}

// ffprobe 输出 JSON 结构（仅取所需字段）
type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe 用 ffprobe 读时长/编码/宽高/是否含音轨
func (p *mediaProber) Probe(ctx context.Context, videoPath string) (loader.MediaInfo, error) {
	out, err := p.run(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration:stream=codec_type,codec_name,width,height",
		"-of", "json",
		videoPath,
	)
	if err != nil {
		return loader.MediaInfo{}, fmt.Errorf("ffprobe 探测失败: %w", err)
	}

	var po probeOutput
	if err := json.Unmarshal(out, &po); err != nil {
		return loader.MediaInfo{}, fmt.Errorf("解析 ffprobe 输出失败: %w", err)
	}

	info := loader.MediaInfo{}
	for _, s := range po.Streams {
		switch s.CodecType {
		case "video":
			info.VideoCodec = s.CodecName
			info.Width = s.Width
			info.Height = s.Height
		case "audio":
			info.AudioCodec = s.CodecName
			info.HasAudio = true
		}
	}
	if secs, err := strconv.ParseFloat(po.Format.Duration, 64); err == nil {
		info.DurationMs = int64(secs * 1000)
	}
	return info, nil
}
