package multimedia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// containsArgs 判断 args 中是否按顺序包含 seq。
func containsArgs(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j := range seq {
			if args[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// 转码命令正确 + 请求体正确 + 响应解析为单段无时间戳
func TestDashscopeTranscribeSuccess(t *testing.T) {
	wavBytes := []byte("FAKE-WAV-BYTES")
	var ffmpegArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "ffmpeg" {
			t.Errorf("转码命令应为 ffmpeg，实际 %q", name)
		}
		ffmpegArgs = append([]string{}, args...)
		// 模拟 ffmpeg 输出：把 wav 内容写入最后一个参数（输出路径）
		if err := os.WriteFile(args[len(args)-1], wavBytes, 0o644); err != nil {
			t.Errorf("写入模拟 wav 失败: %v", err)
		}
		return nil, nil
	}

	var gotPath, gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"你好世界"}}]}`)
	}))
	defer srv.Close()

	p := &dashscopeSpeechProvider{
		cfg:    config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "test-key", Model: "qwen3-asr-flash"},
		client: srv.Client(),
		run:    run,
	}

	segs, err := p.Transcribe(context.Background(), []byte("input-audio-bytes"), loader.SpeechOptions{})
	if err != nil {
		t.Fatalf("Transcribe 失败: %v", err)
	}
	if len(segs) != 1 || segs[0].Text != "你好世界" {
		t.Fatalf("应返回单段「你好世界」，实际 %+v", segs)
	}
	if segs[0].StartMs != 0 || segs[0].EndMs != 0 {
		t.Errorf("qwen ASR 无时间戳，应为 0，实际 %+v", segs[0])
	}

	// 转码命令参数
	if !containsArgs(ffmpegArgs, "-ar", "16000") ||
		!containsArgs(ffmpegArgs, "-ac", "1") ||
		!containsArgs(ffmpegArgs, "-c:a", "pcm_s16le") {
		t.Errorf("ffmpeg 参数应含 16kHz 单声道 PCM 转码，实际 %v", ffmpegArgs)
	}

	// 请求目标与鉴权
	if gotPath != "/v1/chat/completions" {
		t.Errorf("请求路径应为 /v1/chat/completions，实际 %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("鉴权头错误: %q", gotAuth)
	}

	// 请求体结构
	var req dashscopeASRRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("请求体解析失败: %v", err)
	}
	if req.Model != "qwen3-asr-flash" {
		t.Errorf("model 应为 qwen3-asr-flash，实际 %q", req.Model)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 1 {
		t.Fatalf("messages 结构错误: %+v", req.Messages)
	}
	c := req.Messages[0].Content[0]
	if c.Type != "input_audio" || c.InputAudio == nil {
		t.Fatalf("content 应为 input_audio: %+v", c)
	}
	if !strings.HasPrefix(c.InputAudio.Data, "data:audio/wav;base64,") {
		t.Errorf("audio data 应为 wav data URL，实际前缀 %q", c.InputAudio.Data[:min(40, len(c.InputAudio.Data))])
	}
}

// 服务端非 200：返回可读错误
func TestDashscopeTranscribeServerError(t *testing.T) {
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		_ = os.WriteFile(args[len(args)-1], []byte("x"), 0o644)
		return nil, nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"internal boom"}}`)
	}))
	defer srv.Close()

	p := &dashscopeSpeechProvider{
		cfg:    config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"},
		client: srv.Client(),
		run:    run,
	}
	_, err := p.Transcribe(context.Background(), []byte("x"), loader.SpeechOptions{})
	if err == nil || !strings.Contains(err.Error(), "multimedia.speech") {
		t.Fatalf("应返回 speech 服务错误，实际 %v", err)
	}
}

// 转码失败：返回可读错误
func TestDashscopeTranscribeFFmpegError(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	p := &dashscopeSpeechProvider{
		cfg:    config.MultimediaServiceConfig{BaseURL: "http://127.0.0.1:1", APIKey: "k", Model: "m"},
		client: newHTTPClient(1),
		run:    run,
	}
	_, err := p.Transcribe(context.Background(), []byte("x"), loader.SpeechOptions{})
	if err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("应返回 ffmpeg 转码错误，实际 %v", err)
	}
}
