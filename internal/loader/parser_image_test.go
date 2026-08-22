package loader

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// mockVision 可控视觉 Provider
type mockVision struct {
	text string
	err  error
	got  []byte
}

func (m *mockVision) Describe(_ context.Context, img []byte, _ VisionOptions) (string, error) {
	m.got = img
	return m.text, m.err
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成测试 png 失败: %v", err)
	}
	return buf.Bytes()
}

func TestImageParserSupported(t *testing.T) {
	p := NewImageParser(&mockVision{})
	exts := map[string]bool{}
	for _, e := range p.SupportedExts() {
		exts[e] = true
	}
	for _, want := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"} {
		if !exts[want] {
			t.Errorf("缺少扩展名 %s", want)
		}
	}
	mimes := map[string]bool{}
	for _, m := range p.SupportedMIMEs() {
		mimes[m] = true
	}
	if !mimes["image/png"] || !mimes["image/webp"] || !mimes["image/jpeg"] {
		t.Error("MIME 表不完整")
	}
}

// 能力缺失：vision 为 nil 时 CheckCapabilities 与 Parse 均报 vision 缺失
func TestImageParserCapabilityMissing(t *testing.T) {
	p := NewImageParser(nil)
	checker, ok := p.(MediaCapabilityChecker)
	if !ok {
		t.Fatal("图片 parser 应实现 MediaCapabilityChecker")
	}
	if err := checker.CheckCapabilities(); err == nil || !errors.Is(err, &ErrMediaCapabilityMissing{}) {
		t.Errorf("CheckCapabilities 应报 ErrMediaCapabilityMissing，实际 %v", err)
	}
	var mce *ErrMediaCapabilityMissing
	if err := checker.CheckCapabilities(); !errors.As(err, &mce) || mce.Capability != "vision" {
		t.Errorf("应报 vision 能力缺失，实际 %v", err)
	}
	if _, err := p.Parse(context.Background(), bytes.NewReader([]byte("x")), LoadOptions{}); err == nil {
		t.Error("nil vision 时 Parse 应失败")
	}
}

// 正常解析：描述进 Content、宽高/来源进 metadata、Block 类型为 image_description
func TestImageParserParse(t *testing.T) {
	img := testPNG(t, 20, 10)
	mv := &mockVision{text: "一张技术架构图，包含数据库与 API 层"}
	p := NewImageParser(mv)

	result, err := p.Parse(context.Background(), bytes.NewReader(img), LoadOptions{Filename: "架构图.png"})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(result.Document.Blocks) != 1 {
		t.Fatalf("应产出 1 个 Block，实际 %d", len(result.Document.Blocks))
	}
	b := result.Document.Blocks[0]
	if b.Type != BlockImageDescription {
		t.Errorf("Block 类型应为 BlockImageDescription，实际 %v", b.Type)
	}
	if b.Content != "一张技术架构图，包含数据库与 API 层" {
		t.Errorf("Content 应为视觉描述，实际 %q", b.Content)
	}
	if b.Metadata["width"] != 20 || b.Metadata["height"] != 10 {
		t.Errorf("宽高 metadata 错误: %+v", b.Metadata)
	}
	if b.Metadata["source"] != "架构图.png" {
		t.Errorf("source metadata 错误: %+v", b.Metadata)
	}
	if !bytes.Equal(mv.got, img) {
		t.Error("传给 vision 的数据应为原始图片字节")
	}
}

// 视觉服务失败：Parse 返回可读错误，不产出空文档
func TestImageParserProviderError(t *testing.T) {
	p := NewImageParser(&mockVision{err: errors.New("timeout")})
	_, err := p.Parse(context.Background(), bytes.NewReader(testPNG(t, 1, 1)), LoadOptions{})
	if err == nil {
		t.Fatal("服务失败应返回错误")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("视觉理解")) {
		t.Errorf("错误应可读，实际 %v", err)
	}
}

// 空描述：拒绝（防脏数据）
func TestImageParserEmptyDescription(t *testing.T) {
	p := NewImageParser(&mockVision{text: "   "})
	if _, err := p.Parse(context.Background(), bytes.NewReader(testPNG(t, 1, 1)), LoadOptions{}); err == nil {
		t.Error("空描述应拒绝")
	}
}
