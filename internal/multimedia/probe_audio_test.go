package multimedia

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// —— 媒体探测与拆音频流 ——

func TestProbeParse(t *testing.T) {
	jsonOut := `{
	  "streams": [
	    {"codec_type":"video","codec_name":"h264","width":320,"height":240},
	    {"codec_type":"audio","codec_name":"aac"}
	  ],
	  "format": {"duration":"3.500000"}
	}`
	var gotArgs []string
	fakeRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte(jsonOut), nil
	}
	p := &mediaProber{run: fakeRun}
	info, err := p.Probe(context.Background(), "/data/v.mp4")
	if err != nil {
		t.Fatalf("Probe 失败: %v", err)
	}
	if info.DurationMs != 3500 || !info.HasAudio || info.VideoCodec != "h264" || info.AudioCodec != "aac" {
		t.Errorf("MediaInfo 解析错误: %+v", info)
	}
	if info.Width != 320 || info.Height != 240 {
		t.Errorf("宽高解析错误: %+v", info)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "-show_entries") {
		t.Errorf("ffprobe 参数错误: %v", gotArgs)
	}
}

func TestAudioExtractArgs(t *testing.T) {
	var gotArgs []string
	fakeRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		// 模拟 ffmpeg 生成输出文件（最后一个参数是输出路径）
		_ = os.WriteFile(args[len(args)-1], []byte("fake-audio"), 0o644)
		return nil, nil
	}
	e := &audioExtractor{run: fakeRun}
	out, err := e.Extract(context.Background(), "/data/v.mp4")
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	defer os.Remove(out)
	if out == "" {
		t.Fatal("应返回音频路径")
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-map", "0:a:0?", "-vn", "-acodec", "copy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv 缺少 %q：%v", want, gotArgs)
		}
	}
}

func TestAudioExtractNoAudioTrack(t *testing.T) {
	fakeRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		// 不生成输出文件，模拟无音轨
		return nil, nil
	}
	e := &audioExtractor{run: fakeRun}
	out, err := e.Extract(context.Background(), "/data/v.mp4")
	if err != nil {
		t.Fatalf("无音轨不应报错: %v", err)
	}
	if out != "" {
		t.Errorf("无音轨应返回空路径，实际 %q", out)
	}
}

// 真实集成（本机有 ffmpeg/ffprobe）：生成含音轨视频 → 探测 + 拆音频流
func TestProbeAndExtractRealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("无 ffmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("无 ffprobe")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "av.mp4")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", video)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败: %v\n%s", err, out)
	}

	info, err := NewMediaProber().Probe(context.Background(), video)
	if err != nil {
		t.Fatalf("Probe 失败: %v", err)
	}
	if !info.HasAudio || info.VideoCodec == "" || info.DurationMs <= 0 {
		t.Errorf("真实探测结果错误: %+v", info)
	}

	audio, err := NewAudioExtractor().Extract(context.Background(), video)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if audio == "" {
		t.Fatal("含音轨视频应拆出音频")
	}
	defer os.Remove(audio)
	if st, err := os.Stat(audio); err != nil || st.Size() == 0 {
		t.Errorf("拆出的音频文件异常: size=%v err=%v", st.Size(), err)
	}
}
