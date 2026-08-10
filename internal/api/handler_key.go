package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/Bin-hy/bin-rag/internal/auth"
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

// keyView API Key 列表视图（不含 hash，包级定义供 swag 解析）
type keyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	// MCP 权限（spec F4/F5/F6；空 = 无 MCP 权限）
	MCPTools   []string `json:"mcp_tools"`
	MCPKBScope string   `json:"mcp_kb_scope"`
	MCPKBIDs   []string `json:"mcp_kb_ids"`
}

// requireSystemKey 校验当前身份为系统级 API Key（会话 JWT 无权管理 API Key）；
// 不满足时返回 403 并中止。
func (h *handler) requireSystemKey(c *gin.Context) bool {
	if auth.IdentityOf(c).Kind != auth.KindAPIKey {
		Fail(c, CodeForbidden, "仅系统级 API Key 可管理 API Key")
		c.Abort()
		return false
	}
	return true
}

// CreateAPIKey 创建 API Key（明文仅返回一次，库中只存 SHA-256 hash）
//
//	@Summary		创建 API Key
//	@Description	创建 API Key，明文仅返回一次（后续不可再查），库中只存 SHA-256 hash；仅系统级 API Key 可操作
//	@Tags			API Key
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createAPIKeyRequest	true	"Key 名称"
//	@Success		200		{object}	Response{data=object{id=string,name=string,key=string}}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		403		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/api-keys [post]
func (h *handler) CreateAPIKey(c *gin.Context) {
	if !h.requireSystemKey(c) {
		return
	}
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
//
//	@Summary		API Key 列表
//	@Description	列出全部 API Key（不含 hash），含启用状态与最后使用时间
//	@Tags			API Key
//	@Produce		json
//	@Success		200	{object}	Response{data=[]keyView}
//	@Failure		401	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/api-keys [get]
func (h *handler) ListAPIKeys(c *gin.Context) {
	if !h.requireSystemKey(c) {
		return
	}
	keys, err := h.store.ListAPIKeys(c.Request.Context())
	if err != nil {
		Fail(c, CodeInternal, "查询 API Key 失败")
		return
	}

	views := make([]keyView, 0, len(keys))
	for _, k := range keys {
		views = append(views, keyView{
			ID: k.ID, Name: k.Name, Enabled: k.Enabled, LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt,
			// nil → 空数组：MCP 权限字段输出 [] 而非 null（历史 Key 语义 = 无权限）
			MCPTools:   orEmptyStrings(k.MCPTools),
			MCPKBScope: k.MCPKBScope,
			MCPKBIDs:   orEmptyStrings(k.MCPKBIDs),
		})
	}
	OK(c, views)
}

// orEmptyStrings nil → 空数组（JSON 输出 []）
func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// UpdateAPIKeyPermissions 更新 API Key 的 MCP 权限（全量替换，PUT 语义；仅系统级 API Key 可操作）
//
//	@Summary		更新 MCP 权限
//	@Description	全量更新 API Key 的 MCP 权限：mcp_tools（Tool 白名单，空 = 无 Tool 权限）、mcp_kb_scope（"" / "all" / "allowlist"）、mcp_kb_ids（allowlist 时的知识库白名单）；仅系统级 API Key 可操作
//	@Tags			API Key
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"API Key ID"
//	@Param			body	body		store.APIKeyPermissions	true	"MCP 权限配置"
//	@Success		200		{object}	Response{data=object{id=string}}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		403		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/api-keys/{id}/permissions [put]
func (h *handler) UpdateAPIKeyPermissions(c *gin.Context) {
	if !h.requireSystemKey(c) {
		return
	}
	var req store.APIKeyPermissions
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}
	if req.MCPKBScope != "" && req.MCPKBScope != "all" && req.MCPKBScope != "allowlist" {
		Fail(c, CodeBadRequest, "mcp_kb_scope 仅支持 \"\" / \"all\" / \"allowlist\"")
		return
	}
	if err := h.store.UpdateAPIKeyPermissions(c.Request.Context(), c.Param("id"), req); err != nil {
		Fail(c, CodeInternal, "更新 API Key 权限失败")
		return
	}
	OK(c, gin.H{"id": c.Param("id")})
}

// DeleteAPIKey 删除 API Key
//
//	@Summary		删除 API Key
//	@Description	按 ID 删除 API Key
//	@Tags			API Key
//	@Produce		json
//	@Param			id	path		string	true	"API Key ID"
//	@Success		200	{object}	Response{data=object{id=string}}
//	@Failure		401	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/api-keys/{id} [delete]
func (h *handler) DeleteAPIKey(c *gin.Context) {
	if !h.requireSystemKey(c) {
		return
	}
	if err := h.store.DeleteAPIKey(c.Request.Context(), c.Param("id")); err != nil {
		Fail(c, CodeInternal, "删除 API Key 失败")
		return
	}
	OK(c, gin.H{"id": c.Param("id")})
}

// ToggleAPIKey 启用/停用 API Key
//
//	@Summary		启停 API Key
//	@Description	启用或停用指定 API Key；停用后该 Key 请求返回 401
//	@Tags			API Key
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"API Key ID"
//	@Param			body	body		toggleAPIKeyRequest	true	"启用状态"
//	@Success		200		{object}	Response{data=object{id=string,enabled=boolean}}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/api-keys/{id}/toggle [post]
func (h *handler) ToggleAPIKey(c *gin.Context) {
	if !h.requireSystemKey(c) {
		return
	}
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
