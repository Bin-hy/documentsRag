package multimedia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// openaiSpeechProvider OpenAI 兼容语音转写实现（/v1/audio/transcriptions，whisper 类模型）
type openaiSpeechProvider struct {
	cfg    config.MultimediaServiceConfig
	client *http.Client
}

// speechResponse whisper verbose_json 响应：segments 含起止时间（秒）
type speechResponse struct {
	Text     string `json:"text"`
	Segments []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
}

// Transcribe 音频 → 按时间戳分段文本（timestamp_granularities=segment 请求分段）
func (p *openaiSpeechProvider) Transcribe(ctx context.Context, audio []byte, opts loader.SpeechOptions) ([]loader.SpeechSegment, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	filename := opts.Filename
	if filename == "" {
		filename = "audio.mp3"
	}
	// 文件名仅用于 Content-Disposition，服务端忽略扩展名；用 http.DetectContentType 兜底 MIME
	part, err := w.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, errProvider("speech", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, errProvider("speech", err)
	}
	_ = w.WriteField("model", p.cfg.Model)
	_ = w.WriteField("response_format", "verbose_json")
	_ = w.WriteField("timestamp_granularities[]", "segment")
	if err := w.Close(); err != nil {
		return nil, errProvider("speech", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(p.cfg.BaseURL, "/audio/transcriptions"), &buf)
	if err != nil {
		return nil, errProvider("speech", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errProvider("speech", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errProvider("speech", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300)))
	}

	var sr speechResponse
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return nil, errProvider("speech", fmt.Errorf("解析响应失败: %w", err))
	}

	segments := make([]loader.SpeechSegment, 0, len(sr.Segments))
	if len(sr.Segments) > 0 {
		for _, s := range sr.Segments {
			segments = append(segments, loader.SpeechSegment{
				StartMs: int64(s.Start * 1000),
				EndMs:   int64(s.End * 1000),
				Text:    s.Text,
			})
		}
	} else if sr.Text != "" {
		// 服务未返回分段时兜底为单段（无时间戳）
		segments = append(segments, loader.SpeechSegment{Text: sr.Text})
	}
	return segments, nil
}
