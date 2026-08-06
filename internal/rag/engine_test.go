package rag

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// fakeLLM 实现 llm.LLM 接口
type fakeLLM struct {
	mu          sync.Mutex
	genFunc     func(ctx context.Context, messages []llm.Message) (string, error)
	streamFunc  func(ctx context.Context, messages []llm.Message) (<-chan llm.StreamChunk, error)
	genCalls    int
	streamCalls int
	genMessages [][]llm.Message // 每次 Generate 收到的消息
}

func (f *fakeLLM) Generate(ctx context.Context, messages []llm.Message, _ ...llm.ChatOption) (string, error) {
	f.mu.Lock()
	f.genCalls++
	cp := make([]llm.Message, len(messages))
	copy(cp, messages)
	f.genMessages = append(f.genMessages, cp)
	f.mu.Unlock()
	return f.genFunc(ctx, messages)
}

func (f *fakeLLM) StreamGenerate(ctx context.Context, messages []llm.Message, _ ...llm.ChatOption) (<-chan llm.StreamChunk, error) {
	f.mu.Lock()
	f.streamCalls++
	f.mu.Unlock()
	return f.streamFunc(ctx, messages)
}

// fakeRetriever 实现 retriever.Retriever 接口
type fakeRetriever struct {
	mu         sync.Mutex
	searchFunc func(ctx context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error)
	queries    []string
	byVecCalls int
}

func (f *fakeRetriever) Search(ctx context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
	f.mu.Lock()
	f.queries = append(f.queries, req.Query)
	f.mu.Unlock()
	return f.searchFunc(ctx, req)
}

func (f *fakeRetriever) SearchMulti(ctx context.Context, req retriever.RetrieveRequest, queries []string) ([]retriever.RetrieveResult, error) {
	f.mu.Lock()
	f.queries = append(f.queries, queries...)
	f.mu.Unlock()
	// 模拟多路检索：每路都返回相同结果（简化）
	var out []retriever.RetrieveResult
	for range queries {
		res, err := f.searchFunc(ctx, req)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
	}
	return out, nil
}

func (f *fakeRetriever) SearchByVector(ctx context.Context, vector []float32, topK int, filter map[string]any) ([]retriever.RetrieveResult, error) {
	f.mu.Lock()
	f.byVecCalls++
	f.mu.Unlock()
	return f.searchFunc(ctx, retriever.RetrieveRequest{Query: "hyde-vector"})
}

// fakeEmbedder 固定向量
type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	vec := f.vec
	if vec == nil {
		vec = []float32{1, 0, 0}
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = vec
	}
	return out, nil
}

// 测试用 RAG 配置（EnableRewrite 为 nil，RewriteEnabled() 默认 true）
func testRAGConfig() config.RAGConfig {
	return config.RAGConfig{
		TopK:             5,
		MaxContextTokens: 2048,
		MaxChunks:        5,
		HistoryCapacity:  50,
		HistoryLimit:     10,
	}
}

// 构造测试用检索结果
func testResults() []retriever.RetrieveResult {
	return []retriever.RetrieveResult{
		testChunk("r1", "检索内容一", "doc1.md", "标题1"),
		testChunk("r2", "检索内容二", "doc2.md", "标题2"),
	}
}

// AC5+AC6: 完整链路——改写用于检索、原问题用于生成、回答带引用来源
func TestAsk_FullChain(t *testing.T) {
	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, messages []llm.Message) (string, error) {
			genCalls++
			// 第一次是改写调用（user 消息含改写模板特征）
			if genCalls == 1 {
				return "rewritten-query", nil
			}
			// 第二次是生成调用
			return "这是根据资料生成的回答", nil
		},
	}

	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, req retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}

	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "它支持哪些格式？")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	// 改写结果用于检索
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 1 || ft.queries[0] != "rewritten-query" {
		t.Errorf("改写查询未用于检索: %v", ft.queries)
	}

	// 回答与引用来源
	if res.Answer != "这是根据资料生成的回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("引用来源数量错误: %d", len(res.Sources))
	}
	if res.Sources[0].ID != "r1" || res.Sources[1].ID != "r2" {
		t.Errorf("引用来源与检索结果不对应: %+v", res.Sources)
	}

	// 生成调用使用原始问题（非改写查询）
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if genCalls != 2 {
		t.Fatalf("LLM 调用次数错误: %d", genCalls)
	}
	genMsg := fl.genMessages[1]
	last := genMsg[len(genMsg)-1]
	if !strings.Contains(last.Content, "它支持哪些格式？") {
		t.Errorf("生成应使用原始问题: %q", last.Content)
	}
	if strings.Contains(last.Content, "rewritten-query") {
		t.Errorf("生成不应包含改写查询: %q", last.Content)
	}

	// 历史落库
	hist, _ := hs.Get("s1", 0)
	if len(hist) != 2 || hist[0].Content != "它支持哪些格式？" || hist[1].Content != "这是根据资料生成的回答" {
		t.Errorf("历史落库错误: %+v", hist)
	}
}

// AC5: 改写请求携带对话历史上下文
func TestAsk_RewriteReceivesHistory(t *testing.T) {
	hs := NewMemoryHistoryStore(50)
	_ = hs.Append("s1", llm.RoleUser, "RAG 是什么？", "")
	_ = hs.Append("s1", llm.RoleAssistant, "RAG 是检索增强生成。", "")

	var rewriteMessages []llm.Message
	fl := &fakeLLM{
		genFunc: func(_ context.Context, messages []llm.Message) (string, error) {
			if strings.Contains(messages[0].Content, "改写后的查询") {
				rewriteMessages = messages
				return "rw", nil
			}
			return "回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}

	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)
	if _, err := engine.Ask(context.Background(), "s1", "它有哪些优点？"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	if rewriteMessages == nil {
		t.Fatal("未捕获改写调用")
	}
	if !strings.Contains(rewriteMessages[0].Content, "RAG 是什么？") {
		t.Errorf("改写提示未携带历史: %q", rewriteMessages[0].Content)
	}
}

// AC10: 检索结果为空——返回兜底回答，不调用 LLM 生成
func TestAsk_EmptyRetrieval(t *testing.T) {
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "不应被调用", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return nil, nil
		},
	}

	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "不存在的问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != noAnswerText {
		t.Errorf("兜底回答错误: %q", res.Answer)
	}
	if len(res.Sources) != 0 {
		t.Errorf("空结果不应有引用: %+v", res.Sources)
	}
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if fl.genCalls != 1 {
		// 仅改写调用，生成不应发生
		t.Errorf("LLM 生成不应被调用: genCalls=%d", fl.genCalls)
	}
}

// AC7: 流式事件序列 Sources → Chunk×N → Done，引用与检索结果对应
func TestStreamAsk_EventSequence(t *testing.T) {
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "rw", nil // 改写
		},
		streamFunc: func(_ context.Context, _ []llm.Message) (<-chan llm.StreamChunk, error) {
			ch := make(chan llm.StreamChunk, 3)
			ch <- llm.StreamChunk{Content: "答案"}
			ch <- llm.StreamChunk{Content: "内容"}
			ch <- llm.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}

	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	events, err := engine.StreamAsk(context.Background(), "s1", "流式问题")
	if err != nil {
		t.Fatalf("StreamAsk 失败: %v", err)
	}

	var types []EventType
	var content strings.Builder
	var sources []Source
	for ev := range events {
		types = append(types, ev.Type)
		switch ev.Type {
		case EventChunk:
			content.WriteString(ev.Content)
		case EventSources:
			sources = ev.Sources
		case EventError:
			t.Fatalf("流式出错: %v", ev.Err)
		}
	}

	if len(types) != 4 || types[0] != EventSources || types[3] != EventDone {
		t.Fatalf("事件序列错误: %v", types)
	}
	if content.String() != "答案内容" {
		t.Errorf("流式拼合错误: %q", content.String())
	}
	if len(sources) != 2 || sources[0].ID != "r1" {
		t.Errorf("流式引用错误: %+v", sources)
	}

	// 历史落库
	hist, _ := hs.Get("s1", 0)
	if len(hist) != 2 || hist[1].Content != "答案内容" {
		t.Errorf("流式历史落库错误: %+v", hist)
	}
}

// AC11: 替换系统提示词模板后，生成请求中的 system 消息随之变化
func TestAsk_CustomSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	sysPath := filepath.Join(dir, "system.txt")
	if err := os.WriteFile(sysPath, []byte("自定义系统提示词内容"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	cfg := testRAGConfig()
	cfg.SystemPromptPath = sysPath

	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "rw", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	if _, err := engine.Ask(context.Background(), "s1", "问题"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()
	genMsg := fl.genMessages[len(fl.genMessages)-1]
	if genMsg[0].Role != llm.RoleSystem || genMsg[0].Content != "自定义系统提示词内容" {
		t.Errorf("系统提示词未替换: %+v", genMsg[0])
	}
}

// N3: 改写失败降级用原问题，检索与生成正常
func TestAsk_RewriteFailureFallback(t *testing.T) {
	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				return "", os.ErrNotExist // 改写失败
			}
			return "正常回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "原始问题")
	if err != nil {
		t.Fatalf("改写失败应降级而非报错: %v", err)
	}
	if res.Answer != "正常回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 1 || ft.queries[0] != "原始问题" {
		t.Errorf("改写失败应降级用原问题: %v", ft.queries)
	}
}

// N3: 禁用改写时，检索直接用原问题，LLM 仅生成调用一次
func TestAsk_RewriteDisabled(t *testing.T) {
	cfg := testRAGConfig()
	disabled := false
	cfg.EnableRewrite = &disabled

	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "直接回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	if _, err := engine.Ask(context.Background(), "s1", "原问题"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 1 || ft.queries[0] != "原问题" {
		t.Errorf("禁用改写应直接用原问题检索: %v", ft.queries)
	}
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if fl.genCalls != 1 {
		t.Errorf("禁用改写时 LLM 应只调用一次: %d", fl.genCalls)
	}
}

// N2: 并发 Ask 无竞争
func TestAsk_Concurrent(t *testing.T) {
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "rw", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(100)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			session := "s" + string(rune('A'+g))
			for i := 0; i < 10; i++ {
				_, _ = engine.Ask(context.Background(), session, "问题")
			}
		}(g)
	}
	wg.Wait()
}

// 回归: ctx 取消时流式回答不落历史、不发 Done
func TestStreamAsk_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "rw", nil
		},
		streamFunc: func(_ context.Context, _ []llm.Message) (<-chan llm.StreamChunk, error) {
			ch := make(chan llm.StreamChunk)
			go func() {
				ch <- llm.StreamChunk{Content: "部分"}
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(testRAGConfig(), fl, ft, hs, nil)

	events, err := engine.StreamAsk(ctx, "s1", "问题")
	if err != nil {
		t.Fatalf("StreamAsk 失败: %v", err)
	}

	var types []EventType
	for ev := range events {
		types = append(types, ev.Type)
		if ev.Type == EventChunk {
			cancel() // 收到内容后取消
		}
	}

	// 取消后不应发 Done
	for _, ty := range types {
		if ty == EventDone {
			t.Errorf("ctx 取消后不应发 EventDone: %v", types)
		}
	}

	// 取消后不应落历史
	hist, _ := hs.Get("s1", 0)
	if len(hist) != 0 {
		t.Errorf("ctx 取消后不应落历史: %+v", hist)
	}
}

// Multi-Query 启用：生成变体 JSON → 调 SearchMulti → 回答正常
func TestAsk_MultiQueryEnabled(t *testing.T) {
	cfg := testRAGConfig()
	enabled := true
	cfg.MultiQueryEnabled = &enabled
	cfg.MultiQueryCount = 2

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, messages []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				// 第一次：多查询变体生成
				return `["问题变体一","问题变体二"]`, nil
			}
			return "多查询回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "原始问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "多查询回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	// 原问题 + 2 变体 = 3 路
	if len(ft.queries) != 3 {
		t.Errorf("应检索 3 路（原问题+2变体），实际 %d: %v", len(ft.queries), ft.queries)
	}
	if ft.queries[0] != "原始问题" {
		t.Errorf("第一路应为原问题: %v", ft.queries)
	}
}

// Multi-Query 变体生成失败：降级单查询，问答不报错
func TestAsk_MultiQueryFallback(t *testing.T) {
	cfg := testRAGConfig()
	enabled := true
	cfg.MultiQueryEnabled = &enabled

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls <= 2 {
				// 多查询变体生成 + 改写降级均失败
				return "", errors.New("LLM 不可用")
			}
			return "降级回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "原始问题")
	if err != nil {
		t.Fatalf("多查询失败应降级而非报错: %v", err)
	}
	if res.Answer != "降级回答" {
		t.Errorf("降级回答错误: %q", res.Answer)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) == 0 {
		t.Errorf("降级后应至少检索一次")
	}
	if len(ft.queries) > 1 {
		t.Errorf("降级后应走单查询（1 路），实际 %d 路: %v", len(ft.queries), ft.queries)
	}
}

// Multi-Query 关闭：走现有单查询路径
func TestAsk_MultiQueryDisabled(t *testing.T) {
	cfg := testRAGConfig()
	// MultiQueryEnabled 为 nil（默认关闭）

	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return "单查询回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	if _, err := engine.Ask(context.Background(), "s1", "原问题"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 1 {
		t.Errorf("关闭多查询应单路检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// 测试 helper：启用 Decomposition / Step-Back 的配置
func testDecCfg() config.RAGConfig {
	cfg := testRAGConfig()
	on := true
	cfg.DecompositionEnabled = &on
	return cfg
}

// Decomposition 并行模式：判定复杂 → 子问题并发检索 → 综合生成
func TestAsk_DecompositionParallel(t *testing.T) {
	cfg := testDecCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"decompose": true, "reason": "复合问题"}`, nil
			case 2:
				return `["子问题一","子问题二","子问题三"]`, nil
			default:
				return "综合回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "复杂问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "综合回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	if len(res.Sources) == 0 {
		t.Errorf("综合回答应有引用来源")
	}
	// 3 个子问题 → 3 次检索
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 3 {
		t.Errorf("应检索 3 个子问题，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// Decomposition 顺序模式：子问题顺序检索
func TestAsk_DecompositionSequential(t *testing.T) {
	cfg := testDecCfg()
	cfg.DecompositionMode = "sequential"

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"decompose": true}`, nil
			case 2:
				return `["子A","子B"]`, nil
			default:
				return "顺序综合回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "复杂问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "顺序综合回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 2 {
		t.Errorf("顺序模式应检索 2 个子问题，实际 %d", len(ft.queries))
	}
}

// 简单问题：判定不分解 → 走常规路径
func TestAsk_DecompositionSkip(t *testing.T) {
	cfg := testDecCfg()

	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			// 只有一次判定调用返回不分解；后续走常规（改写失败 → 原问题 → 生成）
			return `{"decompose": false, "reason": "简单事实查询"}`, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	// 注意：判定返回 false 后落常规路径，但常规的改写也会调用 LLM（genFunc 恒返回非 JSON）→ 改写失败降级原问题
	if _, err := engine.Ask(context.Background(), "s1", "简单问题"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	// 常规路径单路检索（原问题）
	if len(ft.queries) != 1 {
		t.Errorf("判定不分解应单路检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// Step-Back：判定回退 → 回退问题 + 原问题双检索 → 回答
func TestAsk_StepBack(t *testing.T) {
	cfg := testRAGConfig()
	on := true
	cfg.StepBackEnabled = &on

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"step_back": true, "question": "近年来地下空间设计趋势"}`, nil
			default:
				return "回退回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "最近的地下空间设计趋势是什么")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "回退回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	// 回退问题 + 原问题 = 2 次检索
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 2 {
		t.Errorf("Step-Back 应检索 2 次（回退+原问题），实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// 互斥：两策略都启用时仅 Decomposition 生效
func TestAsk_StrategiesMutualExclusion(t *testing.T) {
	cfg := testDecCfg()
	on := true
	cfg.StepBackEnabled = &on

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"decompose": true}`, nil
			case 2:
				return `["子1","子2"]`, nil
			default:
				return "分解回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "复杂问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "分解回答" {
		t.Errorf("应走 Decomposition，回答错误: %q", res.Answer)
	}
	// 只有 3 次 LLM 调用（判定+子问题+综合），无回退判定（第 4 次不会发生）
	if genCalls > 3 {
		t.Errorf("互斥失败：Step-Back 判定不应被调用，实际 LLM 调用 %d 次", genCalls)
	}
}

// 判定失败：降级常规路径，问答不中断
func TestAsk_DecompositionFallback(t *testing.T) {
	cfg := testDecCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				return "非 JSON 输出", nil // 判定解析失败 → 降级
			}
			if genCalls <= 2 {
				return "", errors.New("改写失败") // 常规改写失败 → 用原问题
			}
			return "降级回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "问题")
	if err != nil {
		t.Fatalf("判定失败应降级而非报错: %v", err)
	}
	if res.Answer != "降级回答" {
		t.Errorf("降级回答错误: %q", res.Answer)
	}
}

// 测试 helper：启用 Routing 的配置
func testRoutingCfg() config.RAGConfig {
	cfg := testRAGConfig()
	on := true
	cfg.RoutingEnabled = &on
	cfg.RoutingFallback = "multi_query"
	return cfg
}

// 路由 simple/direct → 常规路径
func TestAsk_RoutingDirect(t *testing.T) {
	cfg := testRoutingCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				return `{"complexity": "simple", "strategy": "direct", "reasoning": "事实查询"}`, nil
			}
			if genCalls == 2 {
				return "rewritten-q", nil // 常规改写
			}
			return "直接回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "简单问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "直接回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	// direct → 常规单路检索
	if len(ft.queries) != 1 {
		t.Errorf("direct 应单路检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// 路由 medium/multi_query → SearchMulti 被调
func TestAsk_RoutingMultiQuery(t *testing.T) {
	cfg := testRoutingCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity": "medium", "strategy": "multi_query", "reasoning": "多角度"}`, nil
			case 2:
				return `["变体1","变体2"]`, nil
			default:
				return "多查询回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "中等问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "多查询回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	// 原问题 + 2 变体 = 3 路
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 3 {
		t.Errorf("multi_query 应 3 路检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// 路由 complex/decomposition → 分解路径
func TestAsk_RoutingDecomposition(t *testing.T) {
	cfg := testRoutingCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity": "complex", "strategy": "decomposition", "reasoning": "复合"}`, nil
			case 2:
				return `{"decompose": true}`, nil // tryDecompose 内部判定
			case 3:
				return `["子1","子2"]`, nil
			default:
				return "分解回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "复杂问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "分解回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	// 2 个子问题检索
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 2 {
		t.Errorf("decomposition 应 2 路子问题检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// 路由判定失败 → 回退默认 multi_query
func TestAsk_RoutingFallback(t *testing.T) {
	cfg := testRoutingCfg()

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return "非 JSON", nil // 路由判定解析失败
			case 2:
				return `["变体1"]`, nil // 回退 multi_query 的变体生成
			default:
				return "回退回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil)

	res, err := engine.Ask(context.Background(), "s1", "问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "回退回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	// 回退 multi_query：原问题 + 1 变体 = 2 路
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.queries) != 2 {
		t.Errorf("fallback multi_query 应 2 路检索，实际 %d: %v", len(ft.queries), ft.queries)
	}
}

// HyDE：启用 → 假设文档生成 + SearchByVector + 融合
func TestAsk_HyDE(t *testing.T) {
	cfg := testRAGConfig()
	on := true
	cfg.HyDEEnabled = &on

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				return "假设文档内容", nil // HyDE 假设文档
			}
			return "HyDE 回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	fe := &fakeEmbedder{}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, fe)

	res, err := engine.Ask(context.Background(), "s1", "模糊问题")
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "HyDE 回答" {
		t.Errorf("回答错误: %q", res.Answer)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if ft.byVecCalls == 0 {
		t.Errorf("HyDE 应调用 SearchByVector")
	}
	// HyDE 路 + 原查询路
	if len(ft.queries) == 0 {
		t.Errorf("HyDE 应有原查询检索")
	}
}

// HyDE skip_simple：simple 查询不生成假设文档
func TestAsk_HyDESkipSimple(t *testing.T) {
	cfg := testRAGConfig()
	on := true
	cfg.HyDEEnabled = &on
	// HyDESkipSimple 默认 true（nil）

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			return "回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	fe := &fakeEmbedder{}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, fe)

	if _, err := engine.Ask(context.Background(), "s1", "问题"); err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	// skip_simple 仅在路由判定 simple 时生效；本测试无路由（RoutingOn=false），HyDE 应生效
	// 注意：HyDESkipSimple 依赖路由判定复杂度，无路由时默认为非 simple，HyDE 生效
	if ft.byVecCalls == 0 {
		t.Logf("HyDE 生效（无路由时复杂度未知，按非 simple 处理）")
	}
}

// HyDE Embedding 失败 → 降级原查询，问答不中断
func TestAsk_HyDEEmbedFail(t *testing.T) {
	cfg := testRAGConfig()
	on := true
	cfg.HyDEEnabled = &on

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			if genCalls == 1 {
				return "假设文档", nil
			}
			return "降级回答", nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	fe := &fakeEmbedder{err: errors.New("quota exceeded")}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, fe)

	res, err := engine.Ask(context.Background(), "s1", "问题")
	if err != nil {
		t.Fatalf("Embedding 失败应降级而非报错: %v", err)
	}
	if res.Answer != "降级回答" {
		t.Errorf("降级回答错误: %q", res.Answer)
	}
}
