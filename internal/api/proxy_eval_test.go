package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/store"
)

// upstreamCapture 假评测上游服务：记录最近一次请求的透传细节
type upstreamCapture struct {
	mu       sync.Mutex
	hits     int
	method   string
	path     string
	rawQuery string
	token    string
	body     string
	respBody string
	respCode int
}

// newUpstream 启动 httptest 假上游，回显固定 JSON 响应
func newUpstream(t *testing.T) (*httptest.Server, *upstreamCapture) {
	t.Helper()
	cap := &upstreamCapture{respBody: `{"code":0,"message":"ok","data":{"status":"ok"}}`, respCode: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.hits++
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.rawQuery = r.URL.RawQuery
		cap.token = r.Header.Get(evalInternalTokenHeader)
		cap.body = string(body)
		cap.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cap.respCode)
		_, _ = w.Write([]byte(cap.respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// newEvalProxyTestEnv 构建带评测代理配置的测试环境（eval.service_url 指向假上游）。
// 返回真实 HTTP 服务器（httputil.ReverseProxy 需要真实 ResponseWriter，不能用 ResponseRecorder）。
func newEvalProxyTestEnv(t *testing.T, upstreamURL string) (*testEnv, *httptest.Server) {
	t.Helper()

	fs := newFakeStore()
	fs.keys["key-1"] = store.APIKey{ID: "key-1", Name: "测试", KeyHash: keyHash(testAPIKey), Enabled: true}

	authMgr := testAuthManager()
	fe := &fakeEngine{answer: "回答", sources: []rag.Source{{ID: "r1"}}}
	fh := &fakeHistoryStore{msgs: make(map[string][]llm.Message)}

	cfg := config.ServerConfig{
		Port:            8080,
		FileStorageDir:  t.TempDir(),
		UploadMaxSizeMB: 10,
		WorkerCount:     2,
		TaskMaxRetries:  3,
		AuthEnabled:     true,
	}
	globalCfg := &config.Config{
		Eval: config.EvalConfig{ServiceURL: upstreamURL, InternalToken: "internal-test-token"},
	}
	cfgMgr := config.NewConfigManager(filepath.Join(t.TempDir(), "config.yaml"), globalCfg)

	router := NewRouter(Dependencies{
		Config:   cfg,
		CfgMgr:   cfgMgr,
		Store:    fs,
		Auth:     authMgr,
		VS:       &fakeVS{},
		BM25:     &fakeBM25{},
		Registry: &fakeRegistry{supported: map[string]bool{".txt": true}},
		Engine:   func() rag.Engine { return fe },
		History:  fh,
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &testEnv{router: router, store: fs, engine: fe, history: fh, authMgr: authMgr}, srv
}

// doProxyReq 向真实测试服务器发请求，返回状态码与响应体
func doProxyReq(t *testing.T, srv *httptest.Server, method, path string, body string, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// 未配置评测服务（无 cfgMgr 快照）→ 503「评测服务未配置」
func TestEvalProxyNotConfigured(t *testing.T) {
	env := newTestEnv(t) // 无 cfgMgr，快照为 nil

	w := doReq(t, env.router, "GET", "/api/v1/eval/tasks", nil, testAPIKey)
	if w.Code != 503 {
		t.Fatalf("未配置评测服务应 503，实际 %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "评测服务未配置") {
		t.Errorf("错误信息应含「评测服务未配置」: %s", w.Body.String())
	}
}

// 纯透传：路径/查询串/Body 不改写，注入 X-Eval-Internal-Token 头，响应原样回传
func TestEvalProxyPassThrough(t *testing.T) {
	up, cap := newUpstream(t)
	env, srv := newEvalProxyTestEnv(t, up.URL)
	env.store.kbs["kb-1"] = store.KnowledgeBase{ID: "kb-1", Name: "库"}

	body := `{"name":"基线评测","kb_id":"kb-1","dataset_id":"ds-1"}`
	code, respBody := doProxyReq(t, srv, "POST", "/api/v1/eval/tasks?foo=bar&page=1", body, testAPIKey)
	if code != 200 {
		t.Fatalf("透传状态码错误: %d %s", code, respBody)
	}
	if string(respBody) != cap.respBody {
		t.Errorf("上游响应应原样回传: %q", respBody)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.hits != 1 {
		t.Fatalf("上游应被调用 1 次，实际 %d", cap.hits)
	}
	if cap.method != "POST" || cap.path != "/api/v1/eval/tasks" {
		t.Errorf("路径/方法应不改写: %s %s", cap.method, cap.path)
	}
	if cap.rawQuery != "foo=bar&page=1" {
		t.Errorf("查询串应不改写: %q", cap.rawQuery)
	}
	if cap.body != body {
		t.Errorf("Body 应原样透传（读取后复原）: %q", cap.body)
	}
	if cap.token != "internal-test-token" {
		t.Errorf("应注入内部令牌头: %q", cap.token)
	}
}

// POST /eval/tasks 提交任务：kb_id 越权 → 404，且不转发上游
func TestEvalProxyTaskKBForbidden(t *testing.T) {
	up, cap := newUpstream(t)
	env, srv := newEvalProxyTestEnv(t, up.URL)

	// kb 属于其他用户；当前以 user-1 的 JWT 提交 → 越权
	other := "user-2"
	env.store.kbs["kb-other"] = store.KnowledgeBase{ID: "kb-other", Name: "他人库", OwnerID: &other}
	jwtToken := issueTestJWT(t, env, "user-1")

	code, respBody := doProxyReq(t, srv, "POST", "/api/v1/eval/tasks", `{"kb_id":"kb-other"}`, jwtToken)
	if code != 404 {
		t.Fatalf("kb 越权应 404，实际 %d %s", code, respBody)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.hits != 0 {
		t.Errorf("越权请求不应转发上游，实际调用 %d 次", cap.hits)
	}
}

// POST /eval/tasks：登录用户提交自己的知识库 → 通过校验并透传
func TestEvalProxyTaskKBOwned(t *testing.T) {
	up, cap := newUpstream(t)
	env, srv := newEvalProxyTestEnv(t, up.URL)

	userID := "user-1"
	env.store.kbs["kb-mine"] = store.KnowledgeBase{ID: "kb-mine", Name: "我的库", OwnerID: &userID}
	jwtToken := issueTestJWT(t, env, userID)

	code, respBody := doProxyReq(t, srv, "POST", "/api/v1/eval/tasks", `{"kb_id":"kb-mine"}`, jwtToken)
	if code != 200 {
		t.Fatalf("自有知识库提交应透传成功，实际 %d %s", code, respBody)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.hits != 1 {
		t.Errorf("上游应被调用 1 次，实际 %d", cap.hits)
	}
}

// GET /eval/health 豁免鉴权：无 Authorization 头也可达上游
func TestEvalProxyHealthNoAuth(t *testing.T) {
	up, cap := newUpstream(t)
	_, srv := newEvalProxyTestEnv(t, up.URL)

	code, respBody := doProxyReq(t, srv, "GET", "/api/v1/eval/health", "", "")
	if code != 200 {
		t.Fatalf("health 豁免鉴权应 200，实际 %d %s", code, respBody)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.hits != 1 || cap.path != "/api/v1/eval/health" {
		t.Errorf("health 应透传到上游: hits=%d path=%s", cap.hits, cap.path)
	}
}

// 其余 eval 端点仍需鉴权：无 token → 401（不转发上游）
func TestEvalProxyAuthRequired(t *testing.T) {
	up, cap := newUpstream(t)
	_, srv := newEvalProxyTestEnv(t, up.URL)

	code, respBody := doProxyReq(t, srv, "GET", "/api/v1/eval/tasks", "", "")
	if code != 401 {
		t.Fatalf("无凭据应 401，实际 %d %s", code, respBody)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.hits != 0 {
		t.Errorf("未认证请求不应转发上游，实际调用 %d 次", cap.hits)
	}
}

// 上游不可用 → 502 统一包装
func TestEvalProxyUpstreamDown(t *testing.T) {
	up, _ := newUpstream(t)
	up.Close() // 立即关闭，模拟上游不可用
	_, srv := newEvalProxyTestEnv(t, up.URL)

	code, respBody := doProxyReq(t, srv, "GET", "/api/v1/eval/datasets", "", testAPIKey)
	if code != 502 {
		t.Fatalf("上游不可用应 502，实际 %d %s", code, respBody)
	}
	if !strings.Contains(string(respBody), "评测服务不可用") {
		t.Errorf("错误信息应含「评测服务不可用」: %s", respBody)
	}
}

// POST /eval/tasks body 非 JSON：代理层不拦截，原样透传（由 Python 侧返回 400/422）
func TestEvalProxyTaskNonJSONBodyPassThrough(t *testing.T) {
	up, cap := newUpstream(t)
	_, srv := newEvalProxyTestEnv(t, up.URL)

	code, respBody := doProxyReq(t, srv, "POST", "/api/v1/eval/tasks", "not-a-json", testAPIKey)
	if code != 200 {
		t.Fatalf("非 JSON body 应原样透传，实际 %d %s", code, respBody)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.body != "not-a-json" {
		t.Errorf("非 JSON body 应原样透传: %q", cap.body)
	}
}
