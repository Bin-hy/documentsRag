// Package auth 承载 BinRag 身份与会话逻辑：
// 多登录 Provider（自定义 OIDC + 内置 GitHub OAuth2）、JWT 会话签发/验签、
// 一次性 state/ticket 票据、当前请求身份模型。
// 本包不依赖 internal/store，仅处理身份信息；用户持久化由 API 层调用 store 完成。
package auth

import (
	"github.com/gin-gonic/gin"
)

// identityKey gin.Context 中 Identity 的键
const identityKey = "auth.identity"

// Identity 当前请求身份（由认证中间件写入 gin.Context）。
// 不持有 *store.User：完整用户信息由上层按 UserID 按需查询。
type Identity struct {
	// Kind 登录方式："apikey"（系统级 API Key）| "oidc"（登录用户，含 GitHub OAuth2）
	Kind string
	// APIKeyID 仅 Kind=apikey 时有效
	APIKeyID string
	// IsBootstrap 是否 bootstrap Key（保留现有高权限标记语义）
	IsBootstrap bool
	// UserID 仅 Kind=oidc 时有效（users.id）
	UserID string
	// Provider 仅 Kind=oidc 时有效（github / 自定义 OIDC 标识）
	Provider string
}

// Identity 类型常量
const (
	KindAPIKey = "apikey"
	KindUser   = "oidc"
)

// SetIdentity 将身份写入 gin.Context
func SetIdentity(c *gin.Context, id Identity) {
	c.Set(identityKey, id)
}

// IdentityOf 读取当前请求身份；未认证（无中间件写入）时返回零值 Identity
func IdentityOf(c *gin.Context) Identity {
	if v, ok := c.Get(identityKey); ok {
		if id, ok := v.(Identity); ok {
			return id
		}
	}
	return Identity{}
}

// IsSystemKey 当前身份是否为系统级 API Key
func (id Identity) IsSystemKey() bool { return id.Kind == KindAPIKey }
