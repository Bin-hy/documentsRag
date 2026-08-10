package auth

import (
	"context"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

// 无 OIDC 配置 → Manager 可用、providers 为空
func TestNewManagerDisabled(t *testing.T) {
	m, err := NewManager(nil)
	if err != nil {
		t.Fatalf("NewManager(nil) 失败: %v", err)
	}
	if len(m.Providers()) != 0 {
		t.Errorf("未启用时 providers 应为空: %+v", m.Providers())
	}
}

// Enabled 缺 public_url → 装配失败
func TestNewManagerMissingPublicURL(t *testing.T) {
	cfg := &config.OIDCConfig{Enabled: true, Providers: []config.ProviderConfig{
		{Name: "github", Type: config.ProviderTypeOAuth2, ClientID: "c", ClientSecret: "s"},
	}}
	if _, err := NewManager(cfg); err == nil {
		t.Fatal("缺 public_url 应装配失败")
	}
}

// 重复 Name → 装配失败（不后覆盖前）
func TestNewManagerDuplicateName(t *testing.T) {
	srv, _, _ := newOIDCTestServer(t)
	cfg := &config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "company", Type: config.ProviderTypeOIDC, ClientID: "c", ClientSecret: "s", Issuer: srv.issuer},
			{Name: "company", Type: config.ProviderTypeOIDC, ClientID: "c2", ClientSecret: "s2", Issuer: srv.issuer},
		},
	}
	if _, err := NewManager(cfg); err == nil {
		t.Fatal("重复 Name 应装配失败")
	}
}

// oauth2 非 github → 装配失败
func TestNewManagerOAuth2NonGithub(t *testing.T) {
	cfg := &config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "gitlab", Type: config.ProviderTypeOAuth2, ClientID: "c", ClientSecret: "s"},
		},
	}
	if _, err := NewManager(cfg); err == nil {
		t.Fatal("oauth2 非 github 应装配失败")
	}
}

// GitHub provider 装配：可用、redirect URI 按 oauth2 路径
func TestNewManagerGithub(t *testing.T) {
	cfg := &config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "github", Type: config.ProviderTypeOAuth2, ClientID: "c", ClientSecret: "s"},
		},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager(github) 失败: %v", err)
	}
	views := m.Providers()
	if len(views) != 1 || views[0].Type != config.ProviderTypeOAuth2 || views[0].Name != "github" {
		t.Fatalf("providers 视图错误: %+v", views)
	}
	u, state, _, err := m.BeginLogin("github")
	if err != nil {
		t.Fatalf("BeginLogin 失败: %v", err)
	}
	if u == "" || state == "" {
		t.Fatal("授权 URL 与 state 不应为空")
	}
}

// OIDC 全流程：BeginLogin → CompleteLogin → IssueTicket → ExchangeTicket → VerifyJWT
func TestManagerOIDCFlow(t *testing.T) {
	srv, priv, kid := newOIDCTestServer(t)
	var issuedNonce string // BeginLogin 生成的 nonce，signIDToken 闭包引用（token 请求发生在 CompleteLogin 时，晚于赋值）
	srv.signIDToken = func() (string, error) {
		tok := signRS256(t, priv, kid, func(c jwt.MapClaims) { c["iss"] = srv.issuer; c["nonce"] = issuedNonce })
		return tok, nil
	}
	cfg := &config.OIDCConfig{
		Enabled: true, PublicURL: "https://rag.example.com",
		Providers: []config.ProviderConfig{
			{Name: "company", Type: config.ProviderTypeOIDC, ClientID: testClientID, ClientSecret: testClientSecret, Issuer: srv.issuer},
		},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}

	// login：取授权 URL + 提取 state/nonce（模拟浏览器回调回传）
	_, state, nonce, err := m.BeginLogin("company")
	if err != nil {
		t.Fatalf("BeginLogin 失败: %v", err)
	}
	if state == "" {
		t.Fatal("state 不应为空")
	}
	issuedNonce = nonce

	// callback：消费 state → 校验 id_token
	info, _, err := m.CompleteLogin(context.Background(), "code-1", state)
	if err != nil {
		t.Fatalf("CompleteLogin 失败: %v", err)
	}
	if info.Subject != testSubject {
		t.Errorf("subject 错误: %q", info.Subject)
	}

	// 二次回调同一 state 必须失败（一次性）
	if _, _, err := m.CompleteLogin(context.Background(), "code-1", state); err == nil {
		t.Fatal("state 重放应失败")
	}

	// ticket 换 JWT
	ticket, err := m.IssueTicket("user-1", "company")
	if err != nil {
		t.Fatalf("IssueTicket 失败: %v", err)
	}
	jwtStr, err := m.ExchangeTicket(ticket)
	if err != nil {
		t.Fatalf("ExchangeTicket 失败: %v", err)
	}
	claims, err := m.VerifyJWT(jwtStr)
	if err != nil {
		t.Fatalf("VerifyJWT 失败: %v", err)
	}
	if claims.UserID != "user-1" || claims.Provider != "company" {
		t.Errorf("JWT claims 错误: %+v", claims)
	}
	// ticket 重放失败
	if _, err := m.ExchangeTicket(ticket); err == nil {
		t.Fatal("ticket 重放应失败")
	}
	// 未知 provider / 无效 state
	if _, _, _, err := m.BeginLogin("nope"); err == nil {
		t.Fatal("未知 provider 应失败")
	}
	if _, _, err := m.CompleteLogin(context.Background(), "code-x", "fake-state"); err == nil {
		t.Fatal("无效 state 应失败")
	}
}
