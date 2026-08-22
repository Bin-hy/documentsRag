package multimedia

import (
	"context"
	"fmt"
	"math"

	"github.com/Bin-hy/bin-rag/internal/loader"
)

// sceneSampler 两阶段场景检测抽帧策略（spec F3，plan D4）：
//  1. 低频预抽帧（fps=sample_fps）
//  2. 逐帧视觉 embedding → 相邻帧余弦相似度 < threshold 判定场景切换
//  3. 每场景取中间帧作为代表帧（避免逐帧 VLM）
type sceneSampler struct {
	run      commandRunner
	embedder loader.VisualEmbedder
}

// NewSceneSampler 创建场景检测抽帧策略
func NewSceneSampler(emb loader.VisualEmbedder) loader.FrameStrategy {
	return &sceneSampler{run: runCommand, embedder: emb}
}

// SampleFrames 返回场景代表帧（带时间戳）
func (s *sceneSampler) SampleFrames(ctx context.Context, videoPath string, cfg loader.FrameStrategyConfig) ([]loader.VideoFrame, error) {
	fps := cfg.SampleFPS
	if fps <= 0 {
		fps = 2
	}

	// 阶段一：低频预抽帧
	vf := fmt.Sprintf("fps=%d", fps)
	timeMs := func(i int) int64 { return int64(i) * 1000 / int64(fps) }
	frames, err := ffmpegExtractFrames(ctx, s.run, videoPath, vf, timeMs)
	if err != nil {
		return nil, fmt.Errorf("场景检测预抽帧失败: %w", err)
	}
	if len(frames) <= 1 {
		return frames, nil // 过短视频，全部保留
	}

	// 阶段二：逐帧 embedding（批内顺序），计算相邻帧相似度
	vectors := make([][]float32, len(frames))
	for i, f := range frames {
		v, err := s.embedder.EmbedImage(ctx, f.Data)
		if err != nil {
			return nil, fmt.Errorf("场景检测视觉 embedding 失败（t=%dms）: %w", f.TimeMs, err)
		}
		vectors[i] = v
	}

	threshold := cfg.SimThreshold
	if threshold <= 0 {
		threshold = 0.85
	}
	minDur := cfg.MinSceneDurMs

	// 切场景边界：相邻帧相似度 < threshold（受最小场景时长约束）
	boundaries := []int{0}
	lastBoundary := 0
	for i := 1; i < len(frames); i++ {
		sim := cosineSimilarity(vectors[i-1], vectors[i])
		elapsed := frames[i].TimeMs - frames[lastBoundary].TimeMs
		if sim < threshold && elapsed >= minDur {
			boundaries = append(boundaries, i)
			lastBoundary = i
		}
	}
	boundaries = append(boundaries, len(frames))

	// 阶段三：每场景取中间帧
	reps := make([]loader.VideoFrame, 0, len(boundaries)-1)
	for j := 0; j < len(boundaries)-1; j++ {
		start, end := boundaries[j], boundaries[j+1]
		if end <= start {
			continue
		}
		mid := start + (end-start)/2
		reps = append(reps, frames[mid])
	}
	return reps, nil
}

// cosineSimilarity 余弦相似度；空向量或范数为 0 返回 0（视为不相似，触发切场景）
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
