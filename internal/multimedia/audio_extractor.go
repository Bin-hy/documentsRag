package multimedia

import (
	"context"
	"fmt"
	"os"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// audioExtractor ffmpeg demux 拆音频流（copy 不重编码，spec F1/N2）
type audioExtractor struct {
	run commandRunner
}

// NewAudioExtractor 创建音频流提取器
func NewAudioExtractor() loader.AudioExtractor {
	return &audioExtractor{run: runCommand}
}

// Extract 用 ffmpeg 提取主音轨（-map 0:a:0? 无音轨不报错；-acodec copy 不重编码）。
// 无音频轨时返回空路径不报错；ffmpeg 失败（损坏音轨/格式异常）返回含 stderr 的 error。
// 临时输出文件由调用方负责清理。
func (e *audioExtractor) Extract(ctx context.Context, videoPath string) (string, error) {
	tmp, err := os.CreateTemp("", "binrag-audio-*.m4a")
	if err != nil {
		return "", fmt.Errorf("创建音频临时文件失败: %w", err)
	}
	outPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(outPath) // 让 ffmpeg 重新创建（-y 覆盖）

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", videoPath,
		"-map", "0:a:0?",
		"-vn",
		"-acodec", "copy",
		outPath,
	}
	output, err := e.run(ctx, "ffmpeg", args...)
	if err != nil {
		// ffmpeg 失败（损坏音轨/格式异常）：携带 stderr 便于调用方区分与诊断
		return "", fmt.Errorf("音轨提取失败: %w (stderr: %s)", err, string(output))
	}

	// 无音频轨时 ffmpeg 成功但不产生输出文件（-map 0:a:0? 的 ? 语义）
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		return "", nil
	}
	return outPath, nil
}
