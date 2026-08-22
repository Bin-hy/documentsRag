package loader

import (
	"context"
)

// 本文件定义多媒体处理能力抽象接口。
// 设计：接口定义放在 loader 包（Parser 与能力同级），实现放 internal/multimedia 包，
// 保证 Parser 只依赖抽象、loader 不反向依赖实现，多媒体实现可插拔替换（spec G4/N3）。

// VisionProvider 视觉理解能力：图片/视频帧 → 文本描述。
// 未配置对应服务时由构造方注入 nil，Parser 通过 MediaCapabilityChecker 暴露能力缺失。
type VisionProvider interface {
	Describe(ctx context.Context, image []byte, opts VisionOptions) (string, error)
}

// VisionOptions 视觉理解请求选项
type VisionOptions struct {
	Filename string // 来源文件名（图片/视频）
}

// SpeechSegment 语音转写分段（含起止时间戳）
type SpeechSegment struct {
	StartMs int64 // 起始时间（毫秒）
	EndMs   int64 // 结束时间（毫秒）
	Text    string
}

// SpeechProvider 语音转写能力：音频 → 按时间戳分段文本。
type SpeechProvider interface {
	Transcribe(ctx context.Context, audio []byte, opts SpeechOptions) ([]SpeechSegment, error)
}

// SpeechOptions 语音转写请求选项
type SpeechOptions struct {
	Filename string // 来源文件名（音频/视频）
}

// VideoFrame 视频关键帧
type VideoFrame struct {
	TimeMs int64 // 关键帧时间点（毫秒）
	Data   []byte
}

// FrameExtractor 视频抽帧能力（默认实现：ffmpeg 固定时间间隔抽帧）。
// 抽帧策略先以 intervalSec 参数承载，预留场景变化检测等策略扩展（spec F5/N3）。
type FrameExtractor interface {
	ExtractFrames(ctx context.Context, videoPath string, intervalSec int) ([]VideoFrame, error)
}

// MediaCapabilityChecker 能力缺失检查。
// 多媒体 Parser 实现该接口：上传预检阶段只调用 CheckCapabilities 判定能力是否可用，
// 避免上传+入库各调用一次视觉/转写服务（plan D3）。
type MediaCapabilityChecker interface {
	CheckCapabilities() error // 能力缺失返回 *ErrMediaCapabilityMissing
}

// MediaCategory 多媒体 parser 暴露类别，供支持列表分组展示；文本 parser 不实现即视为 "text"。
type MediaCategory interface {
	MediaCategory() string // "image" / "audio" / "video"
}

// ErrMediaCapabilityMissing 能力缺失错误（上传预检 400 返回，spec F7）
type ErrMediaCapabilityMissing struct {
	Capability string // "vision" / "speech"
}

func (e *ErrMediaCapabilityMissing) Error() string {
	return "multimedia." + e.Capability + " 未配置，无法处理该类型文件（请在 config.yaml 中配置 multimedia." + e.Capability + "）"
}

// Is 使 errors.Is(err, &ErrMediaCapabilityMissing{}) 可按类型匹配
func (e *ErrMediaCapabilityMissing) Is(target error) bool {
	_, ok := target.(*ErrMediaCapabilityMissing)
	return ok
}

// —— 视频处理增强能力抽象（spec F1-F4，plan D3）——

// FrameStrategy 视频抽帧策略（fixed 固定间隔 / scene 两阶段场景检测）
type FrameStrategy interface {
	SampleFrames(ctx context.Context, videoPath string, cfg FrameStrategyConfig) ([]VideoFrame, error)
}

// FrameStrategyConfig 抽帧策略参数
type FrameStrategyConfig struct {
	Mode          string  // "fixed" | "scene"
	IntervalSec   int     // fixed 抽帧间隔（秒）
	SampleFPS     int     // scene 预抽帧率
	SimThreshold  float64 // scene 场景切换相似度阈值（低于该值判定切场景）
	MinSceneDurMs int64   // scene 最小场景时长（毫秒）
}

// VisualEmbedder 视觉 embedding（场景检测帧间语义相似度，spec F3）
type VisualEmbedder interface {
	EmbedImage(ctx context.Context, image []byte) ([]float32, error)
}

// MediaInfo 视频媒体信息（spec F1/AC1）
type MediaInfo struct {
	DurationMs int64
	VideoCodec string
	AudioCodec string
	HasAudio   bool
	Width      int
	Height     int
}

// MediaProber 媒体信息探测（ffprobe）
type MediaProber interface {
	Probe(ctx context.Context, videoPath string) (MediaInfo, error)
}

// AudioExtractor 音频流提取（ffmpeg demux copy，spec F1）
// 无音频轨时返回空路径不报错（由调用方判断）。
type AudioExtractor interface {
	Extract(ctx context.Context, videoPath string) (audioPath string, err error)
}
