package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/Bin-hy/bin-rag/internal/vectorstore"
	"github.com/gin-gonic/gin"
)

// ---------- fakes ----------

type fakeStore struct {
	mu    sync.Mutex
	kbs   map[string]store.KnowledgeBase
	docs  map[string]store.Document
	tasks map[string]store.Task
	keys  map[string]store.APIKey
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		kbs:   make(map[string]store.KnowledgeBase),
		docs:  make(map[string]store.Document),
		tasks: make(map[string]store.Task),
		keys:  make(map[string]store.APIKey),
	}
}

func (f *fakeStore) CreateKB(ctx context.Context, kb store.KnowledgeBase) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kbs[kb.ID] = kb
	return nil
}
func (f *fakeStore) ListKBs(ctx context.Context) ([]store.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.KnowledgeBase
	for _, kb := range f.kbs {
		out = append(out, kb)
	}
	return out, nil
}
func (f *fakeStore) GetKB(ctx context.Context, id string) (*store.KnowledgeBase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kb, ok := f.kbs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := kb
	return &cp, nil
}
func (f *fakeStore) UpdateKB(ctx context.Context, kb store.KnowledgeBase) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kbs[kb.ID] = kb
	return nil
}
func (f *fakeStore) DeleteKB(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.kbs, id)
	return nil
}
func (f *fakeStore) CreateDocument(ctx context.Context, doc store.Document) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[doc.ID] = doc
	return nil
}
func (f *fakeStore) ListDocuments(ctx context.Context, kbID string) ([]store.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Document
	for _, d := range f.docs {
		if d.KBID == kbID {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeStore) GetDocument(ctx context.Context, id string) (*store.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := d
	return &cp, nil
}
func (f *fakeStore) UpdateDocumentStatus(ctx context.Context, id string, status string, chunkIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.docs[id]
	d.Status = status
	d.ChunkIDs = chunkIDs
	f.docs[id] = d
	return nil
}
func (f *fakeStore) DeleteDocument(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.docs, id)
	return nil
}
func (f *fakeStore) CreateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) GetTask(ctx context.Context, id string) (*store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := t
	return &cp, nil
}
func (f *fakeStore) ListTasks(ctx context.Context, kbID string) ([]store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Task
	for _, t := range f.tasks {
		if t.KBID == kbID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakeStore) UpdateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) ClaimPendingTasks(ctx context.Context, limit int) ([]store.Task, error) {
	return nil, nil
}
func (f *fakeStore) ResetProcessingTasks(ctx context.Context) error { return nil }
func (f *fakeStore) CreateAPIKey(ctx context.Context, k store.APIKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[k.ID] = k
	return nil
}
func (f *fakeStore) ListAPIKeys(ctx context.Context) ([]store.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.APIKey
	for _, k := range f.keys {
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeStore) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if k.KeyHash == hash {
			cp := k
			return &cp, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[id]
	k.Enabled = enabled
	f.keys[id] = k
	return nil
}
func (f *fakeStore) DeleteAPIKey(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, id)
	return nil
}
func (f *fakeStore) TouchAPIKey(ctx context.Context, id string) error { return nil }
func (f *fakeStore) HistoryStore() store.HistoryStore                 { return nil }
func (f *fakeStore) Migrate(ctx context.Context) error                { return nil }
func (f *fakeStore) Close()                                           {}

type fakeEngine struct {
	mu           sync.Mutex
	answer       string
	sources      []rag.Source
	streamChunks []string
	streamErr    error // 非 nil 时 StreamAsk 发 error 事件替代 chunk/done
	lastQuestion string
	lastAskOpts  []rag.AskOption
	askErr       error
}

func (f *fakeEngine) Ask(ctx context.Context, sessionID string, question string, opts ...rag.AskOption) (*rag.RAGResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuestion = question
	f.lastAskOpts = opts
	if f.askErr != nil {
		return nil, f.askErr
	}
	return &rag.RAGResult{Answer: f.answer, Sources: f.sources}, nil
}

func (f *fakeEngine) StreamAsk(ctx context.Context, sessionID string, question string, opts ...rag.AskOption) (<-chan rag.StreamEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuestion = question
	f.lastAskOpts = opts

	ch := make(chan rag.StreamEvent, len(f.streamChunks)+2)
	ch <- rag.StreamEvent{Type: rag.EventSources, Sources: f.sources}
	if f.streamErr != nil {
		ch <- rag.StreamEvent{Type: rag.EventError, Err: f.streamErr}
	} else {
		for _, s := range f.streamChunks {
			ch <- rag.StreamEvent{Type: rag.EventChunk, Content: s}
		}
		ch <- rag.StreamEvent{Type: rag.EventDone}
	}
	close(ch)
	return ch, nil
}

type fakeVS struct {
	deleted [][]string
}

func (f *fakeVS) Upsert(ctx context.Context, records []vectorstore.VectorRecord) error { return nil }
func (f *fakeVS) Search(ctx context.Context, req vectorstore.SearchRequest) ([]vectorstore.SearchResult, error) {
	return nil, nil
}
func (f *fakeVS) Delete(ctx context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids)
	return nil
}
func (f *fakeVS) EnsureCollection(ctx context.Context) error { return nil }
func (f *fakeVS) Close() error                               { return nil }

type fakeBM25 struct {
	removed []string
}

func (f *fakeBM25) Add(id string, content string, kbID string)           {}
func (f *fakeBM25) Remove(id string)                                     { f.removed = append(f.removed, id) }
func (f *fakeBM25) Search(query string, topK int) []retriever.BM25Result { return nil }
func (f *fakeBM25) SearchFiltered(query string, topK int, kbID string) []retriever.BM25Result {
	return nil
}
func (f *fakeBM25) Rebuild(docs []retriever.BM25Doc) {}
func (f *fakeBM25) DocCount() int                    { return 0 }

type fakeRegistry struct {
	supported map[string]bool
}

func (f *fakeRegistry) Register(p loader.Parser) {}
func (f *fakeRegistry) Resolve(info loader.FileInfo) (loader.Parser, error) {
	ext := strings.ToLower(filepath.Ext(info.Filename))
	if f.supported[ext] {
		return fakeParser{}, nil
	}
	return nil, &loader.ErrUnsupportedFormat{Filename: info.Filename, MIMEType: info.MIMEType}
}

type fakeParser struct{}

func (fakeParser) Parse(ctx context.Context, r io.Reader, opts loader.LoadOptions) (*loader.LoadResult, error) {
	return nil, nil
}
func (fakeParser) SupportedExts() []string  { return nil }
func (fakeParser) SupportedMIMEs() []string { return nil }

// fakeHistoryStore 内存对话历史（store.HistoryStore 实现）
type fakeHistoryStore struct {
	mu   sync.Mutex
	msgs map[string][]llm.Message
}

func (f *fakeHistoryStore) Append(ctx context.Context, sessionID string, role string, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs[sessionID] = append(f.msgs[sessionID], llm.Message{Role: role, Content: content})
	return nil
}

func (f *fakeHistoryStore) Get(ctx context.Context, sessionID string, limit int) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msgs := f.msgs[sessionID]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (f *fakeHistoryStore) Clear(ctx context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.msgs, sessionID)
	return nil
}

// ---------- test env ----------

type testEnv struct {
	router  *gin.Engine
	store   *fakeStore
	engine  *fakeEngine
	vs      *fakeVS
	bm25    *fakeBM25
	history *fakeHistoryStore
}

const testAPIKey = "test-secret-key"

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	fs := newFakeStore()
	fs.keys["key-1"] = store.APIKey{ID: "key-1", Name: "测试", KeyHash: keyHash(testAPIKey), Enabled: true}

	fe := &fakeEngine{answer: "这是测试回答", sources: []rag.Source{{ID: "r1", Filename: "a.md", Score: 0.9}}}
	fv := &fakeVS{}
	fb := &fakeBM25{}
	reg := &fakeRegistry{supported: map[string]bool{".txt": true, ".md": true}}
	fh := &fakeHistoryStore{msgs: make(map[string][]llm.Message)}

	cfg := config.ServerConfig{
		Port:            8080,
		FileStorageDir:  t.TempDir(),
		UploadMaxSizeMB: 10,
		WorkerCount:     2,
		TaskMaxRetries:  3,
		AuthEnabled:     true,
	}

	router := NewRouter(Dependencies{
		Config:   cfg,
		Store:    fs,
		VS:       fv,
		BM25:     fb,
		Registry: reg,
		Engine:   fe,
		History:  fh,
	})

	return &testEnv{router: router, store: fs, engine: fe, vs: fv, bm25: fb, history: fh}
}

func doReq(t *testing.T, r *gin.Engine, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal 失败: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeResp(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body=%s", err, w.Body.String())
	}
	return resp
}

// ---------- 测试 ----------

// AC1: 创建知识库后列表可见
func TestKBCreateAndList(t *testing.T) {
	env := newTestEnv(t)

	w := doReq(t, env.router, "POST", "/api/v1/knowledge-bases",
		map[string]string{"name": "产品文档库", "description": "产品手册"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("创建知识库状态码错误: %d %s", w.Code, w.Body.String())
	}
	resp := decodeResp(t, w)
	if resp.Code != CodeOK {
		t.Fatalf("业务码错误: %+v", resp)
	}

	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("列表状态码错误: %d", w.Code)
	}
	resp = decodeResp(t, w)
	list, ok := resp.Data.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("列表内容错误: %+v", resp.Data)
	}
}

// AC2+AC9: 上传返回 task_id，任务与文档记录创建，统一响应格式
func TestUploadReturnsTaskID(t *testing.T) {
	env := newTestEnv(t)

	// 先建知识库
	doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "kb"}, testAPIKey)

	// 从 fake store 拿任意一个 kb id
	env.store.mu.Lock()
	var kbID string
	for id := range env.store.kbs {
		kbID = id
		break
	}
	env.store.mu.Unlock()

	// multipart 上传（kb_id 走 query）
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	fw.Write([]byte("这是测试文档内容"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/documents/upload?kb_id="+kbID, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("上传状态码错误: %d %s", w.Code, w.Body.String())
	}
	resp := decodeResp(t, w)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("响应数据错误: %+v", resp.Data)
	}
	if data["task_id"] == "" || data["document_id"] == "" {
		t.Errorf("应返回 task_id 与 document_id: %+v", data)
	}

	// 任务记录存在且 pending
	taskID := data["task_id"].(string)
	task, _ := env.store.GetTask(context.Background(), taskID)
	if task == nil || task.Status != store.TaskStatusPending {
		t.Errorf("任务记录错误: %+v", task)
	}

	// 文档列表可见
	w = doReq(t, env.router, "GET", "/api/v1/documents?kb_id="+kbID, nil, testAPIKey)
	resp = decodeResp(t, w)
	docs, ok := resp.Data.([]any)
	if !ok || len(docs) != 1 {
		t.Errorf("文档列表错误: %+v", resp.Data)
	}
}

// AC3: 不支持格式 → 400
func TestUploadUnsupportedFormat(t *testing.T) {
	env := newTestEnv(t)

	// 先建一个存在的知识库
	doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "kb"}, testAPIKey)
	env.store.mu.Lock()
	var kbID string
	for id := range env.store.kbs {
		kbID = id
		break
	}
	env.store.mu.Unlock()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "evil.exe")
	fw.Write([]byte("bad"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/documents/upload?kb_id="+kbID, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("不支持格式应 400，实际 %d %s", w.Code, w.Body.String())
	}
}

// AC4: 删除文档清理向量与 BM25
func TestDeleteDocument(t *testing.T) {
	env := newTestEnv(t)
	env.store.docs["doc-1"] = store.Document{
		ID: "doc-1", KBID: "kb-1", Filename: "a.md", Status: store.DocStatusCompleted,
		ChunkIDs: []string{"c1", "c2"},
	}

	w := doReq(t, env.router, "DELETE", "/api/v1/documents/doc-1", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("删除状态码错误: %d %s", w.Code, w.Body.String())
	}

	if len(env.vs.deleted) != 1 || len(env.vs.deleted[0]) != 2 {
		t.Errorf("向量删除未按 chunk_ids 执行: %+v", env.vs.deleted)
	}
	if len(env.bm25.removed) != 2 {
		t.Errorf("BM25 移除未执行: %+v", env.bm25.removed)
	}
	if _, err := env.store.GetDocument(context.Background(), "doc-1"); err == nil {
		t.Error("文档记录应已删除")
	}
}

// AC3: 上传大小超限 → 400（MaxBytesReader 在 body 读取前生效）
func TestUploadTooLarge(t *testing.T) {
	env := newTestEnv(t)
	doReq(t, env.router, "POST", "/api/v1/knowledge-bases", map[string]string{"name": "kb"}, testAPIKey)
	env.store.mu.Lock()
	var kbID string
	for id := range env.store.kbs {
		kbID = id
		break
	}
	env.store.mu.Unlock()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "big.txt")
	// 超过 UploadMaxSizeMB=10 的限制
	fw.Write(bytes.Repeat([]byte("x"), 11*1024*1024))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/documents/upload?kb_id="+kbID, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("超限上传应 400，实际 %d %s", w.Code, w.Body.String())
	}
	// 不应创建文档与任务记录
	if len(env.store.docs) != 0 || len(env.store.tasks) != 0 {
		t.Error("超限上传不应创建文档/任务记录")
	}
}

// AC6: 问答返回回答与引用来源
func TestChat(t *testing.T) {
	env := newTestEnv(t)

	w := doReq(t, env.router, "POST", "/api/v1/chat",
		map[string]string{"session_id": "s1", "question": "产品支持哪些格式？", "kb_id": "kb-1"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("问答状态码错误: %d %s", w.Code, w.Body.String())
	}

	resp := decodeResp(t, w)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("响应数据错误: %+v", resp.Data)
	}
	if data["answer"] != "这是测试回答" {
		t.Errorf("回答错误: %+v", data)
	}
	if sources, ok := data["sources"].([]any); !ok || len(sources) != 1 {
		t.Errorf("引用来源错误: %+v", data["sources"])
	}
}

// AC6: SSE 流式事件序列 sources → chunk → done
func TestChatSSE(t *testing.T) {
	env := newTestEnv(t)
	env.engine.streamChunks = []string{"答案", "内容"}

	w := doReq(t, env.router, "POST", "/api/v1/chat?stream=1",
		map[string]string{"session_id": "s1", "question": "流式问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("SSE 状态码错误: %d %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "event:sources") {
		t.Errorf("缺少 sources 事件:\n%s", body)
	}
	if !strings.Contains(body, "event:chunk") {
		t.Errorf("缺少 chunk 事件:\n%s", body)
	}
	if !strings.Contains(body, "答案") || !strings.Contains(body, "内容") {
		t.Errorf("chunk 内容缺失:\n%s", body)
	}
	if !strings.Contains(body, "event:done") {
		t.Errorf("缺少 done 事件:\n%s", body)
	}
}

// SSE error 事件：流中出错发 error 事件终止，不再有 chunk/done
func TestChatSSEError(t *testing.T) {
	env := newTestEnv(t)
	env.engine.streamErr = errors.New("模型生成超时")

	w := doReq(t, env.router, "POST", "/api/v1/chat?stream=1",
		map[string]string{"session_id": "s1", "question": "流式问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("SSE 状态码错误: %d %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "event:error") {
		t.Errorf("缺少 error 事件:\n%s", body)
	}
	if !strings.Contains(body, "模型生成超时") {
		t.Errorf("error 事件缺少错误信息:\n%s", body)
	}
	if strings.Contains(body, "event:done") {
		t.Errorf("error 后不应再发 done 事件:\n%s", body)
	}
	if strings.Contains(body, "event:chunk") {
		t.Errorf("error 后不应再发 chunk 事件:\n%s", body)
	}
}

// SSE 空流：无 chunk 直接 sources → done
func TestChatSSEEmptyStream(t *testing.T) {
	env := newTestEnv(t)
	env.engine.streamChunks = nil

	w := doReq(t, env.router, "POST", "/api/v1/chat?stream=1",
		map[string]string{"session_id": "s1", "question": "空流问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("SSE 状态码错误: %d %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "event:sources") {
		t.Errorf("缺少 sources 事件:\n%s", body)
	}
	if !strings.Contains(body, "event:done") {
		t.Errorf("缺少 done 事件:\n%s", body)
	}
	if strings.Contains(body, "event:chunk") {
		t.Errorf("空流不应有 chunk 事件:\n%s", body)
	}
}

// AC8: 认证——无 Key / 错误 Key 401，正确 Key 通过
func TestAuth(t *testing.T) {
	env := newTestEnv(t)

	// 无 Key
	w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, "")
	if w.Code != 401 {
		t.Errorf("无 Key 应 401，实际 %d", w.Code)
	}

	// 错误 Key
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, "wrong-key")
	if w.Code != 401 {
		t.Errorf("错误 Key 应 401，实际 %d", w.Code)
	}

	// 正确 Key
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	if w.Code != 200 {
		t.Errorf("正确 Key 应 200，实际 %d %s", w.Code, w.Body.String())
	}
}

// AC8: 停用 Key 后 401
func TestAuthDisabledKey(t *testing.T) {
	env := newTestEnv(t)
	env.store.SetAPIKeyEnabled(context.Background(), "key-1", false)

	w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	if w.Code != 401 {
		t.Errorf("停用 Key 应 401，实际 %d", w.Code)
	}
}

// AC7: 对话历史查询
func TestGetHistory(t *testing.T) {
	env := newTestEnv(t)
	env.history.msgs["s1"] = []llm.Message{
		{Role: "user", Content: "问题1"},
		{Role: "assistant", Content: "回答1"},
	}

	w := doReq(t, env.router, "GET", "/api/v1/chat/history?session_id=s1", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("历史状态码错误: %d %s", w.Code, w.Body.String())
	}
	resp := decodeResp(t, w)
	msgs, ok := resp.Data.([]any)
	if !ok || len(msgs) != 2 {
		t.Errorf("历史内容错误: %+v", resp.Data)
	}
}

// AC10: 任务重试——failed → pending
func TestRetryTask(t *testing.T) {
	env := newTestEnv(t)
	env.store.tasks["task-1"] = store.Task{
		ID: "task-1", KBID: "kb-1", DocumentID: "doc-1",
		Status: store.TaskStatusFailed, RetryCount: 3, ErrorMessage: "boom",
	}

	w := doReq(t, env.router, "POST", "/api/v1/tasks/task-1/retry", nil, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("重试状态码错误: %d %s", w.Code, w.Body.String())
	}

	task, _ := env.store.GetTask(context.Background(), "task-1")
	if task.Status != store.TaskStatusPending || task.RetryCount != 0 {
		t.Errorf("重试后状态错误: %+v", task)
	}
}

// 非 failed 任务不可重试
func TestRetryTaskNotFailed(t *testing.T) {
	env := newTestEnv(t)
	env.store.tasks["task-1"] = store.Task{ID: "task-1", Status: store.TaskStatusCompleted}

	w := doReq(t, env.router, "POST", "/api/v1/tasks/task-1/retry", nil, testAPIKey)
	if w.Code != 400 {
		t.Errorf("非 failed 任务重试应 400，实际 %d", w.Code)
	}
}

// API Key 管理：创建返回明文一次、列表不含 hash、停用生效
func TestAPIKeyLifecycle(t *testing.T) {
	env := newTestEnv(t)

	// 创建
	w := doReq(t, env.router, "POST", "/api/v1/api-keys", map[string]string{"name": "新密钥"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("创建 Key 状态码错误: %d %s", w.Code, w.Body.String())
	}
	resp := decodeResp(t, w)
	data := resp.Data.(map[string]any)
	plainKey, _ := data["key"].(string)
	if !strings.HasPrefix(plainKey, "binrag_") {
		t.Errorf("应返回明文 Key: %+v", data)
	}

	// 新 Key 可用
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, plainKey)
	if w.Code != 200 {
		t.Errorf("新 Key 应可用，实际 %d", w.Code)
	}

	// 列表不含 hash
	w = doReq(t, env.router, "GET", "/api/v1/api-keys", nil, testAPIKey)
	body := w.Body.String()
	if strings.Contains(body, "key_hash") || strings.Contains(body, plainKey) {
		t.Errorf("列表不应暴露 hash 或明文: %s", body)
	}

	// 停用后 401
	env.store.mu.Lock()
	var newKeyID string
	for id, k := range env.store.keys {
		if k.Name == "新密钥" {
			newKeyID = id
		}
	}
	env.store.mu.Unlock()
	doReq(t, env.router, "POST", "/api/v1/api-keys/"+newKeyID+"/toggle", map[string]bool{"enabled": false}, testAPIKey)
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, plainKey)
	if w.Code != 401 {
		t.Errorf("停用后应 401，实际 %d", w.Code)
	}
}

// 统一响应格式（成功与错误都含 code/message/data）
func TestUnifiedResponseShape(t *testing.T) {
	env := newTestEnv(t)

	// 成功
	w := doReq(t, env.router, "GET", "/api/v1/knowledge-bases", nil, testAPIKey)
	var okResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &okResp)
	if _, hasCode := okResp["code"]; !hasCode {
		t.Error("成功响应缺少 code")
	}
	if _, hasMsg := okResp["message"]; !hasMsg {
		t.Error("成功响应缺少 message")
	}
	if _, hasData := okResp["data"]; !hasData {
		t.Error("成功响应缺少 data")
	}

	// 错误（不存在）
	w = doReq(t, env.router, "GET", "/api/v1/knowledge-bases/not-exist", nil, testAPIKey)
	var errResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if _, hasCode := errResp["code"]; !hasCode {
		t.Error("错误响应缺少 code")
	}
	if _, hasMsg := errResp["message"]; !hasMsg {
		t.Error("错误响应缺少 message")
	}
}

// 扫描件/无可读文本上传：预检拒绝返回 400，不创建文档/任务
func TestUploadNoReadableContent(t *testing.T) {
	fs := newFakeStore()
	fs.keys["key-1"] = store.APIKey{ID: "key-1", Name: "测试", KeyHash: keyHash(testAPIKey), Enabled: true}
	fe := &fakeEngine{}
	fv := &fakeVS{}
	fb := &fakeBM25{}
	fh := &fakeHistoryStore{msgs: make(map[string][]llm.Message)}

	// 需要预检启用：LoaderCfg.MinReadableChars=20；且 fakeRegistry 能真实解析（用真实 loader）
	realLoader := loader.NewDefaultRegistry()
	cfg := config.ServerConfig{
		Port:            8080,
		FileStorageDir:  t.TempDir(),
		UploadMaxSizeMB: 10,
		WorkerCount:     2,
		TaskMaxRetries:  3,
		AuthEnabled:     true,
	}
	router := NewRouter(Dependencies{
		Config:    cfg,
		LoaderCfg: config.LoaderConfig{MinReadableChars: 20},
		Store:     fs,
		VS:        fv,
		BM25:      fb,
		Registry:  realLoader, // 真实 registry：txt parser 可用
		Engine:    fe,
		History:   fh,
	})

	// 创建一个知识库（kb_id 必须是 UUID）
	kbID := "4f2a6d1c-8b9e-4a10-b1c2-d3e4f5a6b7c8"
	fs.kbs[kbID] = store.KnowledgeBase{ID: kbID, Name: "扫描测试"}

	// 乱码 TXT（无可读文本）
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "scan.txt")
	fw.Write([]byte("q 595.44 0 0 841.68 cm 1 g /Im10 Do Q\nq 595.44 0 0 841.68 cm 1 g /Im11 Do Q"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/documents/upload?kb_id="+kbID, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("扫描件应 400，实际 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "无可读文本") {
		t.Errorf("错误信息应含「无可读文本」: %s", w.Body.String())
	}
	// 不应创建文档
	fs.mu.Lock()
	docCount := len(fs.docs)
	fs.mu.Unlock()
	if docCount != 0 {
		t.Errorf("拒绝后不应创建文档记录，实际 %d 条", docCount)
	}
}
