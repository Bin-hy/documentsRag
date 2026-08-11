package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MyMCPKey 用户 MCP 凭据视图（不含 Secret）
type MyMCPKey struct {
	ID         string   `json:"id"`
	Enabled    bool     `json:"enabled"`
	MCPTools   []string `json:"mcp_tools"`
	MCPKBScope string   `json:"mcp_kb_scope"`
	MCPKBIDs   []string `json:"mcp_kb_ids"`
}

// MyMCPStatus 「我的 MCP」面板数据（spec F7/F10）
type MyMCPStatus struct {
	GlobalEnabled bool      `json:"global_enabled"` // 全局 mcp.enabled（bootstrap 管部署级）
	Key           *MyMCPKey `json:"key"`            // 我的凭据；无则 null
	MCPPath       string    `json:"mcp_path"`       // 配置的端点路径（连接信息用）
}

// CreateMyKeyResult 创建凭据响应（明文仅此一次）
type CreateMyKeyResult struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// requireUser 校验会话用户身份（OIDC 登录），返回 UserID；非会话 → 403（spec F8：用户自助仅会话）
func (h *handler) requireUser(c *gin.Context) (string, bool) {
	id := auth.IdentityOf(c)
	if id.Kind != auth.KindUser || id.UserID == "" {
		Fail(c, CodeForbidden, "需要登录用户身份（请使用账号登录）")
		return "", false
	}
	return id.UserID, true
}

// myMCPStatus 查询我的 MCP 面板数据
//
//	@Summary		我的 MCP 状态
//	@Description	返回全局 MCP 开关、我的 MCP 凭据（无则 null）、端点路径（登录用户）
//	@Tags			MCP
//	@Produce		json
//	@Success		200	{object}	Response{data=MyMCPStatus}
//	@Failure		401	{object}	Response
//	@Failure		403	{object}	Response
//	@Security		BearerAuth
//	@Router			/api/v1/mcp/my/status [get]
func (h *handler) myMCPStatus(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	status := MyMCPStatus{
		GlobalEnabled: h.globalMCPEnabled(),
		MCPPath:       h.globalMCPPath(),
	}
	key, err := h.store.GetAPIKeyByOwner(c.Request.Context(), userID)
	if err != nil {
		slog.Warn("查询我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "查询凭据失败")
		return
	}
	if key != nil {
		status.Key = &MyMCPKey{
			ID: key.ID, Enabled: key.Enabled,
			MCPTools:   orEmptyStrings(key.MCPTools),
			MCPKBScope: key.MCPKBScope,
			MCPKBIDs:   orEmptyStrings(key.MCPKBIDs),
		}
	}
	OK(c, status)
}

// myMCPCreateKey 生成我的 MCP 凭据（每用户至多一个；已有 → 409）
//
//	@Summary		生成我的 MCP Key
//	@Description	为当前用户生成 MCP 访问凭据（绑定用户，每用户至多一个）；已有凭据返回 409，需吊销后重建。明文仅此一次返回。
//	@Tags			MCP
//	@Produce		json
//	@Success		200	{object}	Response{data=CreateMyKeyResult}
//	@Failure		401	{object}	Response
//	@Failure		403	{object}	Response
//	@Failure		409	{object}	Response
//	@Security		BearerAuth
//	@Router			/api/v1/mcp/my/key [post]
func (h *handler) myMCPCreateKey(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	existing, err := h.store.GetAPIKeyByOwner(ctx, userID)
	if err != nil {
		slog.Warn("查询我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "查询凭据失败")
		return
	}
	if existing != nil {
		Fail(c, CodeConflict, "已有 MCP 凭据，请先吊销后重新生成")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		Fail(c, CodeInternal, "生成 Key 失败")
		return
	}
	token := "binrag_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	name := "mcp-" + userID
	if len(name) > 12 {
		name = name[:12]
	}

	key := store.APIKey{
		ID:      uuid.New().String(),
		Name:    name,
		KeyHash: hex.EncodeToString(sum[:]),
		Enabled: true,
		OwnerID: userID,
	}
	if err := h.store.CreateAPIKey(ctx, key); err != nil {
		slog.Warn("创建我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "创建凭据失败")
		return
	}
	OK(c, CreateMyKeyResult{ID: key.ID, Key: token})
}

// myMCPToggleKey 启停我的 MCP 凭据
//
//	@Summary		启停我的 MCP Key
//	@Description	启用/停用当前用户的 MCP 凭据（停用后 MCP 调用拒绝）
//	@Tags			MCP
//	@Accept			json
//	@Produce		json
//	@Param			body	body		object{enabled=bool}	true	"启用状态"
//	@Success		200		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		403		{object}	Response
//	@Failure		404		{object}	Response
//	@Security		BearerAuth
//	@Router			/api/v1/mcp/my/key/toggle [post]
func (h *handler) myMCPToggleKey(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}
	ctx := c.Request.Context()
	key, err := h.store.GetAPIKeyByOwner(ctx, userID)
	if err != nil {
		slog.Warn("查询我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "查询凭据失败")
		return
	}
	if key == nil {
		Fail(c, CodeNotFound, "暂无 MCP 凭据，请先生成")
		return
	}
	if err := h.store.SetAPIKeyEnabled(ctx, key.ID, req.Enabled); err != nil {
		slog.Warn("更新凭据状态失败", "err", err)
		Fail(c, CodeInternal, "更新凭据状态失败")
		return
	}
	OK(c, gin.H{"id": key.ID, "enabled": req.Enabled})
}

// myMCPDeleteKey 吊销我的 MCP 凭据
//
//	@Summary		吊销我的 MCP Key
//	@Description	删除当前用户的 MCP 凭据（立即失效，不可恢复）
//	@Tags			MCP
//	@Produce		json
//	@Success		200	{object}	Response
//	@Failure		401	{object}	Response
//	@Failure		403	{object}	Response
//	@Failure		404	{object}	Response
//	@Security		BearerAuth
//	@Router			/api/v1/mcp/my/key [delete]
func (h *handler) myMCPDeleteKey(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	key, err := h.store.GetAPIKeyByOwner(ctx, userID)
	if err != nil {
		slog.Warn("查询我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "查询凭据失败")
		return
	}
	if key == nil {
		Fail(c, CodeNotFound, "暂无 MCP 凭据")
		return
	}
	if err := h.store.DeleteAPIKey(ctx, key.ID); err != nil {
		slog.Warn("吊销凭据失败", "err", err)
		Fail(c, CodeInternal, "吊销凭据失败")
		return
	}
	OK(c, gin.H{"id": key.ID})
}

// myMCPUpdatePermissions 配置我的 MCP 权限（Tool 白名单 + 知识库范围，kb_ids 须为自己的知识库）
//
//	@Summary		配置我的 MCP 权限
//	@Description	全量更新当前用户 MCP 凭据的 Tool 白名单与知识库范围；知识库可选项限于用户自己的知识库（越权 id → 400）
//	@Tags			MCP
//	@Accept			json
//	@Produce		json
//	@Param			body	body		store.APIKeyPermissions	true	"MCP 权限配置"
//	@Success		200		{object}	Response
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		403		{object}	Response
//	@Failure		404		{object}	Response
//	@Security		BearerAuth
//	@Router			/api/v1/mcp/my/key/permissions [put]
func (h *handler) myMCPUpdatePermissions(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
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
	ctx := c.Request.Context()
	key, err := h.store.GetAPIKeyByOwner(ctx, userID)
	if err != nil {
		slog.Warn("查询我的 MCP 凭据失败", "err", err)
		Fail(c, CodeInternal, "查询凭据失败")
		return
	}
	if key == nil {
		Fail(c, CodeNotFound, "暂无 MCP 凭据，请先生成")
		return
	}
	// 知识库范围限定（spec F9）：kb_ids 必须 ∈ 用户自己的知识库
	if len(req.MCPKBIDs) > 0 {
		ownerKBs, err := h.store.ListKBsByOwner(ctx, userID)
		if err != nil {
			slog.Warn("查询用户知识库失败", "err", err)
			Fail(c, CodeInternal, "查询知识库失败")
			return
		}
		ownerSet := make(map[string]struct{}, len(ownerKBs))
		for _, kb := range ownerKBs {
			ownerSet[kb.ID] = struct{}{}
		}
		for _, id := range req.MCPKBIDs {
			if _, ok := ownerSet[id]; !ok {
				Fail(c, CodeBadRequest, "知识库不在你的可访问范围内: "+id)
				return
			}
		}
	}
	if err := h.store.UpdateAPIKeyPermissions(ctx, key.ID, req); err != nil {
		slog.Warn("更新凭据权限失败", "err", err)
		Fail(c, CodeInternal, "更新权限失败")
		return
	}
	OK(c, gin.H{"id": key.ID})
}

// globalMCPEnabled 全局 MCP 开关（配置管理器未初始化视为关闭）
func (h *handler) globalMCPEnabled() bool {
	if h.cfgMgr == nil {
		return false
	}
	cfg := h.cfgMgr.Current()
	return cfg != nil && cfg.Server.MCP.Enabled
}

// globalMCPPath 全局 MCP 端点路径（默认 /mcp）
func (h *handler) globalMCPPath() string {
	if h.cfgMgr == nil {
		return "/mcp"
	}
	cfg := h.cfgMgr.Current()
	if cfg == nil || cfg.Server.MCP.Path == "" {
		return "/mcp"
	}
	return cfg.Server.MCP.Path
}
