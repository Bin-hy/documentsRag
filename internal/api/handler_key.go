package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// createAPIKeyRequest 创建 Key 请求
type createAPIKeyRequest struct {
	Name string `json:"name" binding:"required"`
}

// toggleAPIKeyRequest 启停请求
type toggleAPIKeyRequest struct {
	Enabled bool `json:"enabled"` // 不能用 required：false 是合法值
}

// CreateAPIKey 创建 API Key（明文仅返回一次，库中只存 SHA-256 hash）
func (h *handler) CreateAPIKey(c *gin.Context) {
	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	// 生成 32 字节随机明文 Key
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		Fail(c, CodeInternal, "生成 Key 失败")
		return
	}
	token := "binrag_" + base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(token))
	key := store.APIKey{
		ID:        uuid.New().String(),
		Name:      req.Name,
		KeyHash:   hex.EncodeToString(sum[:]),
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	if err := h.store.CreateAPIKey(c.Request.Context(), key); err != nil {
		Fail(c, CodeInternal, "创建 API Key 失败")
		return
	}

	OK(c, gin.H{"id": key.ID, "name": key.Name, "key": token})
}

// ListAPIKeys 列出 API Key（不含 hash）
func (h *handler) ListAPIKeys(c *gin.Context) {
	keys, err := h.store.ListAPIKeys(c.Request.Context())
	if err != nil {
		Fail(c, CodeInternal, "查询 API Key 失败")
		return
	}

	type keyView struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		Enabled    bool       `json:"enabled"`
		LastUsedAt *time.Time `json:"last_used_at"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	views := make([]keyView, 0, len(keys))
	for _, k := range keys {
		views = append(views, keyView{ID: k.ID, Name: k.Name, Enabled: k.Enabled, LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt})
	}
	OK(c, views)
}

// DeleteAPIKey 删除 API Key
func (h *handler) DeleteAPIKey(c *gin.Context) {
	if err := h.store.DeleteAPIKey(c.Request.Context(), c.Param("id")); err != nil {
		Fail(c, CodeInternal, "删除 API Key 失败")
		return
	}
	OK(c, gin.H{"id": c.Param("id")})
}

// ToggleAPIKey 启用/停用 API Key
func (h *handler) ToggleAPIKey(c *gin.Context) {
	var req toggleAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	if err := h.store.SetAPIKeyEnabled(c.Request.Context(), c.Param("id"), req.Enabled); err != nil {
		Fail(c, CodeInternal, "更新 API Key 失败")
		return
	}
	OK(c, gin.H{"id": c.Param("id"), "enabled": req.Enabled})
}
