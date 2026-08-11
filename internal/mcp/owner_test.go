package mcp

import (
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// MCP 授权 owner 隔离（spec F9）：用户凭据 scope=all 只访问自己的知识库；
// allowlist 含他人 id 被过滤；系统级 Key（owner 空）行为不变。
func TestMCPOwnerIsolation(t *testing.T) {
	st := newFakeStore()
	now := time.Now()
	ownerA, ownerB := "user-a", "user-b"
	st.kbs["kb-mine"] = store.KnowledgeBase{ID: "kb-mine", Name: "我的库", OwnerID: &ownerA, CreatedAt: now, UpdatedAt: now}
	st.kbs["kb-other"] = store.KnowledgeBase{ID: "kb-other", Name: "他人库", OwnerID: &ownerB, CreatedAt: now, UpdatedAt: now}
	st.kbs["kb-sys"] = store.KnowledgeBase{ID: "kb-sys", Name: "系统库", CreatedAt: now, UpdatedAt: now} // 系统级（OwnerID nil）
	// 用户凭据（owner=user-a，scope=all）
	st.addKey(store.APIKey{ID: "key-user", KeyHash: "token_user", Enabled: true, OwnerID: "user-a",
		MCPTools: []string{ToolListKBs, ToolGetKB}, MCPKBScope: "all"})
	// 系统级 Key（owner 空，scope=all）
	st.addKey(store.APIKey{ID: "key-sys", KeyHash: "token_sys", Enabled: true,
		MCPTools: []string{ToolListKBs, ToolGetKB}, MCPKBScope: "all"})
	h, _ := newTestServer(t, st)

	// 用户凭据：list_knowledge_bases 只返回自己的 KB
	sid := doInitialize(t, h, "token_user")
	_, resp := mcpPost(t, h, "token_user", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})
	sc := resp["result"].(map[string]any)["structuredContent"].([]any)
	if len(sc) != 1 {
		t.Fatalf("用户 scope=all 应只返回 1 个自己的 KB，实际 %d: %v", len(sc), sc)
	}
	kb0 := sc[0].(map[string]any)
	if kb0["id"] != "kb-mine" {
		t.Errorf("应只返回自己的 kb-mine，实际 %v", kb0["id"])
	}

	// 用户凭据：get_knowledge_base 他人 KB → -32001（不泄露存在性）
	_, resp = mcpPost(t, h, "token_user", sid, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": ToolGetKB, "arguments": map[string]any{"kb_id": "kb-other"}}})
	if int(resp["error"].(map[string]any)["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("越权他人 KB 应 -32001，实际 %v", resp["error"])
	}
	// 系统级 KB 同样不可访问
	_, resp = mcpPost(t, h, "token_user", sid, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": ToolGetKB, "arguments": map[string]any{"kb_id": "kb-sys"}}})
	if int(resp["error"].(map[string]any)["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("系统级 KB 对用户凭据应 -32001，实际 %v", resp["error"])
	}

	// 系统级 Key：scope=all 可访问全部 3 个
	sid2 := doInitialize(t, h, "token_sys")
	_, resp = mcpPost(t, h, "token_sys", sid2, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})
	sc = resp["result"].(map[string]any)["structuredContent"].([]any)
	if len(sc) != 3 {
		t.Errorf("系统级 Key 应可访问全部 3 个 KB，实际 %d", len(sc))
	}
}

// 用户凭据 allowlist 含他人 kb_id：被 owner 过滤（交集），调用被过滤的 KB → -32001
func TestMCPOwnerAllowlistFiltered(t *testing.T) {
	st := newFakeStore()
	now := time.Now()
	ownerA := "user-a"
	st.kbs["kb-mine"] = store.KnowledgeBase{ID: "kb-mine", Name: "我的库", OwnerID: &ownerA, CreatedAt: now, UpdatedAt: now}
	st.kbs["kb-other"] = store.KnowledgeBase{ID: "kb-other", Name: "他人库", CreatedAt: now, UpdatedAt: now} // 系统级
	// 用户凭据配置 allowlist 含他人 id（kb-other）
	st.addKey(store.APIKey{ID: "key-user", KeyHash: "token_user", Enabled: true, OwnerID: "user-a",
		MCPTools: []string{ToolListKBs, ToolGetKB}, MCPKBScope: "allowlist", MCPKBIDs: []string{"kb-mine", "kb-other"}})
	h, _ := newTestServer(t, st)

	sid := doInitialize(t, h, "token_user")
	// list 只返回交集内的 kb-mine
	_, resp := mcpPost(t, h, "token_user", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})
	sc := resp["result"].(map[string]any)["structuredContent"].([]any)
	if len(sc) != 1 || sc[0].(map[string]any)["id"] != "kb-mine" {
		t.Errorf("allowlist 应被过滤为交集（仅 kb-mine），实际 %v", sc)
	}
	// 被过滤的 kb-other → -32001
	_, resp = mcpPost(t, h, "token_user", sid, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": ToolGetKB, "arguments": map[string]any{"kb_id": "kb-other"}}})
	if int(resp["error"].(map[string]any)["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("被过滤 KB 应 -32001，实际 %v", resp["error"])
	}
}
