package api

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Auth API Key 认证中间件（Authorization: Bearer <key>）
// enabled 为 false 时放行（本地开发用）
func Auth(s store.Store, enabled bool) gin.HandlerFunc {
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
		c.Set("api_key_id", key.ID)
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
