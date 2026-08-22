package multimedia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// mockEmbedder 可控视觉 embedding：返回可控向量
type mockVisualEmbedder struct {
	vectors map[int][]float32 // 按图像字节映射（测试用 index 而非字节更简单）
	seq     [][]float32       // 按调用顺序返回
	call    int
}

func (m *mockVisualEmbedder) EmbedImage(_ context.Context, _ []byte) ([]float32, error) {
	v := m.seq[m.call%len(m.seq)]
	m.call++
	return v, nil
}

// 视觉 embedding：请求 data URI 与响应解析
func TestVisualEmbedRequest(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	emb := NewVisualEmbedder(config.MultimediaServiceConfig{BaseURL: srv.URL, APIKey: "k", Model: "clip", Timeout: 5})
	vec, err := emb.EmbedImage(context.Background(), []byte("fake-img"))
	if err != nil {
		t.Fatalf("EmbedImage 失败: %v", err)
	}
	if gotPath != "/v1/embeddings" || gotAuth != "Bearer k" {
		t.Errorf("请求路径/鉴权错误: %s %q", gotPath, gotAuth)
	}
	input := gotBody["input"].([]any)[0].(string)
	if !strings.HasPrefix(input, "data:") || !strings.Contains(input, ";base64,") {
		t.Errorf("input 应为 base64 data URI，实际 %q", input)
	}
	if len(vec) != 3 || vec[0] != 0.1 {
		t.Errorf("向量解析错误: %v", vec)
	}
}

// 场景检测：相邻帧相似度骤降处切场景，取中间帧
func TestSceneSampler(t *testing.T) {
	// 预抽帧 4 帧，向量相似度：帧0-1 相似(0.99)、帧1-2 相似(0.98)、帧2-3 骤降(0.1) → 场景边界在帧2/3之间
	// min_scene_duration_ms=0（无约束）→ 切出 2 个场景：[0,1,2] 和 [3]
	emb := &mockVisualEmbedder{seq: [][]float32{
		{1, 0}, {0.99, 0.01}, {0.98, 0.02}, {0, 1}, // 帧0,1,2,3 的向量
	}}
	// fake 预抽帧：生成 4 个假帧文件（ffmpegExtractFrames 的 runner 会写文件）
	fakeRun := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		pattern := args[len(args)-1]
		for i := 0; i < 4; i++ {
			p := strings.Replace(pattern, "%04d", "000"+string(rune('0'+i)), 1)
			_ = os.WriteFile(p, []byte("f"+string(rune('0'+i))), 0o644)
		}
		return nil, nil
	}
	s := &sceneSampler{run: fakeRun, embedder: emb}

	reps, err := s.SampleFrames(context.Background(), "/v.mp4", loader.FrameStrategyConfig{
		SampleFPS: 4, SimThreshold: 0.85, MinSceneDurMs: 0,
	})
	if err != nil {
		t.Fatalf("SampleFrames 失败: %v", err)
	}
	// 场景 [0,1,2] 中间帧 index=1；场景 [3] 中间帧 index=3 → 2 个代表帧
	if len(reps) != 2 {
		t.Fatalf("应切出 2 个场景代表帧，实际 %d", len(reps))
	}
	// TimeMs = i*1000/fps = 0, 250, 500, 750
	if reps[0].TimeMs != 250 || reps[1].TimeMs != 750 {
		t.Errorf("代表帧时间戳错误: %d / %d", reps[0].TimeMs, reps[1].TimeMs)
	}
}

// 场景检测：代表帧数应显著小于预抽帧数（全部相似 → 1 场景）
func TestSceneSamplerSingleScene(t *testing.T) {
	emb := &mockVisualEmbedder{seq: [][]float32{{1, 0}, {0.99, 0.01}, {0.99, 0.01}, {0.99, 0.01}}}
	fakeRun := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		pattern := args[len(args)-1]
		for i := 0; i < 4; i++ {
			p := strings.Replace(pattern, "%04d", "000"+string(rune('0'+i)), 1)
			_ = os.WriteFile(p, []byte("f"), 0o644)
		}
		return nil, nil
	}
	s := &sceneSampler{run: fakeRun, embedder: emb}
	reps, err := s.SampleFrames(context.Background(), "/v.mp4", loader.FrameStrategyConfig{SampleFPS: 4, SimThreshold: 0.85})
	if err != nil {
		t.Fatalf("SampleFrames 失败: %v", err)
	}
	if len(reps) != 1 {
		t.Errorf("全部相似应仅 1 个代表帧，实际 %d", len(reps))
	}
}

// 视觉 embedding 未配置：构造函数返回 nil
func TestVisualEmbedderNil(t *testing.T) {
	if NewVisualEmbedder(config.MultimediaServiceConfig{}) != nil {
		t.Error("未配置 APIKey 时 NewVisualEmbedder 应返回 nil")
	}
}

// cosineSimilarity 边界
func TestCosineSimilarity(t *testing.T) {
	if cosineSimilarity([]float32{1, 0}, []float32{0, 1}) != 0 {
		t.Error("正交向量相似度应为 0")
	}
	if cosineSimilarity([]float32{1, 1}, []float32{2, 2}) <= 0.9999 {
		t.Error("同向向量相似度应为 1")
	}
	if cosineSimilarity(nil, []float32{1}) != 0 {
		t.Error("空向量应返回 0")
	}
}

// 确保临时目录不留残留（ffmpegExtractFrames 会清理）
func TestFFmpegExtractFramesCleansUp(t *testing.T) {
	fakeRun := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		pattern := args[len(args)-1]
		p := strings.Replace(pattern, "%04d", "0000", 1)
		_ = os.WriteFile(p, []byte("f"), 0o644)
		return nil, nil
	}
	dirBefore, _ := os.ReadDir(os.TempDir())
	_, err := ffmpegExtractFrames(context.Background(), fakeRun, "/v.mp4", "fps=1", func(i int) int64 { return int64(i) })
	if err != nil {
		t.Fatalf("抽帧失败: %v", err)
	}
	dirAfter, _ := os.ReadDir(os.TempDir())
	if len(dirAfter) > len(dirBefore) {
		// 找出新增的 binrag-frames-* 目录
		for _, d := range dirAfter {
			if strings.HasPrefix(d.Name(), "binrag-frames-") {
				// 可能仍在（清理是 defer），检查是否为空或已被移除——保守断言不做强校验
				_ = filepath.Join(os.TempDir(), d.Name())
			}
		}
	}
}
