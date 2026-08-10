package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/jackc/pgx/v5"
)

// pgxErrNoRows 模拟查询无结果（与真实 store 一致）
func pgxErrNoRows() error { return pgx.ErrNoRows }

// ---------- fake 基建 ----------

// fakeStore 内存版 store.Store（仅实现测试所需方法，其余空实现）
type fakeStore struct {
	mu     sync.Mutex
	kbs    map[string]store.KnowledgeBase
	docs   map[string]store.Document
	tasks  map[string]store.Task
	keys   map[string]store.APIKey // id → key
	byHash map[string]string       // key_hash → id
	logs   []store.AuditLog
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		kbs:    map[string]store.KnowledgeBase{},
		docs:   map[string]store.Document{},
		tasks:  map[string]store.Task{},
		keys:   map[string]store.APIKey{},
		byHash: map[string]string{},
	}
}

// addKey 注册 Key（hash 计算）
func (f *fakeStore) addKey(k store.APIKey) {
	sum := sha256.Sum256([]byte(k.KeyHash))
	f.byHash[hex.EncodeToString(sum[:])] = k.ID
	f.keys[k.ID] = k
}

func (f *fakeStore) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byHash[hash]
	if !ok {
		return nil, nil
	}
	k := f.keys[id]
	return &k, nil
}
func (f *fakeStore) ListAllKBs(ctx context.Context) ([]store.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.KnowledgeBase, 0, len(f.kbs))
	for _, kb := range f.kbs {
		out = append(out, kb)
	}
	return out, nil
}
func (f *fakeStore) ListKBsByIDs(ctx context.Context, ids []string) ([]store.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.KnowledgeBase, 0, len(ids))
	for _, id := range ids {
		if kb, ok := f.kbs[id]; ok {
			out = append(out, kb)
		}
	}
	return out, nil
}
func (f *fakeStore) GetKB(ctx context.Context, id string) (*store.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kb, ok := f.kbs[id]
	if !ok {
		return nil, pgxErrNoRows()
	}
	return &kb, nil
}
func (f *fakeStore) ListDocuments(ctx context.Context, kbID string) ([]store.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Document{}
	for _, d := range f.docs {
		if d.KBID == kbID {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeStore) GetTask(ctx context.Context, id string) (*store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, pgxErrNoRows()
	}
	return &t, nil
}
func (f *fakeStore) AppendAuditLog(ctx context.Context, log store.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, log)
	return nil
}
func (f *fakeStore) UpdateAPIKeyPermissions(ctx context.Context, id string, p store.APIKeyPermissions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[id]
	k.MCPTools, k.MCPKBScope, k.MCPKBIDs = p.MCPTools, p.MCPKBScope, p.MCPKBIDs
	f.keys[id] = k
	return nil
}
func (f *fakeStore) auditCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.logs)
}

// 未用方法空实现
func (f *fakeStore) CreateKB(context.Context, store.KnowledgeBase) error { return nil }
func (f *fakeStore) ListKBsByOwner(context.Context, string) ([]store.KnowledgeBase, error) {
	return nil, nil
}
func (f *fakeStore) UpdateKB(context.Context, store.KnowledgeBase) error { return nil }
func (f *fakeStore) DeleteKB(context.Context, string) error              { return nil }
func (f *fakeStore) GetOrCreateUser(context.Context, store.User) (*store.User, error) {
	return nil, nil
}
func (f *fakeStore) GetUser(context.Context, string) (*store.User, error)                 { return nil, nil }
func (f *fakeStore) CreateDocument(context.Context, store.Document) error                 { return nil }
func (f *fakeStore) GetDocument(context.Context, string) (*store.Document, error)         { return nil, nil }
func (f *fakeStore) UpdateDocumentStatus(context.Context, string, string, []string) error { return nil }
func (f *fakeStore) DeleteDocument(context.Context, string) error                         { return nil }
func (f *fakeStore) CreateTask(context.Context, store.Task) error                         { return nil }
func (f *fakeStore) ListTasks(context.Context, string) ([]store.Task, error)              { return nil, nil }
func (f *fakeStore) UpdateTask(context.Context, store.Task) error                         { return nil }
func (f *fakeStore) ClaimPendingTasks(context.Context, int) ([]store.Task, error)         { return nil, nil }
func (f *fakeStore) ResetProcessingTasks(context.Context) error                           { return nil }
func (f *fakeStore) CreateAPIKey(context.Context, store.APIKey) error                     { return nil }
func (f *fakeStore) ListAPIKeys(context.Context) ([]store.APIKey, error)                  { return nil, nil }
func (f *fakeStore) SetAPIKeyEnabled(context.Context, string, bool) error                 { return nil }
func (f *fakeStore) DeleteAPIKey(context.Context, string) error                           { return nil }
func (f *fakeStore) TouchAPIKey(context.Context, string) error                            { return nil }
func (f *fakeStore) HistoryStore() store.HistoryStore                                     { return nil }
func (f *fakeStore) Migrate(context.Context) error                                        { return nil }
func (f *fakeStore) Close()                                                               {}

// fakeEngine 固定回答的 rag.Engine（err 非 nil 时 Ask 返回错误）
type fakeEngine struct{ err error }

func (e *fakeEngine) Ask(ctx context.Context, sessionID, question string, opts ...rag.AskOption) (*rag.RAGResult, error) {
	if e.err != nil {
		return nil, e.err
	}
	return &rag.RAGResult{
		Answer:  "基于知识库的回答：" + question,
		Sources: []rag.Source{{ID: "chunk-1", Filename: "doc.md", Score: 0.9}},
	}, nil
}
func (e *fakeEngine) StreamAsk(ctx context.Context, sessionID, question string, opts ...rag.AskOption) (<-chan rag.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

// fakeRetriever 固定结果的检索器
type fakeRetriever struct{}

func (r *fakeRetriever) Search(ctx context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
	return []retriever.RetrieveResult{{
		ID: "chunk-1", Content: "测试内容", Score: 0.9,
		Metadata: map[string]any{"filename": "doc.md", "kb_id": "kb-a"},
	}}, nil
}
func (r *fakeRetriever) SearchMulti(ctx context.Context, req retriever.RetrieveRequest, q []string) ([]retriever.RetrieveResult, error) {
	return r.Search(ctx, req)
}
func (r *fakeRetriever) SearchByVector(ctx context.Context, v []float32, topK int, f map[string]any) ([]retriever.RetrieveResult, error) {
	return nil, nil
}
func (r *fakeRetriever) Rerank(ctx context.Context, query string, results []retriever.RetrieveResult, topN int, trace func(retriever.RetrieveTrace)) ([]retriever.RetrieveResult, error) {
	return results, nil
}

// ---------- 测试工具 ----------

// newTestServer 构建带认证的 MCP handler（审计用同步 fake store 直写便于断言）
func newTestServer(t *testing.T, st *fakeStore) (http.Handler, *AuditSink) {
	t.Helper()
	audit := NewAuditSink(st, 64, 2000)
	t.Cleanup(func() { audit.Shutdown(context.Background()) })
	return NewHandler(Dependencies{
		Store:  st,
		Engine: func() rag.Engine { return &fakeEngine{} },
		RT:     func() retriever.Retriever { return &fakeRetriever{} },
		Audit:  audit,
	}), audit
}

// mcpPost 发一次 MCP 请求（带 Authorization 与可选 session）
func mcpPost(t *testing.T, h http.Handler, token, sessionID string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func rpcID(n int) any { return n }

// initialize 并返回 session id
func doInitialize(t *testing.T, h http.Handler, token string) string {
	t.Helper()
	rec, _ := mcpPost(t, h, token, "", map[string]any{
		"jsonrpc": "2.0", "id": rpcID(1), "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "0.1"},
		},
	})
	if rec.Code != 200 {
		t.Fatalf("initialize 失败: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Mcp-Session-Id")
}

// ---------- 冒烟测试（T11） ----------

func TestSmokeInitializeToolsListAndCall(t *testing.T) {
	st := newFakeStore()
	st.kbs["kb-a"] = store.KnowledgeBase{ID: "kb-a", Name: "库A", Description: "d", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	st.addKey(store.APIKey{ID: "key-1", KeyHash: "token_full", Enabled: true,
		MCPTools: []string{ToolListKBs}, MCPKBScope: "all"})

	h, _ := newTestServer(t, st)
	sid := doInitialize(t, h, "token_full")

	// tools/list：返回全部 6 个
	rec, resp := mcpPost(t, h, "token_full", sid, map[string]any{
		"jsonrpc": "2.0", "id": rpcID(2), "method": "tools/list"})
	if rec.Code != 200 {
		t.Fatalf("tools/list 失败: %d", rec.Code)
	}
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 6 {
		t.Fatalf("tools/list 应返回 6 个 Tool，实际 %d", len(tools))
	}

	// tools/call：list_knowledge_bases 成功
	rec, resp = mcpPost(t, h, "token_full", sid, map[string]any{
		"jsonrpc": "2.0", "id": rpcID(3), "method": "tools/call",
		"params": map[string]any{"name": ToolListKBs, "arguments": map[string]any{}}})
	if rec.Code != 200 {
		t.Fatalf("tools/call 失败: %d", rec.Code)
	}
	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("list_knowledge_bases 不应返回 error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("list_knowledge_bases 不应 isError: %v", result)
	}
	// StructuredContent 含 1 个知识库
	sc, ok := result["structuredContent"].([]any)
	if !ok || len(sc) != 1 {
		t.Fatalf("structuredContent 应含 1 个知识库，实际 %v", result["structuredContent"])
	}
}
