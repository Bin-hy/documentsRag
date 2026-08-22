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

// openaiVisualEmbedder OpenAI 兼容视觉 embedding（/v1/embeddings，input 为 base64 data URI）
type openaiVisualEmbedder struct {
	cfg    config.MultimediaServiceConfig
	client *http.Client
}

// NewVisualEmbedder 创建视觉 embedding Provider；APIKey 未配置时返回 nil
func NewVisualEmbedder(cfg config.MultimediaServiceConfig) loader.VisualEmbedder {
	if !cfg.Available() {
		return nil
	}
	return &openaiVisualEmbedder{cfg: cfg, client: newHTTPClient(cfg.Timeout)}
}

type visualEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type visualEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// EmbedImage 单张图像 → 向量（base64 data URI）
func (e *openaiVisualEmbedder) EmbedImage(ctx context.Context, image []byte) ([]float32, error) {
	mime := http.DetectContentType(image)
	uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)

	body, err := json.Marshal(visualEmbedRequest{Model: e.cfg.Model, Input: []string{uri}})
	if err != nil {
		return nil, errProvider("vision_embedding", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(e.cfg.BaseURL, "/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, errProvider("vision_embedding", err)
	}
	setBearer(req, e.cfg.APIKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, errProvider("vision_embedding", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errProvider("vision_embedding", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300)))
	}

	var ve visualEmbedResponse
	if err := json.Unmarshal(respBody, &ve); err != nil {
		return nil, errProvider("vision_embedding", fmt.Errorf("解析响应失败: %w", err))
	}
	if len(ve.Data) == 0 || len(ve.Data[0].Embedding) == 0 {
		return nil, errProvider("vision_embedding", fmt.Errorf("返回空向量"))
	}
	return ve.Data[0].Embedding, nil
}
