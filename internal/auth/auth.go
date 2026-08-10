package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// defaultJWTExpireMinutes 兜底 JWT 有效期（applyDefaults 未跑时）
const defaultJWTExpireMinutes = 120

// ProviderView 公开给前端的 Provider 信息（不含任何机密）
type ProviderView struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // oidc / oauth2
	DisplayName string `json:"display_name"`
}

// Manager 认证管理器：Provider 注册表、会话 JWT 签发/验签、一次性 state/ticket 票据。
// 本类型不依赖 store：用户持久化由 API 层调用 store 完成。
type Manager struct {
	providers map[string]Provider
	signer    *Signer
	states    *stateStore
	tickets   *ticketStore
	jwtTTL    time.Duration
}

// NewManager 构建认证管理器（调用方保证 cfg 已通过 config.Validate；此处仍做二次防线校验）。
//   - JWT 密钥：jwt_secret 或启动期自动生成 32 字节随机（进程内持有）
//   - provider 注册表：type=oidc → oidcProvider（启动期 discovery，失败即装配失败）；
//     type=oauth2 仅允许 name=github → githubProvider
//   - redirect URI：启动时按 type 计算并固定，不随请求参数变化
func NewManager(cfg *config.OIDCConfig) (*Manager, error) {
	if cfg == nil {
		cfg = &config.OIDCConfig{}
	}
	signer, err := NewSigner(cfg.JWTSecret)
	if err != nil {
		return nil, err
	}
	jwtTTL := time.Duration(cfg.JWTExpireMinutes) * time.Minute
	if jwtTTL <= 0 {
		jwtTTL = defaultJWTExpireMinutes * time.Minute
	}
	m := &Manager{
		providers: make(map[string]Provider, len(cfg.Providers)),
		signer:    signer,
		states:    newStateStore(),
		tickets:   newTicketStore(),
		jwtTTL:    jwtTTL,
	}
	if !cfg.Enabled {
		return m, nil
	}
	if cfg.PublicURL == "" {
		return nil, fmt.Errorf("oidc.enabled=true 时 public_url 必填")
	}
	base := strings.TrimRight(cfg.PublicURL, "/")
	ctx := context.Background()
	for _, pc := range cfg.Providers {
		if _, dup := m.providers[pc.Name]; dup {
			return nil, fmt.Errorf("登录 Provider name 重复: %s", pc.Name)
		}
		redirect := pc.RedirectURL
		if redirect == "" {
			if pc.Type == config.ProviderTypeOIDC {
				redirect = fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", base, pc.Name)
			} else {
				redirect = fmt.Sprintf("%s/api/v1/auth/github/callback", base)
			}
		}
		var prov Provider
		switch pc.Type {
		case config.ProviderTypeOIDC:
			p, err := NewOIDCProvider(ctx, pc, redirect)
			if err != nil {
				return nil, err
			}
			prov = p
		case config.ProviderTypeOAuth2:
			if pc.Name != config.ProviderNameGithub {
				return nil, fmt.Errorf("type=oauth2 仅支持内置 github provider: %s", pc.Name)
			}
			p, err := NewGithubProvider(pc, redirect, nil, "", "")
			if err != nil {
				return nil, err
			}
			prov = p
		default:
			return nil, fmt.Errorf("未知 provider type: %s", pc.Type)
		}
		m.providers[pc.Name] = prov
	}
	return m, nil
}

// Providers 公开 Provider 列表（仅 name/type/display_name，无机密）
func (m *Manager) Providers() []ProviderView {
	views := make([]ProviderView, 0, len(m.providers))
	for _, p := range m.providers {
		views = append(views, ProviderView{Name: p.Name(), Type: p.Type(), DisplayName: p.DisplayName()})
	}
	return views
}

// Get 按 name 取 Provider
func (m *Manager) Get(name string) (Provider, bool) {
	p, ok := m.providers[name]
	return p, ok
}

// BeginLogin 生成授权跳转：state 存入 stateStore（OIDC 同时生成并绑定 nonce）
func (m *Manager) BeginLogin(providerName string) (authURL, state, nonce string, err error) {
	p, ok := m.providers[providerName]
	if !ok {
		return "", "", "", fmt.Errorf("未知 provider: %s", providerName)
	}
	if p.Type() == config.ProviderTypeOIDC {
		nonce, err = newToken()
		if err != nil {
			return "", "", "", err
		}
	}
	state, err = m.states.New(providerName, nonce, 0)
	if err != nil {
		return "", "", "", err
	}
	return p.AuthCodeURL(state, nonce), state, nonce, nil
}

// CompleteLogin 回调处理：原子消费 state（不存在/过期立即失败）→ Provider 校验。
// 返回 UserInfo 与 state 绑定的 provider 名（防 URL 参数伪造 provider）；用户持久化由调用方完成。
// 任一失败不创建会话。
func (m *Manager) CompleteLogin(ctx context.Context, code, state string) (*UserInfo, string, error) {
	providerName, nonce, ok := m.states.Consume(state)
	if !ok {
		return nil, "", fmt.Errorf("state 无效或已过期")
	}
	p, ok := m.providers[providerName]
	if !ok {
		return nil, "", fmt.Errorf("未知 provider: %s", providerName)
	}
	info, err := p.ExchangeAndVerify(ctx, code, nonce)
	if err != nil {
		return nil, "", err
	}
	return info, providerName, nil
}

// IssueTicket 登录成功后签发一次性 ticket（绑定已认证用户与 provider）
func (m *Manager) IssueTicket(userID, provider string) (string, error) {
	return m.tickets.New(userID, provider, 0)
}

// ExchangeTicket 用一次性 ticket 换取会话 JWT（消费后立即删除，重放失败）
func (m *Manager) ExchangeTicket(ticket string) (string, error) {
	userID, provider, ok := m.tickets.Consume(ticket)
	if !ok {
		return "", fmt.Errorf("ticket 无效或已过期")
	}
	return m.signer.Sign(userID, provider, m.jwtTTL)
}

// VerifyJWT 校验会话 JWT（认证中间件用）
func (m *Manager) VerifyJWT(token string) (*SessionClaims, error) {
	return m.signer.Verify(token)
}
