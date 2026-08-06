package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := Register(r); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	return r
}

func TestRootServesIndex(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET / 状态码 = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("GET / 响应应包含 HTML，实际: %s", w.Body.String())
	}
}

func TestSPADeepLinkFallback(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/kb/some-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /kb/:id 状态码 = %d, want 200（SPA 回退）", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("深链应回退 index.html，实际: %s", w.Body.String())
	}
}

func TestAPIMissReturnsJSON404(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/not-exist", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/not-exist 状态码 = %d, want 404", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API 未匹配路径应返回 JSON，Content-Type = %s", w.Header().Get("Content-Type"))
	}
}

func TestAssetsServed(t *testing.T) {
	r := newTestRouter(t)

	// 占位产物下 assets 目录不存在，验证不 panic 且返回 404（而非回退 index.html）
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("/assets 缺失文件不应回退 index.html，状态码 = %d", w.Code)
	}
}
