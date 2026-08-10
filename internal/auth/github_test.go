package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// githubTestEnv mock GitHub 的 token 端点与 API /user 端点
type githubTestEnv struct {
	server   *httptest.Server
	userJSON string
	userCode int
}

func newGithubTestEnv(t *testing.T, userJSON string, userCode int) (*githubTestEnv, *githubProvider) {
	t.Helper()
	env := &githubTestEnv{userJSON: userJSON, userCode: userCode}
	env.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "gho_test_token",
				"token_type":   "bearer",
				"scope":        "read:user",
			})
		case "/user":
			w.WriteHeader(env.userCode)
			if env.userCode == http.StatusOK {
				w.Write([]byte(env.userJSON))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(env.server.Close)

	p, err := NewGithubProvider(config.ProviderConfig{
		Name: "github", DisplayName: "GitHub", ClientID: "cid", ClientSecret: "csec",
	}, "http://localhost/cb", env.server.Client(), env.server.URL, env.server.URL+"/token")
	if err != nil {
		t.Fatalf("NewGithubProvider 失败: %v", err)
	}
	return env, p
}

// 2xx + 数字 ID → 成功，subject=数字 ID、name=login
func TestGithubExchangeOK(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"id":12345,"login":"octocat","name":"Octo Cat","email":null}`, http.StatusOK)
	info, err := p.ExchangeAndVerify(context.Background(), "code-1", "")
	if err != nil {
		t.Fatalf("ExchangeAndVerify 失败: %v", err)
	}
	if info.Subject != "12345" {
		t.Errorf("subject 应为数字 ID 字符串: %q", info.Subject)
	}
	if info.Name != "octocat" {
		t.Errorf("name 应为 login: %q", info.Name)
	}
}

// AuthCodeURL 仅携带 state（无 nonce）
func TestGithubAuthCodeURL(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"id":1,"login":"a"}`, http.StatusOK)
	u := p.AuthCodeURL("state-xyz", "should-be-ignored")
	if !containsParam(u, "state", "state-xyz") {
		t.Errorf("AuthCodeURL 应携带 state: %s", u)
	}
	if containsParam(u, "nonce", "should-be-ignored") {
		t.Errorf("GitHub AuthCodeURL 不应携带 nonce: %s", u)
	}
}

// GitHub API 非 2xx → 登录失败
func TestGithubAPIError(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	if _, err := p.ExchangeAndVerify(context.Background(), "code-x", ""); err == nil {
		t.Fatal("GitHub /user 401 应登录失败")
	}
}

// 缺少数字 ID → 登录失败
func TestGithubMissingID(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"login":"ghost"}`, http.StatusOK)
	if _, err := p.ExchangeAndVerify(context.Background(), "code-x", ""); err == nil {
		t.Fatal("缺少数字 ID 应登录失败")
	}
}

// id 非数字（JSON 类型不符）→ 解码失败 → 登录失败
func TestGithubNonNumericID(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"id":"abc","login":"ghost"}`, http.StatusOK)
	if _, err := p.ExchangeAndVerify(context.Background(), "code-x", ""); err == nil {
		t.Fatal("非数字 ID 应登录失败")
	}
}

// Type 语义：GitHub 是 oauth2 而非 oidc
func TestGithubTypeIsOAuth2(t *testing.T) {
	_, p := newGithubTestEnv(t, `{"id":1,"login":"a"}`, http.StatusOK)
	if p.Type() != config.ProviderTypeOAuth2 {
		t.Fatalf("GitHub provider 类型应为 oauth2: %q", p.Type())
	}
}
