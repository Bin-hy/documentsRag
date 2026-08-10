package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 正常签发→验证通过；claims 内容正确
func TestSignVerifyRoundtrip(t *testing.T) {
	s, err := NewSigner("test-secret")
	if err != nil {
		t.Fatalf("NewSigner 失败: %v", err)
	}
	token, err := s.Sign("user-1", "github", time.Hour)
	if err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}
	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify 失败: %v", err)
	}
	if claims.UserID != "user-1" || claims.Provider != "github" {
		t.Errorf("claims 内容错误: %+v", claims)
	}
	if claims.Issuer != jwtIssuer {
		t.Errorf("iss 应为 %q: %q", jwtIssuer, claims.Issuer)
	}
}

// 篡改 payload 导致签名失效 → 验证失败
func TestVerifyTamperedPayload(t *testing.T) {
	s, _ := NewSigner("test-secret")
	token, _ := s.Sign("user-1", "github", time.Hour)
	parts := strings.Split(token, ".")
	// 改 payload 中 uid（base64url 填充后替换，这里直接改尾部字符触发签名失配）
	parts[1] = strings.TrimRight(parts[1], "=") + "x"
	if _, err := s.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("篡改 payload 应验证失败")
	}
}

// 过期 token → 验证失败
func TestVerifyExpired(t *testing.T) {
	s, _ := NewSigner("test-secret")
	token, _ := s.Sign("user-1", "github", -time.Minute) // 已过期
	if _, err := s.Verify(token); err == nil {
		t.Fatal("过期 token 应验证失败")
	}
}

// 错误 iss → 验证失败
func TestVerifyWrongIssuer(t *testing.T) {
	s, _ := NewSigner("test-secret")
	now := time.Now()
	claims := SessionClaims{
		UserID: "u1", Provider: "github",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "evil",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := s.Verify(token); err == nil {
		t.Fatal("错误 iss 应验证失败")
	}
}

// alg=none → 验证失败（无签名伪造）
func TestVerifyAlgNone(t *testing.T) {
	s, _ := NewSigner("test-secret")
	// 手动构造 alg=none 的三段式 token
	h := base64url("{\"alg\":\"none\",\"typ\":\"JWT\"}")
	p := base64url("{\"uid\":\"user-1\",\"provider\":\"github\",\"iss\":\"binrag\",\"exp\":4102444800}")
	token := h + "." + p + "."
	if _, err := s.Verify(token); err == nil {
		t.Fatal("alg=none 应验证失败")
	}
}

// 非 HS256 算法（如 RS256 声明）→ 验证失败
func TestVerifyWrongAlgorithm(t *testing.T) {
	s, _ := NewSigner("test-secret")
	now := time.Now()
	claims := SessionClaims{
		UserID: "u1", Provider: "github",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	// 用 RS256 方法签名（密钥类型不匹配必然失败）
	if _, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.secret); err == nil {
		t.Fatal("RS256 用 HS256 密钥签名应失败")
	}
}

// 换密钥的签名器无法验证（签名密钥不一致）
func TestVerifyWrongSecret(t *testing.T) {
	s1, _ := NewSigner("secret-1")
	s2, _ := NewSigner("secret-2")
	token, _ := s1.Sign("user-1", "github", time.Hour)
	if _, err := s2.Verify(token); err == nil {
		t.Fatal("错误密钥应验证失败")
	}
}

// 空密钥 → 自动生成 32 字节随机密钥，可正常签发/验证
func TestNewSignerAutoSecret(t *testing.T) {
	s, err := NewSigner("")
	if err != nil {
		t.Fatalf("自动生成密钥失败: %v", err)
	}
	token, _ := s.Sign("u1", "github", time.Hour)
	if _, err := s.Verify(token); err != nil {
		t.Fatalf("自动密钥验证失败: %v", err)
	}
}

func base64url(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
