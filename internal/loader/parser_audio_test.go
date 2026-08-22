package loader

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// mockSpeech 可控语音转写 Provider
type mockSpeech struct {
	segments []SpeechSegment
	err      error
	got      []byte
}

func (m *mockSpeech) Transcribe(_ context.Context, audio []byte, _ SpeechOptions) ([]SpeechSegment, error) {
	m.got = audio
	return m.segments, m.err
}

func TestAudioParserSupported(t *testing.T) {
	p := NewAudioParser(&mockSpeech{})
	exts := map[string]bool{}
	for _, e := range p.SupportedExts() {
		exts[e] = true
	}
	for _, want := range []string{".mp3", ".wav", ".m4a", ".flac", ".ogg", ".aac"} {
		if !exts[want] {
			t.Errorf("缺少扩展名 %s", want)
		}
	}
	mimes := map[string]bool{}
	for _, m := range p.SupportedMIMEs() {
		mimes[m] = true
	}
	if !mimes["audio/mpeg"] || !mimes["audio/wav"] || !mimes["audio/ogg"] {
		t.Error("MIME 表不完整")
	}
}

// 能力缺失：speech 为 nil 时 CheckCapabilities 与 Parse 均报 speech 缺失
func TestAudioParserCapabilityMissing(t *testing.T) {
	p := NewAudioParser(nil)
	checker, ok := p.(MediaCapabilityChecker)
	if !ok {
		t.Fatal("音频 parser 应实现 MediaCapabilityChecker")
	}
	var mce *ErrMediaCapabilityMissing
	if err := checker.CheckCapabilities(); !errors.As(err, &mce) || mce.Capability != "speech" {
		t.Errorf("应报 speech 能力缺失，实际 %v", err)
	}
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Error("nil speech 时 Parse 应失败")
	}
}

// 正常解析：分段与时间戳 metadata、总时长=末段 EndMs、来源
func TestAudioParserParse(t *testing.T) {
	ms := &mockSpeech{segments: []SpeechSegment{
		{StartMs: 0, EndMs: 2500, Text: "大家好"},
		{StartMs: 2500, EndMs: 5000, Text: "今天讲需求"},
	}}
	p := NewAudioParser(ms)

	result, err := p.Parse(context.Background(), bytes.NewReader([]byte("fake-audio")), LoadOptions{Filename: "会议录音.mp3"})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	blocks := result.Document.Blocks
	if len(blocks) != 2 {
		t.Fatalf("应产出 2 个 Block，实际 %d", len(blocks))
	}
	if blocks[0].Type != BlockAudioSegment {
		t.Errorf("Block 类型应为 BlockAudioSegment，实际 %v", blocks[0].Type)
	}
	if blocks[0].Content != "大家好" || blocks[1].Content != "今天讲需求" {
		t.Errorf("Content 应为转写文本: %q / %q", blocks[0].Content, blocks[1].Content)
	}
	if blocks[0].Metadata["start_ms"] != int64(0) || blocks[0].Metadata["end_ms"] != int64(2500) {
		t.Errorf("起止时间戳 metadata 错误: %+v", blocks[0].Metadata)
	}
	if blocks[1].Metadata["start_ms"] != int64(2500) || blocks[1].Metadata["end_ms"] != int64(5000) {
		t.Errorf("第二段时间戳 metadata 错误: %+v", blocks[1].Metadata)
	}
	if blocks[0].Metadata["source"] != "会议录音.mp3" {
		t.Errorf("source metadata 错误: %+v", blocks[0].Metadata)
	}
	if result.Document.Metadata.Extra["duration_ms"] != int64(5000) {
		t.Errorf("总时长应为末段 EndMs=5000，实际 %v", result.Document.Metadata.Extra["duration_ms"])
	}
	if !bytes.Equal(ms.got, []byte("fake-audio")) {
		t.Error("传给 speech 的数据应为原始音频字节")
	}
}

// 转写为空：拒绝（防脏数据）
func TestAudioParserEmptySegments(t *testing.T) {
	p := NewAudioParser(&mockSpeech{segments: []SpeechSegment{{StartMs: 0, EndMs: 100, Text: "  "}}})
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Error("空转写应拒绝")
	}
}

// 转写服务失败：返回可读错误
func TestAudioParserProviderError(t *testing.T) {
	p := NewAudioParser(&mockSpeech{err: errors.New("HTTP 500")})
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Fatal("服务失败应返回错误")
	}
}
