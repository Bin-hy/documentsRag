// Package multimedia 多媒体处理能力实现层（图片/音频/视频的 ingestion 阶段能力）。
// 依赖 loader 包的能力接口与 config 包的配置结构，实现可插拔（OpenAI Compatible 默认，预留本地 VLM / Whisper 扩展）。
package multimedia

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// NewVisionProvider 创建视觉理解 Provider；APIKey 未配置（Available()==false）时返回 nil，
// 能力缺失由 Parser 的 CheckCapabilities 暴露（上传预检 400，spec F7）。
func NewVisionProvider(cfg config.MultimediaServiceConfig) loader.VisionProvider {
	if !cfg.Available() {
		return nil
	}
	return &openaiVisionProvider{cfg: cfg, client: newHTTPClient(cfg.Timeout)}
}

// NewSpeechProvider 创建语音转写 Provider；APIKey 未配置时返回 nil。
// provider 取值：openai_compat（默认，whisper 风格 /audio/transcriptions）/ dashscope（qwen ASR，chat/completions + input_audio）。
func NewSpeechProvider(cfg config.MultimediaServiceConfig) loader.SpeechProvider {
	if !cfg.Available() {
		return nil
	}
	switch cfg.Provider {
	case "dashscope":
		return NewDashscopeSpeechProvider(cfg)
	default: // openai_compat
		return &openaiSpeechProvider{cfg: cfg, client: newHTTPClient(cfg.Timeout)}
	}
}

// NewFrameStrategy 创建视频抽帧策略（fixed / scene，spec F2/F3/F4）。
// frame_strategy=scene 且视觉 embedding 未配置时返回 nil（由 parser 的 CheckCapabilities 报配置错误）。
func NewFrameStrategy(cfg config.VideoConfig) loader.FrameStrategy {
	switch cfg.FrameStrategy {
	case "scene":
		emb := NewVisualEmbedder(cfg.VisionEmbedding)
		if emb == nil {
			return nil
		}
		return NewSceneSampler(emb)
	default: // fixed
		return &fixedFrameStrategy{extractor: NewFrameExtractor()}
	}
}

func newHTTPClient(timeoutSec int) *http.Client {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
}

// endpoint 拼接 OpenAI 兼容接口地址：
//   - BaseURL 空 → 官方地址 https://api.openai.com/v1
//   - BaseURL 已含 /v1 前缀（如 http://host:8000/v1）→ 直接拼接
//   - BaseURL 未含 /v1（如 http://host:8000）→ 自动补 /v1
func endpoint(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

func setBearer(req *http.Request, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// errProvider 包装服务调用错误为可读错误（spec F7：不产生空文档/脏数据，明确失败）
func errProvider(capability string, err error) error {
	return fmt.Errorf("multimedia.%s 服务调用失败: %w", capability, err)
}

// truncate 截断错误响应体，避免长文本污染错误信息
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
