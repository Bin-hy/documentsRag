package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
)

// 构造测试用配置
func testConfig(baseURL string) config.LLMConfig {
	return config.LLMConfig{
		BaseURL:     baseURL,
		APIKey:      "test-key",
		Model:       "test-model",
		Temperature: 0.7,
		MaxTokens:   2048,
		MaxRetries:  3,
		QPS:         100,
		Timeout:     10,
	}
}

// decodeRequest 解析 chat/completions 请求体
func decodeRequest(t *testing.T, r *http.Request) chatCompletionRequest {
	t.Helper()
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	return req
}

func generateOKHandler(t *testing.T, content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("URL 路径错误: %s", r.URL.Path)
		}
		req := decodeRequest(t, r)
		if req.Stream {
			t.Errorf("普通生成请求不应带 stream=true")
		}
		if req.Model != "test-model" {
			t.Errorf("model 错误: %s", req.Model)
		}
		if req.Temperature != 0.7 {
			t.Errorf("temperature 错误: %v", req.Temperature)
		}
		if req.MaxTokens != 2048 {
			t.Errorf("max_tokens 错误: %d", req.MaxTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, content)
	}
}

// T4-AC1: 普通生成返回完整文本；配置生效（model/temperature/max_tokens 断言在 handler 内）
func TestGenerate_Success(t *testing.T) {
	srv := httptest.NewServer(generateOKHandler(t, "你好，世界"))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	got, err := l.Generate(context.Background(), []Message{{Role: RoleUser, Content: "你好"}})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if got != "你好，世界" {
		t.Errorf("生成内容错误: %q", got)
	}
}

// T4-AC1: 消息列表正确传递（system/user/assistant 多轮）
func TestGenerate_MessagesPassed(t *testing.T) {
	want := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleUser, Content: "u2"},
	}
	var mu sync.Mutex
	var gotMessages []Message

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		mu.Lock()
		gotMessages = req.Messages
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	if _, err := l.Generate(context.Background(), want); err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotMessages) != len(want) {
		t.Fatalf("消息数量错误: got %d want %d", len(gotMessages), len(want))
	}
	for i := range want {
		if gotMessages[i].Role != want[i].Role || gotMessages[i].Content != want[i].Content ||
			gotMessages[i].Sources != want[i].Sources || gotMessages[i].ToolCallID != want[i].ToolCallID ||
			len(gotMessages[i].ToolCalls) != len(want[i].ToolCalls) {
			t.Errorf("消息[%d]错误: got %+v want %+v", i, gotMessages[i], want[i])
		}
	}
}

// T4-AC3: ChatOption 覆盖 model / temperature / max_tokens
func TestGenerate_OptionsOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if req.Model != "override-model" {
			t.Errorf("model 覆盖失败: %s", req.Model)
		}
		if req.Temperature != 0.1 {
			t.Errorf("temperature 覆盖失败: %v", req.Temperature)
		}
		if req.MaxTokens != 512 {
			t.Errorf("max_tokens 覆盖失败: %d", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	_, err := l.Generate(context.Background(),
		[]Message{{Role: RoleUser, Content: "q"}},
		WithModel("override-model"),
		WithTemperature(0.1),
		WithMaxTokens(512),
	)
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
}

// T4-AC2: 流式增量拼合与普通生成一致
func TestStreamGenerate_ChunksAssemble(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if !req.Stream {
			t.Errorf("流式请求应带 stream=true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"，\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\n"+
				"data: [DONE]\n\n",
		)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	ch, err := l.StreamGenerate(context.Background(), []Message{{Role: RoleUser, Content: "q"}})
	if err != nil {
		t.Fatalf("StreamGenerate 失败: %v", err)
	}

	var sb strings.Builder
	var done bool
	var chunkErr error
	for c := range ch {
		if c.Err != nil {
			chunkErr = c.Err
			break
		}
		if c.Done {
			done = true
			continue
		}
		sb.WriteString(c.Content)
	}
	if chunkErr != nil {
		t.Fatalf("流式出错: %v", chunkErr)
	}
	if !done {
		t.Errorf("未收到 Done 片段")
	}
	if sb.String() != "你好，世界" {
		t.Errorf("流式拼合错误: %q", sb.String())
	}
}

// T4-AC4: 先 500 后成功，重试生效
func TestGenerate_RetryOn500(t *testing.T) {
	var count int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"boom"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"成功"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	got, err := l.Generate(context.Background(), []Message{{Role: RoleUser, Content: "q"}})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if got != "成功" {
		t.Errorf("内容错误: %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Errorf("重试次数错误: got %d want 3", count)
	}
}

// T4-AC4: 429 重试生效
func TestGenerate_RetryOn429(t *testing.T) {
	var count int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"rate limited"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	if _, err := l.Generate(context.Background(), []Message{{Role: RoleUser, Content: "q"}}); err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Errorf("重试次数错误: got %d want 2", count)
	}
}

// T4-AC4: 400 不重试直接失败
func TestGenerate_NoRetryOn400(t *testing.T) {
	var count int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	_, err := l.Generate(context.Background(), []Message{{Role: RoleUser, Content: "q"}})
	if err == nil {
		t.Fatal("400 应返回错误")
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("400 不应重试: got %d want 1", count)
	}
}

// T4: 无 APIKey 时省略 Authorization 头；有 APIKey 时带 Bearer
func TestGenerate_AuthHeader(t *testing.T) {
	cases := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"无 APIKey", "", false},
		{"有 APIKey", "sk-test", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hasAuth bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hasAuth = r.Header.Get("Authorization") != ""
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
			}))
			defer srv.Close()

			cfg := testConfig(srv.URL)
			cfg.APIKey = tc.apiKey
			l := NewLLM(cfg)
			if _, err := l.Generate(context.Background(), []Message{{Role: RoleUser, Content: "q"}}); err != nil {
				t.Fatalf("Generate 失败: %v", err)
			}
			if hasAuth != tc.want {
				t.Errorf("Authorization 头错误: got %v want %v", hasAuth, tc.want)
			}
		})
	}
}

// T4: 流式中段错误透传（已收到 delta 后失败，不重试）
func TestStreamGenerate_ErrorAfterFirstDelta(t *testing.T) {
	var count int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()

		// 手动接管连接：先发合法 delta，再发非法 JSON 触发解析错误
		hij, ok := w.(http.Hijacker)
		if !ok {
			t.Error("Hijacker 不可用")
			return
		}
		conn, buf, err := hij.Hijack()
		if err != nil {
			t.Errorf("Hijack 失败: %v", err)
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n")
		buf.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n")
		buf.WriteString("data: {invalid json}\n\n")
		buf.Flush()
	}))
	defer srv.Close()

	l := NewLLM(testConfig(srv.URL))
	ch, err := l.StreamGenerate(context.Background(), []Message{{Role: RoleUser, Content: "q"}})
	if err != nil {
		t.Fatalf("StreamGenerate 失败: %v", err)
	}

	var sb strings.Builder
	var gotErr error
	for c := range ch {
		if c.Err != nil {
			gotErr = c.Err
			break
		}
		sb.WriteString(c.Content)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotErr == nil {
		t.Error("中段失败应透传错误")
	}
	if !strings.Contains(sb.String(), "部分") {
		t.Errorf("应已收到首段内容: %q", sb.String())
	}
	if count != 1 {
		t.Errorf("收到 delta 后不应重试: got %d want 1", count)
	}
}

// function calling：请求体含 tools 字段，响应 tool_calls 被正确解析
func TestGenerateTool_ToolCallsParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if len(req.Tools) == 0 {
			t.Fatalf("请求体应包含 tools")
		}
		tool := req.Tools[0]
		if tool["type"] != "function" {
			t.Errorf("tool type = %v, want function", tool["type"])
		}
		fn, _ := tool["function"].(map[string]any)
		if fn["name"] != "web_search" {
			t.Errorf("tool 名称 = %v, want web_search", fn["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"RAG 召回\"}"}}]}}]}`)
	}))
	defer server.Close()

	client := NewLLM(testConfig(server.URL)).(*openaiLLM)
	resp, err := client.GenerateTool(context.Background(), []Message{{Role: RoleUser, Content: "查一下"}}, []FunctionTool{
		{Name: "web_search", Description: "联网搜索", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("GenerateTool 失败: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 数量 = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "web_search" || !strings.Contains(tc.Arguments, "RAG 召回") {
		t.Errorf("ToolCall 解析错误: %+v", tc)
	}
}

// function calling：模型未请求工具时返回正文
func TestGenerateTool_PlainContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"直接回答"}}]}`)
	}))
	defer server.Close()

	client := NewLLM(testConfig(server.URL)).(*openaiLLM)
	resp, err := client.GenerateTool(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, []FunctionTool{{Name: "web_search"}})
	if err != nil {
		t.Fatalf("GenerateTool 失败: %v", err)
	}
	if resp.Content != "直接回答" || len(resp.ToolCalls) != 0 {
		t.Errorf("resp = %+v, want content=直接回答 且无工具调用", resp)
	}
}

// 流式：delta.tool_calls 分片按 index 聚合，Done 片段携带完整 ToolCalls
func TestStreamGenerate_ToolCallsAggregated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := "" +
			"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_9\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"query\\\":\\\"RAG\\\"\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]}}]}\n\n" +
			"data: [DONE]\n\n"
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	client := NewLLM(testConfig(server.URL)).(*openaiLLM)
	ch, err := client.StreamGenerate(context.Background(), []Message{{Role: RoleUser, Content: "q"}})
	if err != nil {
		t.Fatalf("StreamGenerate 失败: %v", err)
	}

	var done StreamChunk
	for c := range ch {
		if c.Done {
			done = c
		}
	}
	if len(done.ToolCalls) != 1 {
		t.Fatalf("Done 片段 ToolCalls = %+v, want 1 条", done.ToolCalls)
	}
	tc := done.ToolCalls[0]
	if tc.ID != "call_9" || tc.Name != "web_search" || tc.Arguments != `{"query":"RAG"}` {
		t.Errorf("流式 ToolCall 聚合错误: %+v", tc)
	}
}
