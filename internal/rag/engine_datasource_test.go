package rag

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/datasource"
	"github.com/Bin-hy/bin-rag/internal/llm"
	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// fakeSource 测试用数据源
type fakeSource struct {
	name       string
	available  bool
	searchFunc func(ctx context.Context, req datasource.SearchRequest) ([]retriever.RetrieveResult, error)
	mu         sync.Mutex
	searches   []datasource.SearchRequest
}

func (s *fakeSource) Name() string    { return s.name }
func (s *fakeSource) Available() bool { return s.available }
func (s *fakeSource) Search(ctx context.Context, req datasource.SearchRequest) ([]retriever.RetrieveResult, error) {
	s.mu.Lock()
	s.searches = append(s.searches, req)
	s.mu.Unlock()
	return s.searchFunc(ctx, req)
}

// newRoutingEngine 构造 routing=auto 的引擎；genFunc 第 1 次调用返回路由 JSON
func newRoutingEngine(t *testing.T, cfg config.RAGConfig, fl *fakeLLM, ft *fakeRetriever, opts ...EngineOption) (Engine, *fakeLLM) {
	t.Helper()
	if cfg.Strategy.Routing == "" {
		cfg.Strategy.Routing = "auto"
	}
	hs := NewMemoryHistoryStore(50)
	engine := NewEngine(cfg, fl, ft, hs, nil, opts...)
	return engine, fl
}

// AC5: 路由到 web_search 但默认注册中心 web 不可用 → 降级向量库，请求不中断
func TestAsk_DataSourceWebSearchFallsBackToVectorStore(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"web_search","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil // rewrite（direct 路径单查询改写）
			default:
				return "向量库回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	engine, _ := newRoutingEngine(t, cfg, fl, ft)

	res, err := engine.Ask(context.Background(), "s1", "问题", WithThinking(true))
	if err != nil {
		t.Fatalf("Ask 失败（web 不可用应降级而非报错）: %v", err)
	}
	if res.Answer != "向量库回答" {
		t.Errorf("应降级向量库生成: %q", res.Answer)
	}

	// 实际检索走了向量库（fakeRetriever.Search 被调用）
	ft.mu.Lock()
	n := len(ft.queries)
	ft.mu.Unlock()
	if n != 1 {
		t.Errorf("应走向量库检索 1 次，实际 %d", n)
	}

	// 思考链路：路由判定记录降级后的数据源 vector_store
	var ds string
	for _, st := range res.Thinking {
		if st.Type == StepRouting {
			if rd, ok := st.Data.(RoutingData); ok {
				ds = rd.DataSource
			}
		}
	}
	if ds != datasource.SourceVectorStore {
		t.Errorf("思考链路 data_source = %q, want %s（降级）", ds, datasource.SourceVectorStore)
	}
}

// AC4: 限定仅 vector_store 时，即使路由判定返回 web_search 也不会路由到 web
func TestAsk_DataSourceLimitedToVectorStore(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"web_search","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "受限回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	engine, _ := newRoutingEngine(t, cfg, fl, ft)

	// 请求级限定仅 vector_store（MCP 纯 RAG 场景）
	req := &config.StrategyConfig{DataSources: []string{datasource.SourceVectorStore}}
	_, err := engine.Ask(context.Background(), "s1", "问题", WithStrategy(nil, req), WithThinking(true))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}

	ft.mu.Lock()
	n := len(ft.queries)
	ft.mu.Unlock()
	if n != 1 {
		t.Errorf("限定 vector_store 应走向量库检索 1 次，实际 %d", n)
	}
}

// F5/F3/F8: 注入可用自定义数据源，路由到 web_search 时走该源，结果带 source_type 进入引用
func TestAsk_DataSourceCustomInjected(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{Routing: "auto", Thinking: "on", Query: "single", Fusion: "none"}

	var genCalls int
	fl := &fakeLLM{
		genFunc: func(_ context.Context, _ []llm.Message) (string, error) {
			genCalls++
			switch genCalls {
			case 1:
				return `{"complexity":"simple","strategy":"direct","data_source":"web_search","reasoning":"测试"}`, nil
			case 2:
				return "改写查询", nil
			default:
				return "web 回答", nil
			}
		},
	}
	ft := &fakeRetriever{
		searchFunc: func(_ context.Context, _ retriever.RetrieveRequest) ([]retriever.RetrieveResult, error) {
			return testResults(), nil
		},
	}
	web := &fakeSource{
		name:      datasource.SourceWebSearch,
		available: true,
		searchFunc: func(_ context.Context, _ datasource.SearchRequest) ([]retriever.RetrieveResult, error) {
			return []retriever.RetrieveResult{
				{ID: "w1", Content: "web 检索内容", Score: 0.9, Metadata: map[string]any{"filename": "https://example.com", "source_type": datasource.SourceWebSearch}},
			}, nil
		},
	}
	reg := datasource.NewRegistry()
	reg.Register(datasource.NewVectorStoreSource(ft))
	reg.Register(web)

	engine, _ := newRoutingEngine(t, cfg, fl, ft, WithSources(reg))

	req := &config.StrategyConfig{DataSources: []string{datasource.SourceVectorStore, datasource.SourceWebSearch}}
	res, err := engine.Ask(context.Background(), "s1", "问题", WithStrategy(nil, req))
	if err != nil {
		t.Fatalf("Ask 失败: %v", err)
	}
	if res.Answer != "web 回答" {
		t.Errorf("应走 web 源生成: %q", res.Answer)
	}

	// web 源被调用，向量库未调用
	web.mu.Lock()
	ws := len(web.searches)
	web.mu.Unlock()
	ft.mu.Lock()
	qs := len(ft.queries)
	ft.mu.Unlock()
	if ws != 1 {
		t.Errorf("web 源应被检索 1 次，实际 %d", ws)
	}
	if qs != 0 {
		t.Errorf("向量库不应被检索，实际 %d 次", qs)
	}

	// 引用来源带 source_type
	if len(res.Sources) != 1 || res.Sources[0].SourceType != datasource.SourceWebSearch {
		t.Errorf("Sources = %+v, want 1 条且 source_type=web_search", res.Sources)
	}
	if !strings.Contains(res.Sources[0].Filename, "example.com") {
		t.Errorf("Filename 应为 web 来源: %q", res.Sources[0].Filename)
	}
}

// 未知数据源名兜底 vector_store
func TestResolveDataSourceUnknown(t *testing.T) {
	ft := &fakeRetriever{}
	reg := datasource.NewRegistry()
	reg.Register(datasource.NewVectorStoreSource(ft))
	reg.Register(&fakeSource{name: datasource.SourceWebSearch, available: true})
	engine := NewEngine(testRAGConfig(), &fakeLLM{}, ft, NewMemoryHistoryStore(50), nil, WithSources(reg)).(*RAGEngine)

	if name, _ := engine.resolveDataSource(nil, ""); name != datasource.SourceVectorStore {
		t.Errorf("空候选应默认 vector_store, got %s", name)
	}
	if name, _ := engine.resolveDataSource(nil, "unknown"); name != datasource.SourceVectorStore {
		t.Errorf("未知候选应兜底 vector_store, got %s", name)
	}
	// allowed 非空且候选不在其中 → allowed 首项
	if name, _ := engine.resolveDataSource([]string{datasource.SourceVectorStore}, datasource.SourceWebSearch); name != datasource.SourceVectorStore {
		t.Errorf("限定约束应取 allowed 首项, got %s", name)
	}
	// allowed 含候选 → 候选保留
	if name, _ := engine.resolveDataSource([]string{datasource.SourceVectorStore, datasource.SourceWebSearch}, datasource.SourceWebSearch); name != datasource.SourceWebSearch {
		t.Errorf("候选在 allowed 内应保留, got %s", name)
	}
	// 候选在 allowed 内但源不可用 → 降级 vector_store
	reg2 := datasource.NewRegistry()
	reg2.Register(datasource.NewVectorStoreSource(ft))
	reg2.Register(datasource.NewWebSearchSource()) // 占位不可用
	engine2 := NewEngine(testRAGConfig(), &fakeLLM{}, ft, NewMemoryHistoryStore(50), nil, WithSources(reg2)).(*RAGEngine)
	if name, _ := engine2.resolveDataSource([]string{datasource.SourceVectorStore, datasource.SourceWebSearch}, datasource.SourceWebSearch); name != datasource.SourceVectorStore {
		t.Errorf("不可用源应降级 vector_store, got %s", name)
	}
	// allowed 仅含不可用源 → 降级 vector_store（显式告警，服务可用性优先）
	if name, _ := engine2.resolveDataSource([]string{datasource.SourceWebSearch}, datasource.SourceWebSearch); name != datasource.SourceVectorStore {
		t.Errorf("allowed 内无可用源应降级 vector_store, got %s", name)
	}
	// allowed 含可用源（如 vector_store 在前）→ 降级取 allowed 内可用源
	if name, _ := engine2.resolveDataSource([]string{datasource.SourceVectorStore}, datasource.SourceWebSearch); name != datasource.SourceVectorStore {
		t.Errorf("allowed 含可用源应取之, got %s", name)
	}
}

// 仅配置 data_sources（其他策略字段为空）时，旧开关兜底不得丢失 DataSources
func TestEffectivePreservesDataSourcesWithLegacyFallback(t *testing.T) {
	cfg := testRAGConfig()
	cfg.Strategy = config.StrategyConfig{DataSources: []string{datasource.SourceVectorStore, datasource.SourceWebSearch}}
	engine := NewEngine(cfg, &fakeLLM{}, &fakeRetriever{}, NewMemoryHistoryStore(50), nil).(*RAGEngine)

	eff := engine.effective(AskOptions{})
	if len(eff.DataSources) != 2 {
		t.Errorf("旧开关兜底丢失 DataSources: %v", eff.DataSources)
	}
	if eff.Routing != "off" {
		t.Errorf("未配置 routing 时应为 off, got %q", eff.Routing)
	}
}
