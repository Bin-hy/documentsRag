package auth

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtIssuer 会话 JWT 的签发者标识（Sign/Verify 两侧一致）
const jwtIssuer = "binrag"

// SessionClaims 会话 JWT 载荷。
// 不含展示性 Name：Name 属展示信息，由上层按 UserID 查 store 获取。
type SessionClaims struct {
	UserID   string `json:"uid"`
	Provider string `json:"provider"`
	jwt.RegisteredClaims
}

// Signer HS256 会话 JWT 签发/验签器。
// 密钥由 Manager 启动时一次性提供（配置 jwt_secret 或自动生成），Sign 不再生成密钥。
type Signer struct {
	secret []byte
}

// NewSigner 创建签名器；secret 为空时生成 32 字节随机密钥（仅进程内持有，重启后旧会话失效）。
// 生成时机在启动期（NewManager），保证同一进程已签发 JWT 可持续验证。
func NewSigner(secret string) (*Signer, error) {
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("生成 JWT 密钥失败: %w", err)
		}
		return &Signer{secret: b}, nil
	}
	return &Signer{secret: []byte(secret)}, nil
}

// Sign 签发会话 JWT：显式设置 iss=binrag、iat=now、exp=now+ttl
func (s *Signer) Sign(userID, provider string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := SessionClaims{
		UserID:   userID,
		Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Verify 校验会话 JWT：
//   - 仅接受 HS256（拒绝 alg=none 及其他算法，WithValidMethods 强制）
//   - 校验签名、iss=binrag、exp 未过期、nbf（存在时）与 iat 合法性（jwt/v5 内置）
//
// 返回解码后的载荷；任何校验失败返回 error。
func (s *Signer) Verify(token string) (*SessionClaims, error) {
	claims := &SessionClaims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("不支持的签名算法: %v", t.Header["alg"])
			}
			return s.secret, nil
		},
		jwt.WithIssuer(jwtIssuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
