package multimedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// dashscopeSpeechProvider 阿里云百炼 qwen ASR 语音转写实现。
// qwen ASR 不提供 OpenAI whisper 风格 /audio/transcriptions，而是走 /chat/completions，
// 音频以 input_audio + base64 data URL 传入消息（实测验证，spec F3）。
type dashscopeSpeechProvider struct {
	cfg    config.MultimediaServiceConfig
	client *http.Client
	run    commandRunner // ffmpeg 转码执行器（可注入便于单测）
}

// NewDashscopeSpeechProvider 创建 qwen ASR 转写 Provider。
func NewDashscopeSpeechProvider(cfg config.MultimediaServiceConfig) loader.SpeechProvider {
	return &dashscopeSpeechProvider{cfg: cfg, client: newHTTPClient(cfg.Timeout), run: runCommand}
}

// —— 请求/响应结构（对齐 qwen ASR 文档）——

type dashscopeASRRequest struct {
	Model      string               `json:"model"`
	Messages   []dashscopeMessage   `json:"messages"`
	Stream     bool                 `json:"stream"`
	ASROptions *dashscopeASROptions `json:"asr_options,omitempty"`
}

type dashscopeMessage struct {
	Role    string             `json:"role"`
	Content []dashscopeContent `json:"content"`
}

type dashscopeContent struct {
	Type       string               `json:"type"` // "input_audio"
	InputAudio *dashscopeInputAudio `json:"input_audio,omitempty"`
}

type dashscopeInputAudio struct {
	Data string `json:"data"` // "data:audio/wav;base64,<...>"
}

type dashscopeASROptions struct {
	EnableITN bool `json:"enable_itn"`
}

type dashscopeASRResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Transcribe 音频 → 整段转写文本。
// 流程：落盘 → ffmpeg 转 16kHz 单声道 WAV → base64 → chat/completions(input_audio) → 单段 SpeechSegment。
func (p *dashscopeSpeechProvider) Transcribe(ctx context.Context, audio []byte, _ loader.SpeechOptions) ([]loader.SpeechSegment, error) {
	// 落盘原始音频，供 ffmpeg 读取（ffmpeg 自动探测输入格式）
	in, err := os.CreateTemp("", "binrag-asr-in-*")
	if err != nil {
		return nil, errProvider("speech", fmt.Errorf("创建音频临时文件失败: %w", err))
	}
	inPath := in.Name()
	defer os.Remove(inPath)
	if _, err := in.Write(audio); err != nil {
		in.Close()
		return nil, errProvider("speech", fmt.Errorf("写入音频临时文件失败: %w", err))
	}
	if err := in.Close(); err != nil {
		return nil, errProvider("speech", fmt.Errorf("关闭音频临时文件失败: %w", err))
	}

	out, err := os.CreateTemp("", "binrag-asr-out-*.wav")
	if err != nil {
		return nil, errProvider("speech", fmt.Errorf("创建转码临时文件失败: %w", err))
	}
	outPath := out.Name()
	_ = out.Close()
	_ = os.Remove(outPath) // 让 ffmpeg 用 -y 覆盖创建
	defer os.Remove(outPath)

	// ffmpeg 转码：16kHz 单声道 PCM WAV（qwen ASR 不接受 m4a 等原始格式，实测）
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", inPath,
		"-ar", "16000",
		"-ac", "1",
		"-c:a", "pcm_s16le",
		outPath,
	}
	if _, err := p.run(ctx, "ffmpeg", args...); err != nil {
		return nil, errProvider("speech", fmt.Errorf("ffmpeg 转码失败: %w", err))
	}

	wavData, err := os.ReadFile(outPath)
	if err != nil {
		return nil, errProvider("speech", fmt.Errorf("读取转码结果失败: %w", err))
	}

	body := dashscopeASRRequest{
		Model: p.cfg.Model,
		Messages: []dashscopeMessage{{
			Role: "user",
			Content: []dashscopeContent{{
				Type:       "input_audio",
				InputAudio: &dashscopeInputAudio{Data: "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wavData)},
			}},
		}},
		Stream:     false,
		ASROptions: &dashscopeASROptions{EnableITN: false},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, errProvider("speech", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(p.cfg.BaseURL, "/chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return nil, errProvider("speech", err)
	}
	setBearer(req, p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errProvider("speech", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errProvider("speech", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300)))
	}

	var asrResp dashscopeASRResponse
	if err := json.Unmarshal(respBody, &asrResp); err != nil {
		return nil, errProvider("speech", fmt.Errorf("解析响应失败: %w", err))
	}
	if len(asrResp.Choices) == 0 || asrResp.Choices[0].Message.Content == "" {
		return nil, errProvider("speech", fmt.Errorf("返回空转写内容"))
	}

	return []loader.SpeechSegment{{Text: asrResp.Choices[0].Message.Content}}, nil
}
