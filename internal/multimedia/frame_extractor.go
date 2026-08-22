package multimedia

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// DefaultFrameIntervalSec 默认抽帧间隔（秒），与 config.MultimediaConfig.FrameIntervalSec 默认值一致
const DefaultFrameIntervalSec = 10

// commandRunner 命令执行器（可注入以便单测断言参数构造，plan D4）
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// frameExtractor ffmpeg 固定间隔抽帧实现（plan D4/D5：数组传参、服务端临时文件，防注入）
type frameExtractor struct {
	run commandRunner
}

// NewFrameExtractor 创建抽帧器
func NewFrameExtractor() loader.FrameExtractor {
	return &frameExtractor{run: runCommand}
}

// fixedFrameStrategy 固定间隔抽帧策略（实现 loader.FrameStrategy）
type fixedFrameStrategy struct {
	extractor loader.FrameExtractor
}

// SampleFrames 固定间隔抽帧
func (s *fixedFrameStrategy) SampleFrames(ctx context.Context, videoPath string, cfg loader.FrameStrategyConfig) ([]loader.VideoFrame, error) {
	interval := cfg.IntervalSec
	if interval <= 0 {
		interval = DefaultFrameIntervalSec
	}
	return s.extractor.ExtractFrames(ctx, videoPath, interval)
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ExtractFrames 固定间隔抽帧：ffmpeg fps=1/N 滤镜保证输出帧等间隔 N 秒，
// 第 i 帧时间 = i*N 秒（不依赖帧内时间戳解析，避免额外 ffprobe 调用）。
// 输出 JPEG 到服务端临时目录，完成后清理。
func (f *frameExtractor) ExtractFrames(ctx context.Context, videoPath string, intervalSec int) ([]loader.VideoFrame, error) {
	if intervalSec <= 0 {
		intervalSec = DefaultFrameIntervalSec
	}
	vf := fmt.Sprintf("fps=1/%d", intervalSec)
	timeMs := func(i int) int64 { return int64(i) * int64(intervalSec) * 1000 }
	return ffmpegExtractFrames(ctx, f.run, videoPath, vf, timeMs)
}

// ffmpegExtractFrames 包级共享：ffmpeg fps 滤镜抽帧到临时目录，按序号排序读回，
// 时间戳由 timeMs(i) 计算。供 FixedFrameStrategy 与 SceneSampler 预抽帧复用。
func ffmpegExtractFrames(ctx context.Context, run commandRunner, videoPath, vfExpr string, timeMs func(i int) int64) ([]loader.VideoFrame, error) {
	outDir, err := os.MkdirTemp("", "binrag-frames-*")
	if err != nil {
		return nil, fmt.Errorf("创建抽帧临时目录失败: %w", err)
	}
	defer os.RemoveAll(outDir)

	outPattern := filepath.Join(outDir, "frame_%04d.jpg")
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", videoPath,
		"-vf", vfExpr,
		"-vsync", "vfr",
		outPattern,
	}
	if _, err := run(ctx, "ffmpeg", args...); err != nil {
		return nil, fmt.Errorf("ffmpeg 抽帧失败: %w", err)
	}

	matches, err := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("读取抽帧结果失败: %w", err)
	}
	sort.Strings(matches) // 字典序 = 序号序 = 时间序

	frames := make([]loader.VideoFrame, 0, len(matches))
	for i, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, fmt.Errorf("读取抽帧结果失败: %w", err)
		}
		frames = append(frames, loader.VideoFrame{
			TimeMs: timeMs(i),
			Data:   data,
		})
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("视频未抽到任何帧（文件可能无视频轨或已损坏）")
	}
	return frames, nil
}
