package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
)

// ---------- 认证（spec F3） ----------

func TestAuthFailures(t *testing.T) {
	st := newFakeStore()
	st.addKey(store.APIKey{ID: "key-1", KeyHash: "token_ok", Enabled: true,
		MCPTools: AllTools, MCPKBScope: "all"})
	st.addKey(store.APIKey{ID: "key-disabled", KeyHash: "token_disabled", Enabled: false})
	h, _ := newTestServer(t, st)

	body := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
	cases := []struct {
		name  string
		token string
	}{
		{"缺失 Authorization", ""},
		{"无效 Key", "binrag_wrong"},
		{"停用 Key", "token_disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := mcpPost(t, h, tc.token, "", body)
			if rec.Code != 401 {
				t.Errorf("应返回 HTTP 401，实际 %d (%s)", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 应带 WWW-Authenticate")
			}
		})
	}
}

// ---------- 授权（spec F4/F5/F8） ----------

// Tool 越权：历史 Key（无 Tool 权限）与白名单外 Tool → -32001
func TestToolPermissionDenied(t *testing.T) {
	st := newFakeStore()
	st.addKey(store.APIKey{ID: "hist", KeyHash: "token_hist", Enabled: true}) // 历史 Key：无任何 MCP 权限
	st.addKey(store.APIKey{ID: "wl", KeyHash: "token_wl", Enabled: true,
		MCPTools: []string{ToolRetrieve}, MCPKBScope: "all"})
	h, _ := newTestServer(t, st)

	// 历史 Key：任何 tool → -32001
	rec, resp := mcpPost(t, h, "token_hist", "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})
	if rec.Code != 200 {
		t.Fatalf("授权失败响应应为 HTTP 200（JSON-RPC error 层），实际 %d", rec.Code)
	}
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("历史 Key 应返回 -32001，实际 %v", errObj["code"])
	}

	// 白名单外 tool（wl 只有 retrieve，调 ask）→ -32001
	sid := doInitialize(t, h, "token_wl")
	rec, resp = mcpPost(t, h, "token_wl", sid, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": ToolAsk, "arguments": map[string]any{"question": "hi"}}})
	errObj = resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("白名单外 Tool 应返回 -32001，实际 %v", errObj["code"])
	}
}

// KB 越权：allowlist 外 kb_id → -32001；scope=all 可访问；scope=” 未指定 kb_id → -32001
func TestKBPermissionDenied(t *testing.T) {
	st := newFakeStore()
	st.kbs["kb-a"] = store.KnowledgeBase{ID: "kb-a", Name: "A", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	st.kbs["kb-b"] = store.KnowledgeBase{ID: "kb-b", Name: "B", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	st.addKey(store.APIKey{ID: "wl", KeyHash: "token_wl", Enabled: true,
		MCPTools: []string{ToolRetrieve, ToolGetKB, ToolAsk}, MCPKBScope: "allowlist", MCPKBIDs: []string{"kb-a"}})
	st.addKey(store.APIKey{ID: "none", KeyHash: "token_none", Enabled: true,
		MCPTools: []string{ToolRetrieve}, MCPKBScope: ""}) // 无知识库权限
	h, _ := newTestServer(t, st)
	sid := doInitialize(t, h, "token_wl")

	// allowlist 外 kb_id → -32001（统一消息「知识库不存在或无权限」）
	_, resp := mcpPost(t, h, "token_wl", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolRetrieve,
			"arguments": map[string]any{"query": "q", "kb_id": "kb-b"}}})
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != ErrCodePermissionDenied {
		t.Errorf("allowlist 外 kb_id 应 -32001，实际 %v", errObj["code"])
	}
	if errObj["message"] != msgKBForbidden {
		t.Errorf("消息应统一为 %q，实际 %q", msgKBForbidden, errObj["message"])
	}

	// get_knowledge_base 越权 → -32001
	_, resp = mcpPost(t, h, "token_wl", sid, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": ToolGetKB, "arguments": map[string]any{"kb_id": "kb-b"}}})
	if int(resp["error"].(map[string]any)["code"].(float64)) != ErrCodePermissionDenied {
		t.Error("get_knowledge_base 越权应 -32001")
	}

	// scope='' 且未指定 kb_id → -32001
	sid2 := doInitialize(t, h, "token_none")
	_, resp = mcpPost(t, h, "token_none", sid2, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": ToolRetrieve, "arguments": map[string]any{"query": "q"}}})
	if int(resp["error"].(map[string]any)["code"].(float64)) != ErrCodePermissionDenied {
		t.Error("无知识库权限未指定 kb_id 应 -32001")
	}
}

// Task 越权：无权限知识库的任务 → -32001「任务不存在」；与真实不存在响应一致
func TestTaskPermissionDenied(t *testing.T) {
	st := newFakeStore()
	st.kbs["kb-a"] = store.KnowledgeBase{ID: "kb-a", Name: "A", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	st.tasks["task-1"] = store.Task{ID: "task-1", KBID: "kb-a", Status: "completed"}
	st.addKey(store.APIKey{ID: "wl", KeyHash: "token_wl", Enabled: true,
		MCPTools: []string{ToolGetTask}, MCPKBScope: "allowlist", MCPKBIDs: []string{"kb-a"}})
	st.addKey(store.APIKey{ID: "other", KeyHash: "token_other", Enabled: true,
		MCPTools: []string{ToolGetTask}, MCPKBScope: "allowlist", MCPKBIDs: []string{"kb-other"}})
	h, _ := newTestServer(t, st)

	// 越权（other 无 kb-a 权限）→ -32001「任务不存在」
	sid := doInitialize(t, h, "token_other")
	rec, resp := mcpPost(t, h, "token_other", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolGetTask, "arguments": map[string]any{"task_id": "task-1"}}})
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != ErrCodePermissionDenied || errObj["message"] != msgTaskForbidden {
		t.Errorf("越权任务应 -32001「任务不存在」，实际 %v", errObj)
	}

	// 真实不存在 → 同样 -32001「任务不存在」（不泄露存在性）
	sid2 := doInitialize(t, h, "token_wl")
	rec, resp = mcpPost(t, h, "token_wl", sid2, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": ToolGetTask, "arguments": map[string]any{"task_id": "no-such"}}})
	errObj2 := resp["error"].(map[string]any)
	if int(errObj2["code"].(float64)) != ErrCodePermissionDenied || errObj2["message"] != msgTaskForbidden {
		t.Errorf("不存在任务应返回与越权相同响应，实际 %v", errObj2)
	}
	_ = rec
}

// ---------- 各 Tool 成功路径 ----------

func TestToolsSuccessPaths(t *testing.T) {
	st := newFakeStore()
	now := time.Now()
	st.kbs["kb-a"] = store.KnowledgeBase{ID: "kb-a", Name: "库A", Description: "desc", CreatedAt: now, UpdatedAt: now}
	st.docs["doc-1"] = store.Document{ID: "doc-1", KBID: "kb-a", Filename: "a.md", Status: "completed", CreatedAt: now}
	st.tasks["task-1"] = store.Task{ID: "task-1", KBID: "kb-a", DocumentID: "doc-1", Status: "completed", CreatedAt: now, UpdatedAt: now}
	st.addKey(store.APIKey{ID: "full", KeyHash: "token_full", Enabled: true,
		MCPTools: AllTools, MCPKBScope: "all"})
	h, sink := newTestServer(t, st)
	sid := doInitialize(t, h, "token_full")

	type callCase struct {
		name string
		args map[string]any
	}
	cases := []callCase{
		{ToolListKBs, map[string]any{}},
		{ToolGetKB, map[string]any{"kb_id": "kb-a"}},
		{ToolRetrieve, map[string]any{"query": "问题"}},
		{ToolAsk, map[string]any{"question": "你好"}},
		{ToolListDocs, map[string]any{"kb_id": "kb-a"}},
		{ToolGetTask, map[string]any{"task_id": "task-1"}},
	}
	for i, tc := range cases {
		rec, resp := mcpPost(t, h, "token_full", sid, map[string]any{
			"jsonrpc": "2.0", "id": i + 1, "method": "tools/call",
			"params": map[string]any{"name": tc.name, "arguments": tc.args}})
		if rec.Code != 200 {
			t.Fatalf("%s: HTTP %d", tc.name, rec.Code)
		}
		if _, hasErr := resp["error"]; hasErr {
			t.Errorf("%s 不应返回 error: %v", tc.name, resp["error"])
		}
		if isErr, _ := resp["result"].(map[string]any)["isError"].(bool); isErr {
			t.Errorf("%s 不应 isError", tc.name)
		}
	}

	// ask 结果不含 thinking（不暴露内部推理）
	_, resp := mcpPost(t, h, "token_full", sid, map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "tools/call",
		"params": map[string]any{"name": ToolAsk, "arguments": map[string]any{"question": "q"}}})
	sc := resp["result"].(map[string]any)["structuredContent"]
	if sc == nil {
		t.Fatal("ask 应返回 structuredContent")
	}
	scMap := sc.(map[string]any)
	if _, hasThinking := scMap["thinking"]; hasThinking {
		t.Error("ask 响应不应含 thinking 字段")
	}
	if _, hasAnswer := scMap["answer"]; !hasAnswer {
		t.Error("ask 响应应含 answer")
	}
	if _, hasSources := scMap["sources"]; !hasSources {
		t.Error("ask 响应应含 sources")
	}

	// 审计：6 次成功调用 + ask 复查，共 7 条；Shutdown flush 后断言
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sink.Shutdown(ctx)
	if got := st.auditCount(); got != 7 {
		t.Errorf("应产生 7 条审计（含 1 次 ask 复查），实际 %d", got)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, log := range st.logs {
		if log.APIKeyID != "key-1" && log.APIKeyID != "full" {
			t.Errorf("审计 api_key_id 应为调用方 Key，实际 %q", log.APIKeyID)
		}
		if log.Status != "success" {
			t.Errorf("成功调用审计 status 应为 success，实际 %q", log.Status)
		}
	}
}

// 审计：越权调用（网关层拦截，未进入 handler）不产生审计；认证失败（401）不产生审计
func TestAuditOnlyForAuthorizedCalls(t *testing.T) {
	st := newFakeStore()
	st.addKey(store.APIKey{ID: "none", KeyHash: "token_none", Enabled: true}) // 无权限
	h, sink := newTestServer(t, st)

	// 越权 tools/call（网关层 -32001）不产生审计
	sid := doInitialize(t, h, "token_none")
	mcpPost(t, h, "token_none", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})

	// 认证失败不产生审计
	mcpPost(t, h, "bad_token", "", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sink.Shutdown(ctx)
	if got := st.auditCount(); got != 0 {
		t.Errorf("越权/未认证调用不应产生审计，实际 %d 条", got)
	}
}

// 审计：handler 内部错误（ask 引擎失败）→ isError 结果 + 审计 status=error；
// 成功调用 → status=success（checklist「成功/失败调用产生审计记录」）
func TestAuditSuccessAndErrorRecords(t *testing.T) {
	st := newFakeStore()
	st.addKey(store.APIKey{ID: "ok", KeyHash: "token_ok", Enabled: true,
		MCPTools: []string{ToolAsk}, MCPKBScope: "all"})
	audit := NewAuditSink(st, 64, 2000)
	defer audit.Shutdown(context.Background())
	h := NewHandler(Dependencies{
		Store:  st,
		Engine: func() rag.Engine { return &fakeEngine{err: errors.New("模型不可用")} },
		RT:     func() retriever.Retriever { return &fakeRetriever{} },
		Audit:  audit,
	})
	sid := doInitialize(t, h, "token_ok")

	// ask 引擎错误 → isError 结果 + 审计 error
	_, resp := mcpPost(t, h, "token_ok", sid, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": ToolAsk, "arguments": map[string]any{"question": "q"}}})
	if isErr, _ := resp["result"].(map[string]any)["isError"].(bool); !isErr {
		t.Error("引擎错误应返回 isError 结果")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	audit.Shutdown(ctx)

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.logs) != 1 {
		t.Fatalf("应产生 1 条审计，实际 %d", len(st.logs))
	}
	log := st.logs[0]
	if log.Status != "error" || log.ToolName != ToolAsk || log.ErrorMessage == "" {
		t.Errorf("失败审计内容不符: %+v", log)
	}
	if log.APIKeyID != "ok" {
		t.Errorf("审计 api_key_id 应为调用方，实际 %q", log.APIKeyID)
	}
	if log.ParamsLen == 0 {
		t.Error("审计应记录 params_len")
	}
}
