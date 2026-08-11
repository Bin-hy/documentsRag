package api

import (
	"encoding/json"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// 用户自助 MCP 凭据：生成/409/启停/吊销/权限（越权 400）/非会话 403（spec F7/F8/F9）
func TestMyMCPLifecycle(t *testing.T) {
	env, _ := newConfigTestEnv(t)
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")
	// 用户知识库（owner=user-a）
	ownerA, ownerB := "user-a", "user-b"
	env.store.mu.Lock()
	env.store.kbs["kb-user-1"] = store.KnowledgeBase{ID: "kb-user-1", Name: "我的库1", OwnerID: &ownerA}
	env.store.kbs["kb-other"] = store.KnowledgeBase{ID: "kb-other", Name: "他人库", OwnerID: &ownerB}
	env.store.mu.Unlock()

	// 1) 初始 status：key=null
	w := doReq(t, env.router, "GET", "/api/v1/mcp/my/status", nil, jwtUser)
	if w.Code != 200 {
		t.Fatalf("status 失败: %d %s", w.Code, w.Body.String())
	}
	var status struct {
		Data struct {
			GlobalEnabled bool      `json:"global_enabled"`
			Key           *MyMCPKey `json:"key"`
			MCPPath       string    `json:"mcp_path"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &status)
	if status.Data.Key != nil {
		t.Errorf("初始应无凭据: %+v", status.Data.Key)
	}

	// 2) 生成凭据 → 明文
	w = doReq(t, env.router, "POST", "/api/v1/mcp/my/key", nil, jwtUser)
	if w.Code != 200 {
		t.Fatalf("生成失败: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.Data.ID == "" || created.Data.Key == "" {
		t.Errorf("应返回 id 与明文 Key: %+v", created.Data)
	}

	// 3) 再次生成 → 409
	w = doReq(t, env.router, "POST", "/api/v1/mcp/my/key", nil, jwtUser)
	if w.Code != 409 {
		t.Errorf("重复生成应 409，实际 %d", w.Code)
	}

	// 4) status 现在有凭据
	w = doReq(t, env.router, "GET", "/api/v1/mcp/my/status", nil, jwtUser)
	json.Unmarshal(w.Body.Bytes(), &status)
	if status.Data.Key == nil || status.Data.Key.ID != created.Data.ID {
		t.Errorf("status 应返回我的凭据: %+v", status.Data.Key)
	}

	// 5) 配置权限（allowlist 自己的库）
	w = doReq(t, env.router, "PUT", "/api/v1/mcp/my/key/permissions", map[string]any{
		"mcp_tools":    []string{"retrieve", "ask"},
		"mcp_kb_scope": "allowlist",
		"mcp_kb_ids":   []string{"kb-user-1"},
	}, jwtUser)
	if w.Code != 200 {
		t.Fatalf("配置权限失败: %d %s", w.Code, w.Body.String())
	}
	// 越权 kb_id（他人库）→ 400
	w = doReq(t, env.router, "PUT", "/api/v1/mcp/my/key/permissions", map[string]any{
		"mcp_kb_scope": "allowlist",
		"mcp_kb_ids":   []string{"kb-other"},
	}, jwtUser)
	if w.Code != 400 {
		t.Errorf("越权 kb_id 应 400，实际 %d", w.Code)
	}

	// 6) 停用 → status enabled=false
	w = doReq(t, env.router, "POST", "/api/v1/mcp/my/key/toggle", map[string]any{"enabled": false}, jwtUser)
	if w.Code != 200 {
		t.Fatalf("停用失败: %d", w.Code)
	}
	w = doReq(t, env.router, "GET", "/api/v1/mcp/my/status", nil, jwtUser)
	json.Unmarshal(w.Body.Bytes(), &status)
	if status.Data.Key == nil || status.Data.Key.Enabled {
		t.Errorf("停用后 enabled 应为 false: %+v", status.Data.Key)
	}

	// 7) 吊销 → status key=null；再次生成成功
	w = doReq(t, env.router, "DELETE", "/api/v1/mcp/my/key", nil, jwtUser)
	if w.Code != 200 {
		t.Fatalf("吊销失败: %d", w.Code)
	}
	w = doReq(t, env.router, "GET", "/api/v1/mcp/my/status", nil, jwtUser)
	json.Unmarshal(w.Body.Bytes(), &status)
	if status.Data.Key != nil {
		t.Errorf("吊销后应无凭据: %+v", status.Data.Key)
	}

	// 8) 非会话（API Key）→ 403
	w = doReq(t, env.router, "GET", "/api/v1/mcp/my/status", nil, testAPIKey)
	if w.Code != 403 {
		t.Errorf("API Key 访问用户接口应 403，实际 %d", w.Code)
	}
}
