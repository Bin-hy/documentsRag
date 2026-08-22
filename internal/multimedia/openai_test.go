package multimedia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// 未配置 APIKey 时构造函数返回 nil（能力缺失由 CheckCapabilities 暴露）
func TestNewProviderNilWhenNotConfigured(t *testing.T) {
	if v := NewVisionProvider(config.MultimediaServiceConfig{}); v != nil {
		t.Error("未配置 APIKey 时 NewVisionProvider 应返回 nil")
	}
	if s := NewSpeechProvider(config.MultimediaServiceConfig{}); s != nil {
		t.Error("未配置 APIKey 时 NewSpeechProvider 应返回 nil")
	}
}

// 视觉理解：请求体含 base64 data URI，响应 content 正确返回
func TestVisionDescribe(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"描述：一张包含文字的截图"}}]}`))
	}))
	defer srv.Close()

	p := NewVisionProvider(config.MultimediaServiceConfig{
		Provider: "openai_compat", BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-4o", Timeout: 5,
	})
	out, err := p.Describe(context.Background(), []byte("fake-image-bytes"), loader.VisionOptions{Filename: "a.png"})
	if err != nil {
		t.Fatalf("Describe 失败: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("请求路径应为 /v1/chat/completions，实际 %s", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization 应为 Bearer sk-test，实际 %q", gotAuth)
	}
	msgs := gotBody["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	img := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(img, "data:") || !strings.Contains(img, ";base64,") {
		t.Errorf("image_url 应为 base64 data URI，实际 %q", img)
	}
	if !strings.Contains(out, "截图") {
		t.Errorf("应返回模型 content，实际 %q", out)
	}
}

// 视觉理解：HTTP 错误映射为可读错误
func TestVisionHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	p := NewVisionProvider(config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "bad", Model: "m", Timeout: 5})
	_, err := p.Describe(context.Background(), []byte("x"), loader.VisionOptions{})
	if err == nil {
		t.Fatal("HTTP 401 应返回错误")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("错误应含 HTTP 状态码，实际 %v", err)
	}
}

// 语音转写：multipart 请求体字段正确，分段（秒→毫秒）解析正确
func TestSpeechTranscribe(t *testing.T) {
	var gotPath, gotAuth string
	var gotForm map[string]string
	var hasFile bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
		}
		gotForm = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			gotForm[k] = v[0]
		}
		hasFile = r.MultipartForm.File["file"] != nil
		_, _ = w.Write([]byte(`{"text":"你好 世界","segments":[{"start":0.0,"end":2.5,"text":"你好"},{"start":2.5,"end":5.0,"text":"世界"}]}`))
	}))
	defer srv.Close()

	p := NewSpeechProvider(config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "sk-2", Model: "whisper-1", Timeout: 5})
	segs, err := p.Transcribe(context.Background(), []byte("fake-audio"), loader.SpeechOptions{Filename: "meeting.mp3"})
	if err != nil {
		t.Fatalf("Transcribe 失败: %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("请求路径应为 /v1/audio/transcriptions，实际 %s", gotPath)
	}
	if gotAuth != "Bearer sk-2" {
		t.Errorf("Authorization 应为 Bearer sk-2，实际 %q", gotAuth)
	}
	if !hasFile {
		t.Error("multipart 应包含 file 字段")
	}
	if gotForm["model"] != "whisper-1" || gotForm["timestamp_granularities[]"] != "segment" {
		t.Errorf("multipart 字段不正确: %v", gotForm)
	}
	if len(segs) != 2 {
		t.Fatalf("应解析出 2 段，实际 %d", len(segs))
	}
	if segs[0].StartMs != 0 || segs[0].EndMs != 2500 || segs[1].StartMs != 2500 {
		t.Errorf("时间戳（毫秒）解析错误: %+v", segs)
	}
}

// 语音转写：无 segments 时兜底单段
func TestSpeechTranscribeFallbackSingleSegment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"只有文本没有分段"}`))
	}))
	defer srv.Close()

	p := NewSpeechProvider(config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "k", Model: "w", Timeout: 5})
	segs, err := p.Transcribe(context.Background(), []byte("x"), loader.SpeechOptions{})
	if err != nil {
		t.Fatalf("Transcribe 失败: %v", err)
	}
	if len(segs) != 1 || segs[0].Text != "只有文本没有分段" {
		t.Errorf("兜底单段解析错误: %+v", segs)
	}
}
