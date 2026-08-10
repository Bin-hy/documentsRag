package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"golang.org/x/oauth2"
)

// githubAPITimeout GitHub API 调用超时
const githubAPITimeout = 10 * time.Second

// githubProvider 内置 GitHub OAuth2 适配。
// GitHub 无用户 OIDC 端点（实测 github.com discovery 404；token.actions.githubusercontent.com 仅 Actions 用），
// 故走 OAuth2 授权码 + GitHub API /user：无 nonce / ID Token，**必须校验 state**（由统一回调流程保证）。
type githubProvider struct {
	name        string
	displayName string
	oauthCfg    oauth2.Config
	httpClient  *http.Client // 专用 client（10s 超时），测试注入
	apiBaseURL  string       // 生产 https://api.github.com；测试注入 httptest
}

// NewGithubProvider 构建 GitHub OAuth2 适配。
// httpClient / apiBaseURL / tokenURL 供测试注入隔离真实 GitHub；传 nil/空 用生产默认。
func NewGithubProvider(cfg config.ProviderConfig, redirectURL string, httpClient *http.Client, apiBaseURL, tokenURL string) (*githubProvider, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: githubAPITimeout}
	}
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}
	if tokenURL == "" {
		tokenURL = "https://github.com/login/oauth/access_token"
	}
	scope := cfg.Scope
	if len(scope) == 0 {
		scope = []string{"read:user"} // 最小权限，不请求 email
	}
	return &githubProvider{
		name:        cfg.Name,
		displayName: cfg.DisplayName,
		oauthCfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: tokenURL,
			},
			RedirectURL: redirectURL,
			Scopes:      scope,
		},
		httpClient: httpClient,
		apiBaseURL: apiBaseURL,
	}, nil
}

func (p *githubProvider) Name() string        { return p.name }
func (p *githubProvider) DisplayName() string { return p.displayName }
func (p *githubProvider) Type() string        { return config.ProviderTypeOAuth2 }

// AuthCodeURL 仅携带 state（GitHub 无 nonce 语义）
func (p *githubProvider) AuthCodeURL(state, _ string) string {
	return p.oauthCfg.AuthCodeURL(state)
}

// ExchangeAndVerify OAuth2 授权码交换 + GitHub API /user 取稳定数字 ID 作为 subject。
// access token 仅本次使用，不写日志、不落库。
func (p *githubProvider) ExchangeAndVerify(ctx context.Context, code, _ string) (*UserInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, githubAPITimeout)
	defer cancel()

	tok, err := p.oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("GitHub 授权码交换失败: %w", err)
	}

	// 用专用 client 携带 access token 调 /user
	reqCtx := context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	client := p.oauthCfg.Client(reqCtx, tok)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.apiBaseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("构造 GitHub /user 请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 GitHub /user 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GitHub /user 返回非 2xx: %d", resp.StatusCode)
	}

	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("解析 GitHub /user 失败: %w", err)
	}
	if u.ID == 0 {
		return nil, fmt.Errorf("GitHub /user 缺少稳定数字 ID")
	}
	// subject 用数字 ID 字符串（不用 email 作身份）
	return &UserInfo{Subject: strconv.FormatInt(u.ID, 10), Name: u.Login, Email: u.Email}, nil
}
