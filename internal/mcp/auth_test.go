package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// fakeKeyLookup 最小认证 fake（仅实现 keyLookup 所需方法）
type fakeKeyLookup struct {
	keys map[string]*store.APIKey // hash → key
}

func (f *fakeKeyLookup) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	return f.keys[hash], nil
}

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newFakeKeyLookup() *fakeKeyLookup {
	return &fakeKeyLookup{keys: map[string]*store.APIKey{}}
}

func TestAuthenticate(t *testing.T) {
	valid := "binrag_valid"
	disabled := "binrag_disabled"

	st := newFakeKeyLookup()
	st.keys[hashOf(valid)] = &store.APIKey{
		ID: "key-1", KeyHash: hashOf(valid), Enabled: true,
		MCPTools: []string{"ask"}, MCPKBScope: "all",
	}
	st.keys[hashOf(disabled)] = &store.APIKey{
		ID: "key-2", KeyHash: hashOf(disabled), Enabled: false,
	}

	cases := []struct {
		name      string
		authz     string
		wantOK    bool
		want401   bool
		wantKeyID string
	}{
		{"缺失 Authorization", "", false, true, ""},
		{"非 Bearer 前缀", "Token xyz", false, true, ""},
		{"无效 Key", "Bearer binrag_nope", false, true, ""},
		{"停用 Key", "Bearer " + disabled, false, true, ""},
		{"有效 Key", "Bearer " + valid, true, false, "key-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			rec := httptest.NewRecorder()

			kc, ok := authenticate(rec, req, st)
			if ok != tc.wantOK {
				t.Fatalf("authenticate ok = %v，期望 %v", ok, tc.wantOK)
			}
			if tc.want401 {
				if rec.Code != 401 {
					t.Errorf("认证失败应返回 HTTP 401，实际 %d", rec.Code)
				}
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Error("401 应带 WWW-Authenticate 头")
				}
			}
			if tc.wantKeyID != "" {
				if kc == nil || kc.KeyID != tc.wantKeyID {
					t.Errorf("keyCtx.KeyID = %v，期望 %q", kc, tc.wantKeyID)
				}
				// 权限解析：MCPTools 不进入 keyCtx（网关层从 store 读）；Scope 由字段解析
				if kc.Scope.All != true {
					t.Errorf("scope=all 应解析为 All=true，实际 %+v", kc.Scope)
				}
			}
		})
	}
}

// 有效 Key 但 scope 为空（历史 Key）：Scope 为空，任何 KB 不可访问
func TestAuthenticateNoScope(t *testing.T) {
	st := newFakeKeyLookup()
	st.keys[hashOf("binrag_hist")] = &store.APIKey{
		ID: "key-hist", KeyHash: hashOf("binrag_hist"), Enabled: true,
		// 无 MCP 权限字段（历史 Key 迁移后默认）
	}

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer binrag_hist")
	rec := httptest.NewRecorder()

	kc, ok := authenticate(rec, req, st)
	if !ok || kc == nil {
		t.Fatalf("有效 Key 应认证通过")
	}
	if kc.Scope.All || len(kc.Scope.IDs) != 0 {
		t.Errorf("历史 Key 的 Scope 应为空（无知识库权限），实际 %+v", kc.Scope)
	}
	if len(kc.Tools) != 0 {
		t.Errorf("历史 Key 的 Tool 白名单应为空（无 MCP Tool 权限），实际 %v", kc.Tools)
	}
}
