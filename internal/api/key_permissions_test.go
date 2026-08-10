package api

import (
	"encoding/json"
	"testing"
)

// 系统级 Key 可查询/更新 MCP 权限；历史 Key 默认无 MCP 权限；会话 JWT 越权 403
func TestAPIKeyPermissions(t *testing.T) {
	env := newTestEnv(t)

	// 历史 Key（无 MCP 权限字段）列表返回空权限
	w := doReq(t, env.router, "GET", "/api/v1/api-keys", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("列表 Key 失败: %d", w.Code)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	keyID := ""
	for _, k := range resp.Data {
		if id, _ := k["id"].(string); id != "" {
			keyID = id
			if tools, ok := k["mcp_tools"].([]any); !ok || len(tools) != 0 {
				t.Errorf("历史 Key 的 mcp_tools 应为空: %v", k["mcp_tools"])
			}
			if scope, _ := k["mcp_kb_scope"].(string); scope != "" {
				t.Errorf("历史 Key 的 mcp_kb_scope 应为空: %q", scope)
			}
			break
		}
	}
	if keyID == "" {
		t.Fatal("测试环境应有 API Key")
	}

	// 系统级 Key 更新权限（allowlist）
	w = doReq(t, env.router, "PUT", "/api/v1/api-keys/"+keyID+"/permissions", map[string]any{
		"mcp_tools":     []string{"retrieve", "ask"},
		"mcp_kb_scope":  "allowlist",
		"mcp_kb_ids":    []string{"kb-a", "kb-b"},
	}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("更新权限失败: %d %s", w.Code, w.Body.String())
	}

	// 更新后查询可见
	w = doReq(t, env.router, "GET", "/api/v1/api-keys", nil, testAPIKey)
	var resp2 struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp2)
	for _, k := range resp2.Data {
		if k["id"] == keyID {
			tools := k["mcp_tools"].([]any)
			if len(tools) != 2 {
				t.Errorf("更新后 mcp_tools 应为 2 个，实际 %v", tools)
			}
			if scope, _ := k["mcp_kb_scope"].(string); scope != "allowlist" {
				t.Errorf("更新后 mcp_kb_scope 应为 allowlist，实际 %q", scope)
			}
		}
	}

	// 非法 scope → 400
	w = doReq(t, env.router, "PUT", "/api/v1/api-keys/"+keyID+"/permissions", map[string]any{
		"mcp_kb_scope": "weird",
	}, testAPIKey)
	if w.Code != 400 {
		t.Errorf("非法 scope 应 400，实际 %d", w.Code)
	}

	// 会话 JWT 更新 → 403
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")
	w = doReq(t, env.router, "PUT", "/api/v1/api-keys/"+keyID+"/permissions", map[string]any{
		"mcp_kb_scope": "all",
	}, jwtUser)
	if w.Code != 403 {
		t.Errorf("会话 JWT 更新权限应 403，实际 %d", w.Code)
	}
}
