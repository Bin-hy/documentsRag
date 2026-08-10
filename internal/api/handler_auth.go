package api

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// exchangeRequest 一次性 ticket 换 JWT 请求
type exchangeRequest struct {
	Ticket string `json:"ticket" binding:"required"`
}

// Providers 可用的登录 Provider 列表（公开，供前端渲染登录按钮；不含任何机密）
//
//	@Summary		登录 Provider 列表
//	@Description	返回已启用的三方登录 Provider（name/type/display_name），不含 client_secret 等机密；未配置时为空数组
//	@Tags			认证
//	@Produce		json
//	@Success		200	{object}	Response{data=[]auth.ProviderView}
//	@Router			/api/v1/auth/providers [get]
func (h *handler) Providers(c *gin.Context) {
	OK(c, h.authMgr.Providers())
}

// OIDCLogin 发起 OIDC 登录：302 到 issuer 授权页（携带 state+nonce）
//
//	@Summary		发起 OIDC 登录
//	@Description	按 provider 名发起 OIDC 授权码登录，302 重定向到对应 issuer 授权页
//	@Tags			认证
//	@Produce		json
//	@Param			provider	path	string	true	"OIDC provider 名（见 /auth/providers）"
//	@Success		302
//	@Failure		404	{object}	Response
//	@Router			/api/v1/auth/oidc/{provider}/login [get]
func (h *handler) OIDCLogin(c *gin.Context) {
	provider := c.Param("provider")
	p, ok := h.authMgr.Get(provider)
	if !ok || p.Type() != config.ProviderTypeOIDC {
		Fail(c, CodeNotFound, "登录 Provider 不存在")
		return
	}
	authURL, _, _, err := h.authMgr.BeginLogin(provider)
	if err != nil {
		slog.Warn("发起 OIDC 登录失败", "provider", provider, "err", err)
		Fail(c, CodeInternal, "发起登录失败")
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

// GithubLogin 发起 GitHub 登录：302 到 GitHub 授权页（携带 state）
//
//	@Summary		发起 GitHub 登录
//	@Description	GitHub 使用 OAuth2 适配（非 OIDC），302 到 GitHub 授权页
//	@Tags			认证
//	@Produce		json
//	@Success		302
//	@Failure		404	{object}	Response
//	@Router			/api/v1/auth/github/login [get]
func (h *handler) GithubLogin(c *gin.Context) {
	authURL, _, _, err := h.authMgr.BeginLogin(config.ProviderNameGithub)
	if err != nil {
		slog.Warn("发起 GitHub 登录失败", "err", err)
		Fail(c, CodeNotFound, "GitHub 登录未配置")
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

// OIDCCallback OIDC 回调：校验 state/nonce/ID Token → 建用户 → 签发一次性 ticket → 302 前端
//
//	@Summary		OIDC 回调
//	@Description	OIDC 授权码回调；成功后 302 到 /login?ticket=xxx（一次性，前端换取 JWT）。校验失败不创建会话
//	@Tags			认证
//	@Param			provider	path	string	true	"OIDC provider 名"
//	@Success		302
//	@Router			/api/v1/auth/oidc/{provider}/callback [get]
func (h *handler) OIDCCallback(c *gin.Context) {
	info, provider, err := h.authMgr.CompleteLogin(c.Request.Context(), c.Query("code"), c.Query("state"))
	if err != nil {
		slog.Warn("OIDC 回调校验失败", "provider", c.Param("provider"), "err", err)
		redirectLoginError(c, "登录失败，请重试")
		return
	}
	h.finishLogin(c, info, provider)
}

// GithubCallback GitHub 回调：校验 state → 取数字 ID → 建用户 → ticket → 302 前端
//
//	@Summary		GitHub 回调
//	@Description	GitHub OAuth2 回调；成功后 302 到 /login?ticket=xxx。校验失败不创建会话
//	@Tags			认证
//	@Success		302
//	@Router			/api/v1/auth/github/callback [get]
func (h *handler) GithubCallback(c *gin.Context) {
	info, provider, err := h.authMgr.CompleteLogin(c.Request.Context(), c.Query("code"), c.Query("state"))
	if err != nil {
		slog.Warn("GitHub 回调校验失败", "err", err)
		redirectLoginError(c, "登录失败，请重试")
		return
	}
	h.finishLogin(c, info, provider)
}

// finishLogin 回调成功公共路径：自动注册/复用用户 → 签发一次性 ticket → 302 前端登录页。
// JWT 永不进入 URL；ticket 由前端 /auth/exchange 换取 JWT。
func (h *handler) finishLogin(c *gin.Context, info *auth.UserInfo, provider string) {
	ctx := c.Request.Context()
	user, err := h.store.GetOrCreateUser(ctx, store.User{
		ID:       uuid.New().String(),
		Provider: provider,
		Subject:  info.Subject,
		Name:     info.Name,
		Email:    info.Email,
	})
	if err != nil {
		slog.Warn("创建/复用用户失败", "provider", provider, "err", err)
		redirectLoginError(c, "登录失败，请重试")
		return
	}
	ticket, err := h.authMgr.IssueTicket(user.ID, provider)
	if err != nil {
		slog.Warn("签发 ticket 失败", "err", err)
		redirectLoginError(c, "登录失败，请重试")
		return
	}
	c.Redirect(http.StatusFound, "/login?ticket="+url.QueryEscape(ticket))
}

// redirectLoginError 回调失败跳转前端登录页（错误信息明确但不泄露内部机密）
func redirectLoginError(c *gin.Context, msg string) {
	c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape(msg))
}

// ExchangeTicket 用一次性 ticket 换取会话 JWT（ticket 一次性消费，重放失败）
//
//	@Summary		换取会话 JWT
//	@Description	用回调后拿到的一次性 ticket 换取会话 JWT；ticket 仅可成功消费一次
//	@Tags			认证
//	@Accept			json
//	@Produce		json
//	@Param			body	body		exchangeRequest	true	"一次性 ticket"
//	@Success		200		{object}	Response{data=object{token=string}}
//	@Failure		401		{object}	Response
//	@Router			/api/v1/auth/exchange [post]
func (h *handler) ExchangeTicket(c *gin.Context) {
	var req exchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}
	token, err := h.authMgr.ExchangeTicket(req.Ticket)
	if err != nil {
		Fail(c, CodeUnauthorized, "ticket 无效或已过期")
		return
	}
	OK(c, gin.H{"token": token})
}

// Me 当前会话身份信息
//
//	@Summary		当前身份
//	@Description	返回当前凭据身份：apikey → {kind,is_bootstrap}；oidc → {kind,user_id,provider,name}
//	@Tags			认证
//	@Produce		json
//	@Success		200	{object}	Response
//	@Failure		401	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/auth/me [get]
func (h *handler) Me(c *gin.Context) {
	id := auth.IdentityOf(c)
	switch id.Kind {
	case auth.KindAPIKey:
		OK(c, gin.H{"kind": auth.KindAPIKey, "is_bootstrap": id.IsBootstrap})
	case auth.KindUser:
		u, err := h.store.GetUser(c.Request.Context(), id.UserID)
		if err != nil || u == nil {
			slog.Warn("查询用户失败", "user_id", id.UserID, "err", err)
			Fail(c, CodeInternal, "查询用户失败")
			return
		}
		OK(c, gin.H{"kind": auth.KindUser, "user_id": u.ID, "provider": u.Provider, "name": u.Name})
	default:
		Fail(c, CodeUnauthorized, "未认证")
	}
}
