package api

import (
	"testing"

	"github.com/Bin-hy/bin-rag/internal/rag"
)

// include_contexts=true（query）透传到 engine AskOption
func TestChatIncludeContextsPassThrough(t *testing.T) {
	env := newTestEnv(t)

	w := doReq(t, env.router, "POST", "/api/v1/chat?include_contexts=true",
		map[string]string{"session_id": "s1", "question": "问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("状态码错误: %d %s", w.Code, w.Body.String())
	}

	env.engine.mu.Lock()
	var o rag.AskOptions
	for _, opt := range env.engine.lastAskOpts {
		opt(&o)
	}
	env.engine.mu.Unlock()
	if !o.IncludeContexts {
		t.Errorf("include_contexts=true 未透传到 engine（o.IncludeContexts=%v）", o.IncludeContexts)
	}
}

// 默认不传 include_contexts → 不启用（默认路径零影响）
func TestChatIncludeContextsDefaultOff(t *testing.T) {
	env := newTestEnv(t)

	w := doReq(t, env.router, "POST", "/api/v1/chat",
		map[string]string{"session_id": "s1", "question": "问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("状态码错误: %d %s", w.Code, w.Body.String())
	}

	env.engine.mu.Lock()
	var o rag.AskOptions
	for _, opt := range env.engine.lastAskOpts {
		opt(&o)
	}
	env.engine.mu.Unlock()
	if o.IncludeContexts {
		t.Errorf("默认不应启用 include_contexts（o.IncludeContexts=%v）", o.IncludeContexts)
	}
}

// include_contexts=true 时响应 Source 含 content 字段（fake engine 返回带正文的来源）
func TestChatIncludeContextsResponseHasContent(t *testing.T) {
	env := newTestEnv(t)
	env.engine.sources = []rag.Source{{ID: "r1", Filename: "a.md", Score: 0.9, Content: "片段正文"}}

	w := doReq(t, env.router, "POST", "/api/v1/chat?include_contexts=true",
		map[string]string{"session_id": "s1", "question": "问题"}, testAPIKey)
	if w.Code != 200 {
		t.Fatalf("状态码错误: %d %s", w.Code, w.Body.String())
	}

	resp := decodeResp(t, w)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("响应数据错误: %+v", resp.Data)
	}
	sources, ok := data["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("引用来源错误: %+v", data["sources"])
	}
	first, ok := sources[0].(map[string]any)
	if !ok || first["content"] != "片段正文" {
		t.Errorf("include_contexts=true 时 Source 应含 content: %+v", sources[0])
	}
}
