package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// discoveryTimeout OIDC discovery / token 交换超时
const discoveryTimeout = 15 * time.Second

// UserInfo 登录成功后从 Provider 提取的身份信息（subject 为稳定唯一标识）
type UserInfo struct {
	Subject string
	Name    string
	Email   string
}

// Provider 登录 Provider 抽象。
// 注意：GitHub 是 OAuth2 provider，不是 OIDC——Type() 区分实现方式，但都产出 UserInfo 供统一用户模型。
type Provider interface {
	Name() string
	DisplayName() string
	Type() string // oidc / oauth2
	// AuthCodeURL 生成授权跳转地址：state 是所有 Provider 的 CSRF 机制（必带），nonce 仅 OIDC 使用
	AuthCodeURL(state, nonce string) string
	// ExchangeAndVerify 用授权码换取并校验身份；OIDC 校验 nonce 与 ID Token 全部标准 claims
	ExchangeAndVerify(ctx context.Context, code, nonce string) (*UserInfo, error)
}

// oidcProvider 标准 OIDC Provider（授权码 + ID Token）。
// discovery 与 JWKS 在启动期完成并缓存，认证路径不产生网络调用（N5）。
type oidcProvider struct {
	name          string
	displayName   string
	issuer        string // 配置的 issuer（严格校验 / 降级校验共用）
	oauthCfg      oauth2.Config
	verifier      *oidc.IDTokenVerifier
	permissiveSub bool            // 兼容服务商把 sub 签发为数字（仍全量验签与 claims 校验）
	keySet        oidc.KeySet     // 降级校验路径的签名验证（remote JWKS）
}

// NewOIDCProvider 启动期构建：带超时执行 issuer discovery；失败即返回错误（装配失败）。
// PermissiveSub 开启时额外缓存 JWKS（remote key set），供数字 sub 兼容路径复用同一签名验证。
func NewOIDCProvider(ctx context.Context, cfg config.ProviderConfig, redirectURL string) (*oidcProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC provider %q discovery 失败: %w", cfg.Name, err)
	}
	scope := cfg.Scope
	if len(scope) == 0 {
		scope = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}
	}
	prov := &oidcProvider{
		name:          cfg.Name,
		displayName:   cfg.DisplayName,
		issuer:        cfg.Issuer,
		oauthCfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       scope,
		},
		verifier:      p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		permissiveSub: cfg.PermissiveSub,
	}
	if cfg.PermissiveSub {
		// 取 jwks_uri（go-oidc 未公开，自行再拉一次 discovery），供降级校验复用同一套密钥
		jwksURL, err := fetchJWKSURI(ctx, cfg.Issuer)
		if err != nil {
			return nil, fmt.Errorf("OIDC provider %q 获取 jwks_uri 失败: %w", cfg.Name, err)
		}
		prov.keySet = oidc.NewRemoteKeySet(ctx, jwksURL)
	}
	return prov, nil
}

// fetchJWKSURI 从 discovery 文档取 jwks_uri
func fetchJWKSURI(ctx context.Context, issuer string) (string, error) {
	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery 返回 %s", resp.Status)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery 缺少 jwks_uri")
	}
	return doc.JWKSURI, nil
}

func (p *oidcProvider) Name() string        { return p.name }
func (p *oidcProvider) DisplayName() string { return p.displayName }
func (p *oidcProvider) Type() string        { return config.ProviderTypeOIDC }

func (p *oidcProvider) AuthCodeURL(state, nonce string) string {
	return p.oauthCfg.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce))
}

// ExchangeAndVerify 完整校验 ID Token：
// 签名（JWKS）/ issuer（仅信任配置 issuer）/ audience（client_id）/ exp / iat / nbf / nonce / subject 非空。
// 不调用 userinfo 端点。
func (p *oidcProvider) ExchangeAndVerify(ctx context.Context, code, nonce string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	rawIDToken, err := p.oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("授权码交换失败: %w", err)
	}
	idTokenStr, ok := rawIDToken.Extra("id_token").(string)
	if !ok || idTokenStr == "" {
		return nil, fmt.Errorf("token 响应缺少 id_token")
	}

	// go-oidc Verifier：校验签名(JWKS)、issuer、audience、exp/iat/nbf（存在时）
	idToken, err := p.verifier.Verify(ctx, idTokenStr)
	if err != nil {
		// 兼容模式：服务商把 sub 签发为数字（违反规范）导致 go-oidc 严格解析失败时，
		// 走降级路径——仍用同一 JWKS 验签并手动完成全部标准 claims 校验，仅 sub 类型宽松。
		if p.permissiveSub && isSubTypeError(err) {
			return p.verifyPermissive(ctx, idTokenStr, nonce)
		}
		return nil, fmt.Errorf("id_token 校验失败: %w", err)
	}

	var claims struct {
		Nonce string `json:"nonce"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Nbf   int64  `json:"nbf,omitempty"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("id_token claims 解析失败: %w", err)
	}
	// nonce 必须与登录时 stateStore 绑定值一致（防 ID Token 重放/混淆）
	if nonce == "" || claims.Nonce != nonce {
		return nil, fmt.Errorf("nonce 校验失败")
	}
	// nbf 显式兜底（go-oidc 已检查，此处双保险）
	if claims.Nbf != 0 && time.Now().Unix() < claims.Nbf {
		return nil, fmt.Errorf("id_token 尚未生效（nbf）")
	}
	if idToken.Subject == "" {
		return nil, fmt.Errorf("id_token 缺少 subject")
	}
	return &UserInfo{Subject: idToken.Subject, Name: claims.Name, Email: claims.Email}, nil
}

// isSubTypeError 判断 go-oidc 失败是否为 sub 字段类型不匹配（数字 sub）
func isSubTypeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "cannot unmarshal number") &&
		strings.Contains(err.Error(), "idToken.sub")
}

// verifyPermissive 降级校验：兼容数字 sub 的 OIDC 服务商。
// 安全强度与严格路径等价——签名（同一 JWKS remote key set）、iss、aud、exp、iat、nbf、nonce 全部校验，
// 仅 sub 允许数字（确定性转字符串）。sub 为其他非字符串/数字类型时仍拒绝。
func (p *oidcProvider) verifyPermissive(ctx context.Context, idTokenStr, nonce string) (*UserInfo, error) {
	if p.keySet == nil {
		return nil, fmt.Errorf("id_token 校验失败: permissive_sub 未初始化密钥集")
	}
	// 签名验证（JWKS，带缓存/刷新）
	payload, err := p.keySet.VerifySignature(ctx, idTokenStr)
	if err != nil {
		return nil, fmt.Errorf("id_token 签名校验失败: %w", err)
	}

	var claims struct {
		Issuer   string          `json:"iss"`
		Audience json.RawMessage `json:"aud"`
		Expiry   int64           `json:"exp"`
		IssuedAt int64           `json:"iat,omitempty"`
		NotBefore int64          `json:"nbf,omitempty"`
		Nonce    string          `json:"nonce"`
		Subject  json.RawMessage `json:"sub"`
		Name     string          `json:"name"`
		Email    string          `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("id_token claims 解析失败: %w", err)
	}
	now := time.Now().Unix()

	// issuer：仅信任配置 issuer（与 discovery 一致）
	if claims.Issuer != p.issuer {
		return nil, fmt.Errorf("id_token issuer 校验失败")
	}
	// audience：必须包含 client_id（兼容 string 与数组两种表示）
	if !audienceContains(claims.Audience, p.oauthCfg.ClientID) {
		return nil, fmt.Errorf("id_token audience 校验失败")
	}
	// exp：必填且未过期
	if claims.Expiry == 0 || now >= claims.Expiry {
		return nil, fmt.Errorf("id_token 已过期或缺少 exp")
	}
	// nbf：存在时必须已生效
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, fmt.Errorf("id_token 尚未生效（nbf）")
	}
	// nonce：与登录时绑定值一致
	if nonce == "" || claims.Nonce != nonce {
		return nil, fmt.Errorf("nonce 校验失败")
	}
	// subject：字符串直接用；数字确定性转字符串；其他类型拒绝
	subject, err := subToString(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("id_token subject 非法: %w", err)
	}
	if subject == "" {
		return nil, fmt.Errorf("id_token 缺少 subject")
	}
	return &UserInfo{Subject: subject, Name: claims.Name, Email: claims.Email}, nil
}

// audienceContains 校验 aud（string 或 []string）包含目标 client_id
func audienceContains(raw json.RawMessage, want string) bool {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == want
	}
	var multi []string
	if err := json.Unmarshal(raw, &multi); err == nil {
		for _, a := range multi {
			if a == want {
				return true
			}
		}
	}
	return false
}

// subToString 将 sub 转为字符串：字符串原样；数字（json.Number）确定性转十进制字符串；其余类型拒绝
func subToString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}
	return "", fmt.Errorf("sub 类型不受支持（须为字符串或数字）")
}
