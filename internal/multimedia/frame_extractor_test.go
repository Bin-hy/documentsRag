package multimedia

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// 单测：注入 fake runner 断言 ffmpeg 参数构造与帧时间戳计算
func TestExtractFramesArgsAndTimestamps(t *testing.T) {
	var gotName string
	var gotArgs []string
	fakeRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = args
		// 最后一个参数是输出模式，生成 2 个假帧文件
		pattern := args[len(args)-1]
		for i := 0; i < 2; i++ {
			p := strings.Replace(pattern, "%04d", fmt.Sprintf("%04d", i), 1)
			if err := os.WriteFile(p, []byte("fake-jpeg-"+fmt.Sprint(i)), 0o644); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	fx := &frameExtractor{run: fakeRun}
	frames, err := fx.ExtractFrames(context.Background(), "/data/input.mp4", 5)
	if err != nil {
		t.Fatalf("ExtractFrames 失败: %v", err)
	}

	if gotName != "ffmpeg" {
		t.Errorf("命令应为 ffmpeg，实际 %s", gotName)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-i", "/data/input.mp4", "-vf", "fps=1/5", "-vsync", "vfr"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv 缺少 %q：%v", want, gotArgs)
		}
	}
	if len(frames) != 2 {
		t.Fatalf("应生成 2 帧，实际 %d", len(frames))
	}
	if frames[0].TimeMs != 0 || frames[1].TimeMs != 5000 {
		t.Errorf("时间戳应为 0/5000ms（interval=5s），实际 %d/%d", frames[0].TimeMs, frames[1].TimeMs)
	}
	if string(frames[0].Data) != "fake-jpeg-0" {
		t.Errorf("帧数据未正确读回: %q", frames[0].Data)
	}
}

// 单测：interval<=0 时使用默认 10s
func TestExtractFramesDefaultInterval(t *testing.T) {
	var gotArgs []string
	fakeRun := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		pattern := args[len(args)-1]
		_ = os.WriteFile(strings.Replace(pattern, "%04d", "0000", 1), []byte("x"), 0o644)
		return nil, nil
	}
	fx := &frameExtractor{run: fakeRun}
	if _, err := fx.ExtractFrames(context.Background(), "v.mp4", 0); err != nil {
		t.Fatalf("ExtractFrames 失败: %v", err)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "fps=1/10") {
		t.Errorf("默认间隔应为 fps=1/10，实际 %v", gotArgs)
	}
}

// 真实集成（本机有 ffmpeg）：testsrc 生成小视频 → 抽帧
func TestExtractFramesRealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("环境无 ffmpeg，跳过真实集成")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "src.mp4")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=160x120:rate=10",
		"-pix_fmt", "yuv420p", video)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败: %v\n%s", err, out)
	}

	fx := NewFrameExtractor()
	frames, err := fx.ExtractFrames(context.Background(), video, 1)
	if err != nil {
		t.Fatalf("ExtractFrames 失败: %v", err)
	}
	if len(frames) < 1 {
		t.Fatal("3 秒视频 interval=1 至少应抽到 1 帧")
	}
	for i := 1; i < len(frames); i++ {
		if frames[i].TimeMs <= frames[i-1].TimeMs {
			t.Errorf("时间戳应递增: %+v", frames)
		}
	}
	// 验证 JPEG 魔数
	if len(frames[0].Data) < 2 || frames[0].Data[0] != 0xFF || frames[0].Data[1] != 0xD8 {
		t.Error("抽帧结果应为 JPEG 数据")
	}
}

var _ loader.FrameExtractor = (*frameExtractor)(nil)
