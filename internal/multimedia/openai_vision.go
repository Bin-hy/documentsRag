package multimedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
)

// openaiVisionProvider OpenAI 兼容视觉理解实现（/v1/chat/completions，image_url 传 base64 data URI）
type openaiVisionProvider struct {
	cfg    config.MultimediaServiceConfig
	client *http.Client
}

// visionPrompt 固定系统提示：要求输出可检索的图片内容描述（含版面与可识别文字）
const visionPrompt = "请详细描述这张图片的内容，包括主体、版面布局以及图中可识别的文字信息。用中文回答，直接输出描述，不要额外说明。"

type visionChatRequest struct {
	Model     string          `json:"model"`
	Messages  []visionMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

type visionMessage struct {
	Role    string          `json:"role"`
	Content []visionContent `json:"content"`
}

type visionContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Describe 图片 → 文本描述（base64 data URI 注入 image_url）
func (p *openaiVisionProvider) Describe(ctx context.Context, image []byte, opts loader.VisionOptions) (string, error) {
	mime := http.DetectContentType(image)
	body := visionChatRequest{
		Model: p.cfg.Model,
		Messages: []visionMessage{{
			Role: "user",
			Content: []visionContent{
				{Type: "text", Text: visionPrompt},
				{Type: "image_url", ImageURL: &visionImageURL{
					URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image),
				}},
			},
		}},
		MaxTokens: 1024,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", errProvider("vision", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(p.cfg.BaseURL, "/chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return "", errProvider("vision", err)
	}
	setBearer(req, p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", errProvider("vision", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", errProvider("vision", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300)))
	}

	var chatResp visionChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", errProvider("vision", fmt.Errorf("解析响应失败: %w", err))
	}
	if len(chatResp.Choices) == 0 {
		return "", errProvider("vision", fmt.Errorf("返回空 choices"))
	}
	return chatResp.Choices[0].Message.Content, nil
}
