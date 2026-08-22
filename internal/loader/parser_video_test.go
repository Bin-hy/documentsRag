package loader

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// —— 视频 Parser 重构测试（拆流 + 双抽帧 + 时间戳）——

type mockStrategy struct {
	frames []VideoFrame
	err    error
}

func (m *mockStrategy) SampleFrames(_ context.Context, _ string, _ FrameStrategyConfig) ([]VideoFrame, error) {
	return m.frames, m.err
}

type mockProber struct {
	info MediaInfo
	err  error
}

func (m *mockProber) Probe(_ context.Context, _ string) (MediaInfo, error) {
	return m.info, m.err
}

type mockAudioExtractor struct {
	path string
	err  error
}

func (m *mockAudioExtractor) Extract(_ context.Context, _ string) (string, error) {
	return m.path, m.err
}

func newVideoParser(v VisionProvider, s SpeechProvider, strat FrameStrategy, prober MediaProber, ext AudioExtractor) Parser {
	return NewVideoParser(v, s, strat, prober, ext, FrameStrategyConfig{IntervalSec: 10})
}

func TestVideoParserSupported2(t *testing.T) {
	p := newVideoParser(&mockVision{text: "x"}, nil, &mockStrategy{}, &mockProber{}, &mockAudioExtractor{})
	exts := map[string]bool{}
	for _, e := range p.SupportedExts() {
		exts[e] = true
	}
	for _, want := range []string{".mp4", ".avi", ".mkv", ".mov", ".webm"} {
		if !exts[want] {
			t.Errorf("缺少扩展名 %s", want)
		}
	}
}

// 能力缺失：vision 为 nil 报 vision 缺失；strategy 为 nil（scene 缺 embedding）报配置错误
func TestVideoParserCapabilityChecks2(t *testing.T) {
	p := newVideoParser(nil, &mockSpeech{}, &mockStrategy{}, &mockProber{}, &mockAudioExtractor{})
	checker := p.(MediaCapabilityChecker)
	var mce *ErrMediaCapabilityMissing
	if err := checker.CheckCapabilities(); !errors.As(err, &mce) || mce.Capability != "vision" {
		t.Errorf("应报 vision 能力缺失，实际 %v", err)
	}

	p2 := newVideoParser(&mockVision{text: "x"}, nil, nil, &mockProber{}, &mockAudioExtractor{})
	if err := p2.(MediaCapabilityChecker).CheckCapabilities(); err == nil {
		t.Error("strategy nil（scene 缺 vision_embedding）应报配置错误")
	}
}

// 正常：拆流 + 抽帧 + 音轨转写，时间戳正确、媒体信息入 Extra
func TestVideoParserParse2(t *testing.T) {
	audioFile := filepath.Join(t.TempDir(), "a.m4a")
	_ = os.WriteFile(audioFile, []byte("fake-audio"), 0o644)

	vision := &mockVision{text: "画面描述"}
	speech := &mockSpeech{segments: []SpeechSegment{{StartMs: 0, EndMs: 3000, Text: "大家好"}}}
	strat := &mockStrategy{frames: []VideoFrame{
		{TimeMs: 0, Data: []byte("f0")},
		{TimeMs: 10000, Data: []byte("f1")},
	}}
	prober := &mockProber{info: MediaInfo{DurationMs: 20000, VideoCodec: "h264", AudioCodec: "aac", HasAudio: true, Width: 320, Height: 240}}
	ext := &mockAudioExtractor{path: audioFile}

	p := newVideoParser(vision, speech, strat, prober, ext)
	result, err := p.Parse(context.Background(), bytes.NewReader([]byte("fake-video")), LoadOptions{Filename: "培训.mp4"})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("完整能力不应有 warning，实际 %v", result.Warnings)
	}
	blocks := result.Document.Blocks
	if len(blocks) != 3 { // 2 帧 + 1 音轨段
		t.Fatalf("应产出 3 个 Block，实际 %d", len(blocks))
	}
	if blocks[0].Type != BlockImageDescription || blocks[1].Type != BlockImageDescription {
		t.Errorf("帧 Block 类型错误: %v / %v", blocks[0].Type, blocks[1].Type)
	}
	// 帧 0：start=0, end=下一帧 10000
	if blocks[0].Metadata["start_ms"] != int64(0) || blocks[0].Metadata["end_ms"] != int64(10000) {
		t.Errorf("帧 0 时间戳错误: %+v", blocks[0].Metadata)
	}
	if blocks[0].Metadata["frame_index"] != 0 || blocks[1].Metadata["frame_index"] != 1 {
		t.Errorf("frame_index 错误: %v / %v", blocks[0].Metadata["frame_index"], blocks[1].Metadata["frame_index"])
	}
	// 帧 1：start=10000, end=start+interval=20000
	if blocks[1].Metadata["start_ms"] != int64(10000) {
		t.Errorf("帧 1 start_ms 错误: %+v", blocks[1].Metadata)
	}
	// 音轨段保留原始时间戳
	if blocks[2].Type != BlockAudioSegment || blocks[2].Metadata["start_ms"] != int64(0) || blocks[2].Metadata["end_ms"] != int64(3000) {
		t.Errorf("音轨 Block 时间戳错误: %+v", blocks[2])
	}
	// 媒体信息入 Extra
	extra := result.Document.Metadata.Extra
	if extra["duration_ms"] != int64(20000) || extra["video_codec"] != "h264" || extra["has_audio"] != true {
		t.Errorf("媒体信息 Extra 错误: %+v", extra)
	}
}

// 降级：无 speech 但有音轨 → warning 跳过音轨，视觉照常
func TestVideoParserSpeechMissing2(t *testing.T) {
	vision := &mockVision{text: "画面"}
	strat := &mockStrategy{frames: []VideoFrame{{TimeMs: 0, Data: []byte("f0")}}}
	prober := &mockProber{info: MediaInfo{HasAudio: true, DurationMs: 10000}}
	p := newVideoParser(vision, nil, strat, prober, &mockAudioExtractor{})

	result, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(result.Warnings) == 0 || !bytes.Contains([]byte(result.Warnings[0]), []byte("跳过视频音轨转写")) {
		t.Errorf("应有音轨降级 warning，实际 %v", result.Warnings)
	}
	if len(result.Document.Blocks) != 1 {
		t.Errorf("视觉部分应照常产出，实际 %d blocks", len(result.Document.Blocks))
	}
}

// 降级：speech 有但拆流失败 → warning 不阻断
func TestVideoParserAudioExtractFail2(t *testing.T) {
	vision := &mockVision{text: "画面"}
	speech := &mockSpeech{segments: []SpeechSegment{{Text: "x"}}}
	strat := &mockStrategy{frames: []VideoFrame{{TimeMs: 0, Data: []byte("f0")}}}
	prober := &mockProber{info: MediaInfo{HasAudio: true}}
	ext := &mockAudioExtractor{err: errors.New("boom")}

	p := newVideoParser(vision, speech, strat, prober, ext)
	result, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{})
	if err != nil {
		t.Fatalf("拆流失败不应阻断: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("拆流失败应有 warning")
	}
}

// 失败：抽帧失败
func TestVideoParserFrameFail2(t *testing.T) {
	p := newVideoParser(&mockVision{text: "x"}, nil, &mockStrategy{err: errors.New("boom")}, &mockProber{info: MediaInfo{}}, &mockAudioExtractor{})
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Error("抽帧失败应返回错误")
	}
}

// 失败：帧 VLM 失败
func TestVideoParserVisionFail2(t *testing.T) {
	p := newVideoParser(&mockVision{err: errors.New("timeout")}, nil, &mockStrategy{frames: []VideoFrame{{TimeMs: 0, Data: []byte("f0")}}}, &mockProber{info: MediaInfo{}}, &mockAudioExtractor{})
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Error("帧视觉理解失败应返回错误")
	}
}
