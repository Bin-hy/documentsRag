package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/search"
)

// fakeSearchProvider 测试用搜索提供者
type fakeSearchProvider struct {
	query     string
	available bool
}

func (p *fakeSearchProvider) Name() string    { return "bocha" }
func (p *fakeSearchProvider) Available() bool { return p.available }
func (p *fakeSearchProvider) Search(ctx context.Context, query string, opts search.Options) ([]search.Result, error) {
	p.query = query
	return []search.Result{
		{Title: "RAG 召回详解", URL: "https://example.com/rag", Snippet: "RAG 召回是检索增强生成的核心环节", Content: "RAG 召回通过向量检索与 BM25 融合实现。"},
	}, nil
}

// 增强模式：LLM 请求 web_search 工具 → 工具执行 → 结果回传 → 最终回答
func TestAsk_EnhancedToolLoop(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on"}

	var genCalls int
	var toolRounds int // 工具循环轮次计数（闭包）
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"vector_store","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "不应直接生成（应走工具循环）", nil
			}
		},
		toolFunc: func(ctx context.Context, messages []llm.Message, tools []llm.FunctionTool) (*llm.ToolResponse, error) {
			// 断言工具使用引导已注入（第一条 system 含 web_search 指引）
			if len(messages) == 0 || messages[0].Role != llm.RoleSystem || !strings.Contains(messages[0].Content, "web_search") {
				t.Errorf("增强模式应注入工具引导 system 消息, messages[0]=%+v", messages[0])
			}
			// 第一轮请求 web_search；后续轮返回最终回答（轮次计数，不受系统注入的搜索结果干扰）
			toolRounds++
			if toolRounds == 1 {
				return &llm.ToolResponse{ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: "web_search", Arguments: `{"query":"RAG 召回"}`},
				}}, nil
			}
			return &llm.ToolResponse{Content: "基于搜索结果：RAG 召回是核心环节"}, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	web := &fakeSearchProvider{available: true}

	engine := NewEngine(cfg, fl, ft, NewMemoryHistoryStore(50), nil, WithSearchProvider(web)).(*RAGEngine)

	// web_search 工具已注册
	if _, ok := engine.tools.Get("web_search"); !ok {
		t.Fatalf("web_search 工具未注册")
	}

	res, err := engine.Ask(context.Background(), "s1", "什么是RAG召回", WithThinking(true), WithEnhanced(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "基于搜索结果：RAG 召回是核心环节" {
		t.Errorf("Answer = %q", res.Answer)
	}

	// 思考链路含工具调用步骤
	var toolStep *ThinkingStep
	for i := range res.Thinking {
		if res.Thinking[i].Type == StepTool {
			toolStep = &res.Thinking[i]
		}
	}
	if toolStep == nil {
		t.Fatal("思考链路缺少 StepTool")
	}
	data, ok := toolStep.Data.(ToolStepData)
	if !ok || data.Name != "web_search" || data.Result == "" {
		t.Errorf("ToolStepData 错误: %+v", toolStep.Data)
	}
	// 结构化搜索结果进入思考链（标题/链接/摘要完整展示）
	if len(data.Items) != 1 {
		t.Errorf("ToolStepData.Items 应含搜索结果条目: %+v", data.Items)
	} else if data.Items[0].Title != "RAG 召回详解" || data.Items[0].URL != "https://example.com/rag" {
		t.Errorf("Items 内容错误: %+v", data.Items[0])
	}
}

// 增强模式：未注入搜索提供者（或不可用）→ 无工具，行为与普通模式一致
func TestAsk_EnhancedNoToolsFallback(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"vector_store","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "普通回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}

	engine := NewEngine(cfg, fl, ft, NewMemoryHistoryStore(50), nil)
	res, err := engine.Ask(context.Background(), "s1", "问题", WithEnhanced(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "普通回答" {
		t.Errorf("无工具时增强模式应回退普通生成: %q", res.Answer)
	}
}

// 增强模式：LLM 请求未知工具名 → 错误结果回传 → 继续循环拿到回答
func TestAsk_EnhancedUnknownTool(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on"}

	var toolRounds int // 工具循环轮次计数（闭包）
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			return `{"complexity":"simple","strategy":"direct","data_source":"vector_store","reasoning":"测试"}`, nil
		},
		toolFunc: func(ctx context.Context, messages []llm.Message, tools []llm.FunctionTool) (*llm.ToolResponse, error) {
			// 第一轮请求未知工具；后续轮返回兜底回答（轮次计数，不受系统注入的搜索结果干扰）
			toolRounds++
			if toolRounds == 1 {
				return &llm.ToolResponse{ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "unknown_tool", Arguments: `{}`},
				}}, nil
			}
			return &llm.ToolResponse{Content: "兜底回答"}, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}

	// 注入可用搜索提供者使 tools 非空（否则 runToolLoop 直接跳过）
	engine := NewEngine(cfg, fl, ft, NewMemoryHistoryStore(50), nil,
		WithSearchProvider(&fakeSearchProvider{available: true}))
	res, err := engine.Ask(context.Background(), "s1", "问题", WithEnhanced(true), WithThinking(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "兜底回答" {
		t.Errorf("未知工具后应继续循环拿到回答: %q", res.Answer)
	}
	// 思考链路应记录工具失败
	var hasToolErr bool
	for _, st := range res.Thinking {
		if st.Type == StepTool {
			if d, ok := st.Data.(ToolStepData); ok && d.Error != "" {
				hasToolErr = true
			}
		}
	}
	if !hasToolErr {
		t.Errorf("思考链路应记录未知工具错误")
	}
}

// StreamAsk 增强：无可用工具时回退普通流式，不把 user 消息（上下文+问题）当回答发出（审查 blocking 修复）
func TestStreamAsk_EnhancedNoToolsFallsBackToStream(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"vector_store","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "不应直接生成", nil
			}
		},
		streamFunc: func(_ context.Context, _ []llm.Message) (<-chan llm.StreamChunk, error) {
			ch := make(chan llm.StreamChunk)
			go func() {
				ch <- llm.StreamChunk{Content: "流式回答内容"}
				ch <- llm.StreamChunk{Done: true}
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

	engine := NewEngine(cfg, fl, ft, NewMemoryHistoryStore(50), nil) // 未注入 provider → 无工具

	events, err := engine.StreamAsk(context.Background(), "s1", "问题", WithEnhanced(true))
	if err != nil {
		t.Fatalf("StreamAsk 失败: %v", err)
	}

	var content strings.Builder
	for ev := range events {
		if ev.Type == EventChunk {
			content.WriteString(ev.Content)
		}
	}
	if content.String() != "流式回答内容" {
		t.Errorf("无工具时增强应回退普通流式，实际输出: %q", content.String())
	}
}

// 增强模式 + 检索为空：系统强制 web 搜索兜底（不依赖模型自觉），回答基于搜索结果
func TestAsk_EmptyRetrievalEnhancedForcesSearch(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"vector_store","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "不应直接生成", nil
			}
		},
		toolFunc: func(ctx context.Context, messages []llm.Message, tools []llm.FunctionTool) (*llm.ToolResponse, error) {
			// 系统兜底已注入搜索结果（role=tool 消息），模型应直接基于结果回答
			var hasToolResult bool
			for _, m := range messages {
				if m.Role == llm.RoleTool {
					hasToolResult = true
				}
			}
			if !hasToolResult {
				t.Errorf("强制搜索兜底后应存在 role=tool 搜索结果消息")
			}
			return &llm.ToolResponse{Content: "基于网络搜索：量子纠缠是量子力学现象"}, nil
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return nil, nil // 检索为空
		},
	}
	web := &fakeSearchProvider{available: true}

	engine := NewEngine(cfg, fl, ft, NewMemoryHistoryStore(50), nil, WithSearchProvider(web)).(*RAGEngine)

	res, err := engine.Ask(context.Background(), "s1", "什么是量子纠缠？", WithThinking(true), WithEnhanced(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "基于网络搜索：量子纠缠是量子力学现象" {
		t.Errorf("Answer = %q, 应基于强制搜索结果", res.Answer)
	}
	if web.query == "" {
		t.Errorf("强制搜索未执行（web.query 为空）")
	}

	// 思考链含「检索为空自动搜索」工具步骤
	var toolStep *ThinkingStep
	for i := range res.Thinking {
		if res.Thinking[i].Type == StepTool {
			toolStep = &res.Thinking[i]
		}
	}
	if toolStep == nil {
		t.Fatal("思考链路缺少 StepTool（强制搜索兜底）")
	}
	data, ok := toolStep.Data.(ToolStepData)
	if !ok || data.Name != "web_search" {
		t.Errorf("ToolStepData 错误: %+v", toolStep.Data)
	}
	if genCalls == 0 {
		t.Errorf("增强生成未调用 GenerateTool")
	}
}

// 增强模式 + 检索为空 + 无 web_search 工具：仍回退「未找到相关资料」
func TestAsk_EmptyRetrievalEnhancedNoToolFallsBack(t *testing.T) {
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
	engine := NewEngine(testRAGConfig(), fl, ft, NewMemoryHistoryStore(50), nil) // 无搜索提供者

	res, err := engine.Ask(context.Background(), "s1", "不存在的问题", WithEnhanced(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != noAnswerText {
		t.Errorf("无工具时应兜底 noAnswerText: %q", res.Answer)
	}
}
