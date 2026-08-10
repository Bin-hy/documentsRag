package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// signTestJWT 通过 ticket 流程签发一个会话 JWT（同时覆盖 ticket 一次性路径）
func signTestJWT(t *testing.T, m *auth.Manager, userID, provider string) string {
	t.Helper()
	tk, err := m.IssueTicket(userID, provider)
	if err != nil {
		t.Fatalf("IssueTicket 失败: %v", err)
	}
	token, err := m.ExchangeTicket(tk)
	if err != nil {
		t.Fatalf("ExchangeTicket 失败: %v", err)
	}
	return token
}

// ---------- 认证判别（T12） ----------

// JWT / API Key 双凭据；伪造三段式 JWT 直接 401 且不查 API Key
func TestAuthDispatch(t *testing.T) {
	env := newTestEnv(t)

	// 有效 API Key → 200
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey); w.Code != 200 {
		t.Fatalf("有效 API Key 应 200: %d", w.Code)
	}
	// 非 binrag_ 前缀但已登记的 API Key → 200（兼容性）
	env.store.CreateAPIKey(context.Background(), store.APIKey{ID: "key-custom", Name: "自定义", KeyHash: keyHash("custom-prefix-key"), Enabled: true})
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, "custom-prefix-key"); w.Code != 200 {
		t.Fatalf("非 binrag_ 前缀的有效 Key 应 200: %d", w.Code)
	}
	// 有效 JWT → 200
	jwtStr := signTestJWT(t, env.authMgr, "user-1", "github")
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, jwtStr); w.Code != 200 {
		t.Fatalf("有效 JWT 应 200: %d", w.Code)
	}
	// 伪造但符合三段式 JWT → 401
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, "eyJhbGciOiJIUzI1NiJ9.eyJ1aWQiOiJ1In0.fake-signature"); w.Code != 401 {
		t.Fatalf("伪造 JWT 应 401: %d", w.Code)
	}
	// 非 JWT 形态的无效凭据 → 401
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, "not-a-jwt-token"); w.Code != 401 {
		t.Fatalf("无效凭据应 401: %d", w.Code)
	}
	// 无凭据 → 401
	if w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, ""); w.Code != 401 {
		t.Fatalf("无凭据应 401: %d", w.Code)
	}
}

// ---------- /auth/me（T13） ----------

// apikey 身份：kind=apikey + is_bootstrap
func TestMeAPIKey(t *testing.T) {
	env := newTestEnv(t)
	w := doReq(t, env.router, "GET", "/api/v1/auth/me", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("me 应 200: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Kind        string `json:"kind"`
			IsBootstrap bool   `json:"is_bootstrap"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Data.Kind != "apikey" || resp.Data.IsBootstrap {
		t.Errorf("apikey 身份输出错误: %+v", resp.Data)
	}
}

// oidc 身份：kind=oidc + user_id/provider/name（按需查 store）
func TestMeOIDC(t *testing.T) {
	env := newTestEnv(t)
	u, err := env.store.GetOrCreateUser(context.Background(), store.User{
		ID: "user-1", Provider: "github", Subject: "12345", Name: "octocat",
	})
	if err != nil {
		t.Fatalf("造用户失败: %v", err)
	}
	jwtStr := signTestJWT(t, env.authMgr, u.ID, "github")
	w := doReq(t, env.router, "GET", "/api/v1/auth/me", nil, jwtStr)
	if w.Code != 200 {
		t.Fatalf("me 应 200: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Kind     string `json:"kind"`
			UserID   string `json:"user_id"`
			Provider string `json:"provider"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Data.Kind != "oidc" || resp.Data.UserID != u.ID || resp.Data.Provider != "github" || resp.Data.Name != "octocat" {
		t.Errorf("oidc 身份输出错误: %+v", resp.Data)
	}
}

// me 未认证 → 401
func TestMeUnauthorized(t *testing.T) {
	env := newTestEnv(t)
	if w := doReq(t, env.router, "GET", "/api/v1/auth/me", nil, ""); w.Code != 401 {
		t.Fatalf("未认证 me 应 401: %d", w.Code)
	}
}

// ---------- 公开接口（T14） ----------

// providers 公开可访问；未配置 provider 时为空数组
func TestProvidersPublic(t *testing.T) {
	env := newTestEnv(t)
	w := doReq(t, env.router, "GET", "/api/v1/auth/providers", nil, "")
	if w.Code != 200 {
		t.Fatalf("providers 应公开 200: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []auth.ProviderView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("空 manager 的 providers 应为空: %+v", resp.Data)
	}
}

// 未知 provider 发起登录 → 404；callback 无效 state → 302 error（不创建会话）
func TestLoginUnknownProvider(t *testing.T) {
	env := newTestEnv(t)
	if w := doReq(t, env.router, "GET", "/api/v1/auth/oidc/nope/login", nil, ""); w.Code != 404 {
		t.Fatalf("未知 provider 应 404: %d", w.Code)
	}
	w := doReq(t, env.router, "GET", "/api/v1/auth/oidc/company/callback?code=x&state=bad", nil, "")
	if w.Code != 302 {
		t.Fatalf("无效 state 回调应 302: %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/login?error=") {
		t.Errorf("回调失败应跳 /login?error=: %s", loc)
	}
}

// ---------- ticket 一次性（AC11，T13） ----------

func TestExchangeReplay(t *testing.T) {
	env := newTestEnv(t)
	tk, err := env.authMgr.IssueTicket("user-1", "github")
	if err != nil {
		t.Fatalf("IssueTicket 失败: %v", err)
	}
	// 第一次成功
	w := doReq(t, env.router, "POST", "/api/v1/auth/exchange", map[string]string{"ticket": tk}, "")
	if w.Code != 200 {
		t.Fatalf("首次 exchange 应 200: %d %s", w.Code, w.Body.String())
	}
	// 第二次（重放）失败
	w = doReq(t, env.router, "POST", "/api/v1/auth/exchange", map[string]string{"ticket": tk}, "")
	if w.Code != 401 {
		t.Fatalf("ticket 重放应 401: %d", w.Code)
	}
	// 无效 ticket
	w = doReq(t, env.router, "POST", "/api/v1/auth/exchange", map[string]string{"ticket": "fake-ticket"}, "")
	if w.Code != 401 {
		t.Fatalf("无效 ticket 应 401: %d", w.Code)
	}
}

// ---------- 完整 OIDC HTTP 登录流程（AC3/AC9/AC12，T13） ----------

// mockOIDCIssuer api 层测试用的轻量 OIDC issuer（discovery + JWKS + token）
// 返回 issuer 与 setNonce：由测试控制下一个 id_token 携带的 nonce（对应登录时生成的 nonce）
func mockOIDCIssuer(t *testing.T) (issuer string, setNonce func(nonce string)) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA 密钥失败: %v", err)
	}
	const kid = "api-test-kid"
	var nextNonce string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			iss := srv.URL
			json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                iss,
				"authorization_endpoint":                iss + "/authorize",
				"token_endpoint":                        iss + "/token",
				"jwks_uri":                              iss + "/jwks",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			jwk := jose.JSONWebKey{Key: &priv.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"}
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
		case "/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-xxx",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     signIDToken(t, priv, kid, srv.URL, nextNonce),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func(n string) { nextNonce = n }
}

// signIDToken 签发带指定 nonce 的 id_token（oidc-client / sub-42）
func signIDToken(t *testing.T, priv *rsa.PrivateKey, kid, iss, nonce string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   iss,
		"aud":   "oidc-client",
		"sub":   "sub-42",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": nonce,
		"name":  "OIDC User",
		"email": "oidc@example.com",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	return signed
}

// 完整流程：providers → login(302) → callback(302 ticket) → exchange(JWT) → me
func TestOIDCFullHTTPFlow(t *testing.T) {
	issuer, setNonce := mockOIDCIssuer(t)
	m, err := auth.NewManager(&config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "company", Type: config.ProviderTypeOIDC, ClientID: "oidc-client", ClientSecret: "sec", Issuer: issuer},
		},
	})
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	env := newTestEnvWithAuth(t, m)

	// providers 含 company
	w := doReq(t, env.router, "GET", "/api/v1/auth/providers", nil, "")
	var pv struct {
		Data []auth.ProviderView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pv); err != nil {
		t.Fatalf("解析 providers 失败: %v", err)
	}
	if len(pv.Data) != 1 || pv.Data[0].Name != "company" || pv.Data[0].Type != "oidc" {
		t.Fatalf("providers 错误: %+v", pv.Data)
	}

	// login → 302 issuer 授权页（含 state 与 nonce）
	w = doReq(t, env.router, "GET", "/api/v1/auth/oidc/company/login", nil, "")
	if w.Code != 302 {
		t.Fatalf("login 应 302: %d", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	q := loc.Query()
	state := q.Get("state")
	nonce := q.Get("nonce")
	if state == "" || nonce == "" {
		t.Fatalf("授权 URL 应含 state 与 nonce: %s", loc)
	}

	// callback：token 端点签发的 id_token 必须携带登录时生成的 nonce
	setNonce(nonce)
	w = doReq(t, env.router, "GET", "/api/v1/auth/oidc/company/callback?code=code-1&state="+url.QueryEscape(state), nil, "")
	if w.Code != 302 {
		t.Fatalf("callback 应 302: %d %s", w.Code, w.Body.String())
	}
	cbLoc, _ := url.Parse(w.Header().Get("Location"))
	ticket := cbLoc.Query().Get("ticket")
	if ticket == "" || !strings.HasPrefix(cbLoc.Path, "/login") {
		t.Fatalf("callback 应跳 /login?ticket=: %s", w.Header().Get("Location"))
	}
	// JWT 不得出现在 URL
	if cbLoc.Query().Get("token") != "" {
		t.Fatal("URL 中不应出现 token")
	}

	// exchange → JWT
	w = doReq(t, env.router, "POST", "/api/v1/auth/exchange", map[string]string{"ticket": ticket}, "")
	if w.Code != 200 {
		t.Fatalf("exchange 应 200: %d %s", w.Code, w.Body.String())
	}
	var ex struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ex); err != nil {
		t.Fatalf("解析 exchange 失败: %v", err)
	}

	// JWT 访问 /auth/me → 用户已自动注册
	w = doReq(t, env.router, "GET", "/api/v1/auth/me", nil, ex.Data.Token)
	if w.Code != 200 {
		t.Fatalf("me 应 200: %d %s", w.Code, w.Body.String())
	}
	var me struct {
		Data struct {
			Kind     string `json:"kind"`
			Provider string `json:"provider"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("解析 me 失败: %v", err)
	}
	if me.Data.Kind != "oidc" || me.Data.Provider != "company" || me.Data.Name != "OIDC User" {
		t.Errorf("me 身份错误: %+v", me.Data)
	}
}

// AC12：nonce 被篡改 → callback 失败且不创建会话
func TestOIDCCallbackNonceMismatch(t *testing.T) {
	issuer, setNonce := mockOIDCIssuer(t)
	m, err := auth.NewManager(&config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "company", Type: config.ProviderTypeOIDC, ClientID: "oidc-client", ClientSecret: "sec", Issuer: issuer},
		},
	})
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	env := newTestEnvWithAuth(t, m)

	w := doReq(t, env.router, "GET", "/api/v1/auth/oidc/company/login", nil, "")
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")

	// 篡改：id_token 携带错误的 nonce
	setNonce("attacker-nonce")
	w = doReq(t, env.router, "GET", "/api/v1/auth/oidc/company/callback?code=code-1&state="+url.QueryEscape(state), nil, "")
	if w.Code != 302 {
		t.Fatalf("callback 应 302: %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/login?error=") {
		t.Fatalf("nonce 不匹配应跳 error: %s", loc)
	}
	// 未创建会话/用户
	if len(env.store.users) != 0 {
		t.Fatalf("nonce 篡改不应创建用户: %+v", env.store.users)
	}
}
