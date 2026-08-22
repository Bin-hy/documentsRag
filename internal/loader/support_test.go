package loader

import (
	"strings"
	"testing"
)

// Support：文本类型恒支持，不依赖外部能力
func TestSupportTextAlwaysSupported(t *testing.T) {
	r := NewDefaultRegistry()
	res := r.Support(FileInfo{Filename: "note.txt"})
	if !res.Supported {
		t.Fatalf("文本应恒支持，实际 false，reason=%q", res.Reason)
	}
	if res.Reason != "" {
		t.Errorf("支持时 reason 应为空，实际 %q", res.Reason)
	}
}

// Support：多媒体能力未配置时返回 false 且原因指向对应能力
func TestSupportMediaWithoutConfig(t *testing.T) {
	r := NewDefaultRegistry() // 多媒体以 nil 能力注册
	cases := []struct {
		name     string
		filename string
		cap      string
	}{
		{"音频需 speech", "a.mp3", "speech"},
		{"图片需 vision", "a.png", "vision"},
		{"视频需 vision", "a.mp4", "vision"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := r.Support(FileInfo{Filename: c.filename})
			if res.Supported {
				t.Fatalf("%s 应不支持", c.filename)
			}
			if !strings.Contains(res.Reason, c.cap) {
				t.Errorf("原因应含 %q，实际 %q", c.cap, res.Reason)
			}
		})
	}
}

// Support：speech 就绪后音频变支持
func TestSupportAudioWithSpeech(t *testing.T) {
	r := NewRegistry()
	r.Register(NewAudioParser(&mockSpeech{}))
	res := r.Support(FileInfo{Filename: "a.mp3"})
	if !res.Supported {
		t.Fatalf("speech 就绪后音频应支持，实际 false，reason=%q", res.Reason)
	}
}

// Support：未知格式返回 false，原因指向「不支持的文件格式」
func TestSupportUnknownFormat(t *testing.T) {
	r := NewDefaultRegistry()
	res := r.Support(FileInfo{Filename: "malware.exe"})
	if res.Supported {
		t.Fatal(".exe 应不支持")
	}
	if !strings.Contains(res.Reason, "不支持的文件格式") {
		t.Errorf("原因应含「不支持的文件格式」，实际 %q", res.Reason)
	}
}

// SupportedTypes：类别正确、含不支持多媒体、按 ext 升序
func TestSupportedTypesCategories(t *testing.T) {
	r := NewDefaultRegistry()
	types := r.SupportedTypes()

	byExt := map[string]SupportedType{}
	for _, st := range types {
		byExt[st.Ext] = st
	}

	// 类别
	wantCat := map[string]string{
		".txt": "text",
		".mp3": "audio",
		".png": "image",
		".mp4": "video",
	}
	for ext, cat := range wantCat {
		st, ok := byExt[ext]
		if !ok {
			t.Errorf("SupportedTypes 缺少 %s", ext)
			continue
		}
		if st.Category != cat {
			t.Errorf("%s 类别应为 %q，实际 %q", ext, cat, st.Category)
		}
	}

	// 多媒体未配置时应不支持且带原因
	for _, ext := range []string{".mp3", ".png", ".mp4"} {
		if byExt[ext].Supported {
			t.Errorf("%s 未配置能力时应不支持", ext)
		}
		if byExt[ext].Reason == "" {
			t.Errorf("%s 不支持时应有原因", ext)
		}
	}
	// 文本恒支持
	if !byExt[".txt"].Supported {
		t.Error(".txt 应支持")
	}

	// 升序
	for i := 1; i < len(types); i++ {
		if types[i-1].Ext >= types[i].Ext {
			t.Fatalf("SupportedTypes 未按 ext 升序：%s >= %s", types[i-1].Ext, types[i].Ext)
		}
	}
}
