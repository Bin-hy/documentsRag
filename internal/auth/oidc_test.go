package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testClientID     = "test-client"
	testClientSecret = "test-secret"
	testSubject      = "oidc-subject-123"
	testNonce        = "nonce-abc-123"
)

// oidcTestServer 模拟 OIDC issuer：discovery + JWKS + token 端点
type oidcTestServer struct {
	server *httptest.Server
	issuer string
	// signIDToken 由各测试场景注入，决定 /token 返回的 id_token
	signIDToken func() (string, error)
}

// newOIDCTestServer 生成 RSA 密钥对并启动 mock OIDC server
func newOIDCTestServer(t *testing.T) (*oidcTestServer, *rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败: %v", err)
	}
	const kid = "test-kid"

	srv := &oidcTestServer{}
	srv.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                srv.issuer,
				"authorization_endpoint":                srv.issuer + "/authorize",
				"token_endpoint":                        srv.issuer + "/token",
				"jwks_uri":                              srv.issuer + "/jwks",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			jwk := jose.JSONWebKey{Key: &priv.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"}
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
		case "/token":
			if srv.signIDToken == nil {
				http.Error(w, "no token generator", http.StatusInternalServerError)
				return
			}
			idToken, err := srv.signIDToken()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-xxx",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     idToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	srv.issuer = srv.server.URL
	t.Cleanup(srv.server.Close)
	return srv, priv, kid
}

// signRS256 用给定私钥签发带完整 claims 的 id_token（kid 匹配 JWKS）
func signRS256(t *testing.T, priv *rsa.PrivateKey, kid string, mut func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   "",
		"aud":   testClientID,
		"sub":   testSubject,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": testNonce,
		"name":  "Test User",
		"email": "test@example.com",
	}
	if mut != nil {
		mut(claims)
	}
	if _, ok := claims["iss"]; !ok || claims["iss"] == "" {
		claims["iss"] = ""
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 id_token 失败: %v", err)
	}
	return signed
}

// newTestOIDCProvider 构建指向 mock server 的 oidcProvider（严格模式）
func newTestOIDCProvider(t *testing.T, issuer string) *oidcProvider {
	return newTestOIDCProviderOpt(t, issuer, false)
}

// newTestOIDCProviderOpt 可指定 permissive_sub 模式
func newTestOIDCProviderOpt(t *testing.T, issuer string, permissive bool) *oidcProvider {
	t.Helper()
	p, err := NewOIDCProvider(context.Background(), config.ProviderConfig{
		Name: "company", DisplayName: "公司", ClientID: testClientID, ClientSecret: testClientSecret, Issuer: issuer,
		PermissiveSub: permissive,
	}, "http://localhost/callback")
	if err != nil {
		t.Fatalf("NewOIDCProvider 失败: %v", err)
	}
	return p
}

// 正常 ID Token → 校验通过，返回 UserInfo
func TestOIDCExchangeOK(t *testing.T) {
	srv, priv, kid := newOIDCTestServer(t)
	srv.signIDToken = func() (string, error) {
		tok := signRS256(t, priv, kid, func(c jwt.MapClaims) { c["iss"] = srv.issuer })
		return tok, nil
	}
	p := newTestOIDCProvider(t, srv.issuer)

	info, err := p.ExchangeAndVerify(context.Background(), "code-1", testNonce)
	if err != nil {
		t.Fatalf("ExchangeAndVerify 失败: %v", err)
	}
	if info.Subject != testSubject || info.Name != "Test User" {
		t.Errorf("UserInfo 错误: %+v", info)
	}
}

// AuthCodeURL 必须携带 nonce 与 state
func TestOIDCAuthCodeURL(t *testing.T) {
	srv, _, _ := newOIDCTestServer(t)
	p := newTestOIDCProvider(t, srv.issuer)
	u := p.AuthCodeURL("state-1", testNonce)
	if !containsParam(u, "nonce", testNonce) || !containsParam(u, "state", "state-1") {
		t.Errorf("AuthCodeURL 缺少 state/nonce: %s", u)
	}
}

// 测试矩阵：各类 ID Token 缺陷必须全部校验失败
func TestOIDCExchangeFailures(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(jwt.MapClaims)
		nonce string
	}{
		{"错误签名（另一私钥签发）", func(c jwt.MapClaims) {}, testNonce},
		{"错误 issuer", func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" }, testNonce},
		{"错误 audience", func(c jwt.MapClaims) { c["aud"] = "other-client" }, testNonce},
		{"过期 exp", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, testNonce},
		{"nbf 未到", func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }, testNonce},
		{"nonce 不匹配", func(c jwt.MapClaims) {}, "wrong-nonce"},
		{"nonce 缺失", func(c jwt.MapClaims) { delete(c, "nonce") }, testNonce},
		{"subject 为空", func(c jwt.MapClaims) { c["sub"] = "" }, testNonce},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, priv, kid := newOIDCTestServer(t)
			// 错误签名：用新生成的另一密钥签发（kid 仍配 JWKS 但签名不符）
			var signPriv = priv
			var signKid = kid
			if tc.name == "错误签名（另一私钥签发）" {
				other, _ := rsa.GenerateKey(rand.Reader, 2048)
				signPriv = other
				signKid = "other-kid" // JWKS 只有原 kid，verify 会因 kid 无匹配密钥失败
			}
			srv.signIDToken = func() (string, error) {
				return signRS256(t, signPriv, signKid, func(c jwt.MapClaims) {
					c["iss"] = srv.issuer
					tc.mut(c)
				}), nil
			}
			p := newTestOIDCProvider(t, srv.issuer)
			if _, err := p.ExchangeAndVerify(context.Background(), "code-x", tc.nonce); err == nil {
				t.Fatalf("%s: 应校验失败", tc.name)
			}
		})
	}
}

// discovery 失败 → NewOIDCProvider 返回错误（装配失败）
func TestOIDCDiscoveryFail(t *testing.T) {
	if _, err := NewOIDCProvider(context.Background(), config.ProviderConfig{
		Name: "bad", ClientID: "c", ClientSecret: "s", Issuer: "http://127.0.0.1:1/nonexistent",
	}, "http://localhost/cb"); err == nil {
		t.Fatal("discovery 失败应返回错误")
	}
}

// 数字 sub：严格模式失败；permissive_sub 模式成功且 subject 确定性转字符串
func TestOIDCPermissiveSub(t *testing.T) {
	// 数字 sub 的 id_token（服务商不规范实现）
	srv, priv, kid := newOIDCTestServer(t)
	srv.signIDToken = func() (string, error) {
		tok := signRS256(t, priv, kid, func(c jwt.MapClaims) {
			c["iss"] = srv.issuer
			c["sub"] = int64(12345) // 数字 sub
		})
		return tok, nil
	}

	// 严格模式：必须失败（保持规范）
	strict := newTestOIDCProvider(t, srv.issuer)
	if _, err := strict.ExchangeAndVerify(context.Background(), "code-1", testNonce); err == nil {
		t.Fatal("严格模式数字 sub 应校验失败")
	}

	// permissive 模式：成功，subject="12345"
	perm := newTestOIDCProviderOpt(t, srv.issuer, true)
	info, err := perm.ExchangeAndVerify(context.Background(), "code-1", testNonce)
	if err != nil {
		t.Fatalf("permissive 模式数字 sub 应成功: %v", err)
	}
	if info.Subject != "12345" {
		t.Errorf("数字 sub 应转为字符串 \"12345\": %q", info.Subject)
	}
}

// permissive 模式不放松其他校验：nonce 不匹配 / 错误 issuer / 过期仍失败
func TestOIDCPermissiveStillStrict(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(jwt.MapClaims)
		nonce string
	}{
		{"nonce 不匹配", func(c jwt.MapClaims) {}, "wrong"},
		{"错误 issuer", func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" }, testNonce},
		{"过期 exp", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, testNonce},
		{"aud 不含 client_id", func(c jwt.MapClaims) { c["aud"] = "other-client" }, testNonce},
		{"sub 为布尔", func(c jwt.MapClaims) { c["sub"] = true }, testNonce},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, priv, kid := newOIDCTestServer(t)
			srv.signIDToken = func() (string, error) {
				tok := signRS256(t, priv, kid, func(c jwt.MapClaims) {
					c["iss"] = srv.issuer
					c["sub"] = int64(42)
					tc.mut(c)
				})
				return tok, nil
			}
			p := newTestOIDCProviderOpt(t, srv.issuer, true)
			if _, err := p.ExchangeAndVerify(context.Background(), "code-x", tc.nonce); err == nil {
				t.Fatalf("%s: permissive 模式也应收敛失败", tc.name)
			}
		})
	}
}

func containsParam(u, key, want string) bool {
	// 简单子串级校验：授权 URL 以 ?key=value 或 &key=value 形式携带
	marker := key + "=" + want
	for i := 0; i+len(marker) <= len(u); i++ {
		if u[i:i+len(marker)] == marker {
			before := byte('?')
			if i > 0 {
				before = u[i-1]
			}
			if before == '?' || before == '&' {
				return true
			}
		}
	}
	return false
}
