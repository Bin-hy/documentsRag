package api

import (
	"encoding/json"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// AC6/AC7：用户 A 创建的知识库仅 A 与系统级 Key 可见；用户 B 一律 404；DTO 不暴露 owner_id
func TestKBOwnerIsolation(t *testing.T) {
	env := newTestEnv(t)
	jwtA := signTestJWT(t, env.authMgr, "user-a", "github")
	jwtB := signTestJWT(t, env.authMgr, "user-b", "github")

	// 用户 A 创建知识库
	w := doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "A 的库"}, jwtA)
	if w.Code != 200 {
		t.Fatalf("A 创建库失败: %d %s", w.Code, w.Body.String())
	}
	// DTO 不暴露 owner_id（AC：KB 对外视图剔除归属字段）
	if containsJSONKey(w.Body.Bytes(), "owner_id") {
		t.Fatalf("创建响应不应含 owner_id: %s", w.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"ID"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	kbID := created.Data.ID

	// A 列表可见
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, jwtA)
	if !respContainsID(t, w.Body.Bytes(), kbID) {
		t.Fatalf("A 应看到自己的库: %s", w.Body.String())
	}
	// B 列表不可见
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, jwtB)
	if respContainsID(t, w.Body.Bytes(), kbID) {
		t.Fatalf("B 不应看到 A 的库: %s", w.Body.String())
	}
	// B 详情/更新/删除一律 404
	if w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases/"+kbID, nil, jwtB); w.Code != 404 {
		t.Fatalf("B GET 应 404: %d", w.Code)
	}
	if w = doReq(t, env.router, "PUT", "/api/v1/knowledge-bases/"+kbID, map[string]string{"name": "改"}, jwtB); w.Code != 404 {
		t.Fatalf("B PUT 应 404: %d", w.Code)
	}
	if w = doReq(t, env.router, "DELETE", "/api/v1/knowledge-bases/"+kbID, nil, jwtB); w.Code != 404 {
		t.Fatalf("B DELETE 应 404: %d", w.Code)
	}
	// 系统级 Key 列表可见（ListAllKBs 含用户级）
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	if !respContainsID(t, w.Body.Bytes(), kbID) {
		t.Fatalf("系统级 Key 应看到全部库: %s", w.Body.String())
	}
}

// AC6：用户创建时 owner 绑定本人；再次创建另一个用户的库互不可见
func TestKBOwnerPersisted(t *testing.T) {
	env := newTestEnv(t)
	jwtA := signTestJWT(t, env.authMgr, "user-a", "github")
	jwtB := signTestJWT(t, env.authMgr, "user-b", "github")

	w := doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "A 库"}, jwtA)
	if w.Code != 200 {
		t.Fatalf("A 建库失败: %d", w.Code)
	}
	// fakeStore 中 owner 已绑定 A
	for _, kb := range env.store.kbs {
		if kb.OwnerID == nil || *kb.OwnerID != "user-a" {
			t.Fatalf("知识库 owner 应绑定 user-a: %+v", kb.OwnerID)
		}
	}

	w = doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "B 库"}, jwtB)
	if w.Code != 200 {
		t.Fatalf("B 建库失败: %d", w.Code)
	}
	// B 列表只含 B 的库
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, jwtB)
	if n := respCount(t, w.Body.Bytes()); n != 1 {
		t.Fatalf("B 应只看到 1 个自己的库，实际 %d: %s", n, w.Body.String())
	}
}

// AC8：系统级 Key 创建的库为系统级（NULL owner）；OIDC 用户不可见、系统级 Key 可见
func TestKBSystemLevelInvisibleToUser(t *testing.T) {
	env := newTestEnv(t)
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")

	// 系统级 Key 创建库（OwnerID nil）
	w := doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "系统库"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("系统 Key 建库失败: %d", w.Code)
	}
	var created struct {
		Data struct {
			ID string `json:"ID"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)

	// OIDC 用户不可见且访问 404
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, jwtUser)
	if respContainsID(t, w.Body.Bytes(), created.Data.ID) {
		t.Fatalf("OIDC 用户不应看到系统级库: %s", w.Body.String())
	}
	if w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases/"+created.Data.ID, nil, jwtUser); w.Code != 404 {
		t.Fatalf("OIDC 用户访问系统级库应 404: %d", w.Code)
	}
	// 系统级 Key 可见
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	if !respContainsID(t, w.Body.Bytes(), created.Data.ID) {
		t.Fatalf("系统级 Key 应看到系统级库: %s", w.Body.String())
	}
}

// AC5：OIDC 用户调用 API Key 管理接口 → 403；系统级 Key 正常
func TestKeyManagementSystemOnly(t *testing.T) {
	env := newTestEnv(t)
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")

	if w := doReq(t, env.router, "POST", "/api/v1/api-keys", map[string]string{"name": "x"}, jwtUser); w.Code != 403 {
		t.Fatalf("OIDC 用户创建 Key 应 403: %d", w.Code)
	}
	if w := doReq(t, env.router, "GET", "/api/v1/api-keys", nil, jwtUser); w.Code != 403 {
		t.Fatalf("OIDC 用户列表 Key 应 403: %d", w.Code)
	}
	if w := doReq(t, env.router, "DELETE", "/api/v1/api-keys/key-1", nil, jwtUser); w.Code != 403 {
		t.Fatalf("OIDC 用户删除 Key 应 403: %d", w.Code)
	}
	if w := doReq(t, env.router, "POST", "/api/v1/api-keys/key-1/toggle", map[string]bool{"enabled": false}, jwtUser); w.Code != 403 {
		t.Fatalf("OIDC 用户启停 Key 应 403: %d", w.Code)
	}
	// 系统级 Key 仍可管理
	if w := doReq(t, env.router, "POST", "/api/v1/api-keys", map[string]string{"name": "新"}, testAPIKey); w.Code != 200 {
		t.Fatalf("系统级 Key 创建应 200: %d", w.Code)
	}
}

// ---------- helpers ----------

func containsJSONKey(body []byte, key string) bool {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	if data, ok := m["data"].(map[string]any); ok {
		if _, has := data[key]; has {
			return true
		}
	}
	return false
}

func respContainsID(t *testing.T, body []byte, id string) bool {
	t.Helper()
	var m struct {
		Data []struct {
			ID string `json:"ID"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("解析列表失败: %v body=%s", err, body)
	}
	for _, item := range m.Data {
		if item.ID == id {
			return true
		}
	}
	return false
}

func respCount(t *testing.T, body []byte) int {
	t.Helper()
	var m struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("解析失败: %v body=%s", err, body)
	}
	return len(m.Data)
}

var _ = store.KnowledgeBase{} // 保持 import（fakeStore 类型引用）

// 安全回归：登录用户不带 kb_id 的 chat 必须 400（防跨租户检索）
func TestChatUserRequiresKBID(t *testing.T) {
	env := newTestEnv(t)
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")

	if w := doReq(t, env.router, "POST", "/api/v1/chat",
		map[string]string{"session_id": "s1", "question": "问题"}, jwtUser); w.Code != 400 {
		t.Fatalf("登录用户不带 kb_id 应 400: %d %s", w.Code, w.Body.String())
	}
	// 带他人/不存在的 kb_id → 404
	if w := doReq(t, env.router, "POST", "/api/v1/chat",
		map[string]string{"session_id": "s1", "question": "问题", "kb_id": "kb-other"}, jwtUser); w.Code != 404 {
		t.Fatalf("登录用户带无权 kb_id 应 404: %d", w.Code)
	}
}

// 安全回归：chunk 无法关联到可访问文档时一律 404（防残留向量跨租户读取）
func TestChunkAccessControl(t *testing.T) {
	env := newTestEnv(t)
	env.vs.payloads = map[string]map[string]any{}
	jwtUser := signTestJWT(t, env.authMgr, "user-a", "github")

	// chunk 无 document_id → 404
	if w := doReq(t, env.router, "GET", "/api/v1/chunks/10000000-0000-0000-0000-000000000001", nil, jwtUser); w.Code != 404 {
		t.Fatalf("无 document_id 的 chunk 应 404: %d", w.Code)
	}
	// 文档不属于用户 → 404（fakeVS 返回 payload 但归属校验拦截）
	uid := "user-b"
	env.store.kbs["kb-b"] = store.KnowledgeBase{ID: "kb-b", Name: "B 库", OwnerID: &uid}
	env.store.docs["doc-b"] = store.Document{ID: "doc-b", KBID: "kb-b"}
	env.vs.payloads["10000000-0000-0000-0000-000000000002"] = map[string]any{"content": "机密内容", "document_id": "doc-b"}
	if w := doReq(t, env.router, "GET", "/api/v1/chunks/10000000-0000-0000-0000-000000000002", nil, jwtUser); w.Code != 404 {
		t.Fatalf("他人文档的 chunk 应 404: %d %s", w.Code, w.Body.String())
	}
	// 文档已删除（残留向量）→ 404
	env.store.docs["doc-gone"] = store.Document{ID: "doc-gone", KBID: "kb-b"}
	env.vs.payloads["10000000-0000-0000-0000-000000000003"] = map[string]any{"content": "残留", "document_id": "doc-gone"}
	delete(env.store.docs, "doc-gone")
	if w := doReq(t, env.router, "GET", "/api/v1/chunks/10000000-0000-0000-0000-000000000003", nil, jwtUser); w.Code != 404 {
		t.Fatalf("文档已删除的 chunk 应 404: %d", w.Code)
	}
	// 自己的文档 → 200
	uidA := "user-a"
	env.store.kbs["kb-a"] = store.KnowledgeBase{ID: "kb-a", Name: "A 库", OwnerID: &uidA}
	env.store.docs["doc-a"] = store.Document{ID: "doc-a", KBID: "kb-a"}
	env.vs.payloads["10000000-0000-0000-0000-000000000004"] = map[string]any{"content": "自己的内容", "document_id": "doc-a"}
	if w := doReq(t, env.router, "GET", "/api/v1/chunks/10000000-0000-0000-0000-000000000004", nil, jwtUser); w.Code != 200 {
		t.Fatalf("自己文档的 chunk 应 200: %d %s", w.Code, w.Body.String())
	}
}
