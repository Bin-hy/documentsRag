package api

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// jwtShapeRe JWT 三段式结构判别（不使用 binrag_ 前缀作为认证分支条件）：
// header.payload.signature，均为 base64url 字符。
// 符合该形态的 token 才进入 JWT 本地验签；验签失败直接 401，不再尝试 API Key。
var jwtShapeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

// Auth 认证中间件（Authorization: Bearer <token>），同时接受两种凭据：
//   - 会话 JWT（登录用户）：符合三段式结构才本地验签，成功 → Kind=oidc Identity；失败 → 直接 401
//   - 系统级 API Key（含 bootstrap，任意字符串格式）：走现有 SHA-256 查库流程
//
// enabled 为 false 时放行（本地开发用）。认证路径无外部网络调用（N5）。
func Auth(s store.Store, authMgr *auth.Manager, enabled bool, bootstrapKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		var token string
		if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
			token = header[7:]
		}
		if token == "" {
			Fail(c, CodeUnauthorized, "缺少或无效的 Authorization 头")
			c.Abort()
			return
		}

		// JWT 形态：本地验签（一次），失败直接 401，不再查询 API Key
		if jwtShapeRe.MatchString(token) {
			claims, err := authMgr.VerifyJWT(token)
			if err != nil {
				slog.Warn("会话 JWT 校验失败", "err", err)
				Fail(c, CodeUnauthorized, "无效或过期的会话")
				c.Abort()
				return
			}
			auth.SetIdentity(c, auth.Identity{Kind: auth.KindUser, UserID: claims.UserID, Provider: claims.Provider})
			c.Next()
			return
		}

		// 非 JWT 形态：现有 API Key 流程（至多一次查询）
		sum := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(sum[:])

		ctx := c.Request.Context()
		key, err := s.GetAPIKeyByHash(ctx, hash)
		if err != nil {
			slog.Warn("API Key 校验出错", "err", err)
			Fail(c, CodeInternal, "内部错误")
			c.Abort()
			return
		}
		if key == nil || !key.Enabled {
			Fail(c, CodeUnauthorized, "无效或已停用的 API Key")
			c.Abort()
			return
		}

		_ = s.TouchAPIKey(ctx, key.ID)
		isBootstrap := bootstrapKey != "" && token == bootstrapKey
		auth.SetIdentity(c, auth.Identity{Kind: auth.KindAPIKey, APIKeyID: key.ID, IsBootstrap: isBootstrap})
		c.Set("api_key_id", key.ID)
		c.Set("is_bootstrap", isBootstrap)
		c.Next()
	}
}

// Logger 请求日志中间件（方法 / 路径 / 状态码 / 耗时）
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("HTTP 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"耗时ms", time.Since(start).Milliseconds(),
		)
	}
}

// CORS 跨域中间件（全放开，供前端调用）
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// RateLimit 全局限流中间件；qps <= 0 时不限制
func RateLimit(qps int) gin.HandlerFunc {
	if qps <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	limiter := rate.NewLimiter(rate.Limit(qps), qps)
	return func(c *gin.Context) {
		if !limiter.Allow() {
			Fail(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
