# 多数据源与数据源注册中心 Plan

## 架构概览

新增独立数据源层 `internal/datasource`，与检索器解耦：

- **数据源抽象（Source 接口）**：统一「名称 / 可用性 / 检索」三个能力，新增数据源只需实现该接口。
- **注册中心（Registry）**：仿 `internal/loader/registry.go` 的注册表模式，支持运行时注册、按名获取、列出全部数据源（动态加载）。
- **两个内置数据源**：`vector_store`（包装现有 `retriever.Retriever`，可用）与 `web_search`（占位，不可用）。
- **路由判定扩展**：`routeQuery` 在输出复杂度/策略之外，新增 `data_source` 维度；`routing=auto` 时由 LLM 判定本次查询用哪个数据源。
- **私有性限定**：`StrategyConfig` 新增 `DataSources`（允许的数据源集合），复用现有「请求 > 知识库 > 全局」三层合并机制；路由结果受允许集合约束，限定后即使 LLM 输出限定外的数据源也不会被采用。
- **不可用降级**：路由到未实现/不可用的数据源时自动降级回 `vector_store`，请求不中断。

模块划分：`datasource`（抽象 + 注册中心 + 内置源）→ 被 `rag.RAGEngine` 消费；`config` 提供配置字段；`app.BuildRuntime` 负责装配与注入。

## 核心数据结构

### datasource 包

```go
// source.go
// SearchRequest 数据源检索请求
type SearchRequest struct {
    Query  string
    TopK   int
    Filter map[string]any // 知识库范围等过滤条件（kb_id）
}

// Source 数据源接口：新增数据源只需实现此接口并在注册中心注册
type Source interface {
    Name() string                                        // 数据源名称（如 vector_store / web_search）
    Available() bool                                     // 是否已实现可用（占位源返回 false）
    Search(ctx context.Context, req SearchRequest) ([]retriever.RetrieveResult, error)
}

// 数据源名称常量
const (
    SourceVectorStore = "vector_store" // 内置：向量知识库（默认，始终可用）
    SourceWebSearch   = "web_search"   // 占位：web 搜索（未实现，Available()==false）
)

// registry.go（仿 loader/registry.go）
type Registry interface {
    Register(s Source)          // 动态注册（运行时追加数据源）
    Get(name string) (Source, bool)
    List() []Source             // 全部已注册数据源
    Names() []string
}
type defaultRegistry struct{ m map[string]Source }
func NewRegistry() Registry

// vectorstore.go — vectorStoreSource：包装 retriever.Retriever
// Search 调 retriever.Search（含向量+BM25+重排），并对结果 Metadata 补 source_type=vector_store
type vectorStoreSource struct{ retriever retriever.Retriever }

// websource.go — webSearchSource：占位，Available()==false，Search 返回明确错误
type webSearchSource struct{}
```

### config 包

```go
// StrategyConfig 新增第 8 个字段（与 query/fusion 等同层级，三层覆盖）
type StrategyConfig struct {
    // ...现有 7 字段...
    // DataSources 允许的数据源（vector_store / web_search），空=默认仅 vector_store（私有性默认）；
    // 三层合并：请求 > 知识库 > 全局，高优先级非空覆盖
    DataSources []string `yaml:"data_sources" json:"data_sources,omitempty"`
}
```

### rag 包

```go
// strategy.go
type EffectiveStrategy struct {
    // ...现有 7 字段...
    DataSources []string // 允许的数据源（合并后），空=nil 表示默认仅 vector_store
}
// ResolveStrategy 合并：req.DataSources 非空用之，否则 kb，否则 global；全部为空 → nil

// routing.go
type routeResult struct {
    Complexity string // simple / medium / complex
    Strategy   string // direct / multi_query / decomposition
    DataSource string // vector_store / web_search / ""（空=默认 vector_store）
    Reasoning  string
}

// thinking.go
type RoutingData struct {
    Complexity string `json:"complexity"`
    Strategy   string `json:"strategy"`
    DataSource string `json:"data_source,omitempty"`
    Reasoning  string `json:"reasoning,omitempty"`
}

// context.go — 引用来源标记来源类型（F8）
type Source struct {
    ID         string  `json:"id"`
    Filename   string  `json:"filename"`
    Heading    string  `json:"heading"`
    Score      float32 `json:"score"`
    SourceType string  `json:"source_type,omitempty"` // vector_store / web_search
}
// buildContext 从 chunk.Metadata["source_type"] 读取填充

// engine.go
type AskOptions struct {
    // ...现有字段...
    // 内部字段：路由判定选中的数据源（prepare 读取）；允许集合（解析后，避免重复合并）
    DataSource        string   // 非导出语义：仅 engine 内部设置（routing 判定后），不对外配置
    AllowedDataSources []string
}
```

## 模块设计

### internal/datasource（新建）

**职责：** 数据源抽象 + 注册中心 + 内置源。
**对外接口：** `Source`、`Registry`、`SearchRequest`、`NewRegistry`、`NewVectorStoreSource(retriever)`、`NewWebSearchSource()`、`SourceVectorStore`/`SourceWebSearch` 常量。
**依赖：** `retriever`（复用 `RetrieveResult`）。无反向依赖。

### internal/rag（改造）

- `NewEngine` 增加可变参数 `opts ...EngineOption`，新增 `WithSources(reg datasource.Registry)`；`RAGEngine` 增加字段 `sources datasource.Registry`，为 nil 时在构造内构建默认 registry（vector + web 占位）。
- `routeQuery(ctx, question, allowed []string)`：`routeData` 增加 `AllowedText`（由 allowed 渲染的可选数据源说明），LLM 输出解析含 `DataSource`。
- `resolveDataSource(allowed, candidate string) (string, error)`：候选空/未知 → `vector_store`；allowed 非空且候选不在 allowed → 取 allowed 首项；否则候选。
- `Ask`/`StreamAsk` 路由段：判定后调用 `resolveDataSource`，不可用降级（`slog.Warn` + 思考链路记录），设置 `o.DataSource`、`o.AllowedDataSources`。
- `prepare` 检索分派（engine.go:560-572 处）：`o.DataSource` 非空且 ≠ `vector_store` 时，从 `sources` 取该源 `Search`；否则走现有 HyDE / `retriever.Search` 路径（行为不变）。
- `defaultRoutingTemplate` 扩展输出 `data_source`（模板内联 allowed 说明）；`routeData` 结构扩展。

### internal/config（改造）

`StrategyConfig` 增加 `DataSources []string`；`configs/config.yaml` rag.strategy 段补充注释示例。

### internal/app（改造）

`rebuild.go` 的 `BuildRuntime`：创建 `datasource.Registry`（或复用 NewEngine 默认），通过 `rag.WithSources` 注入；为未来动态注册（如 MCP 数据源）预留入口。

## 模块交互（数据流）

```
Ask/StreamAsk
  └─ effective() 合并策略 → EffectiveStrategy{..., DataSources=allowed}
  └─ routing=auto ?
        └─ routeQuery(question, allowed) → route{Strategy, DataSource}
              └─ resolveDataSource(allowed, route.DataSource)
                    ├─ allowed 非空且候选不在内 → allowed 首项（私有性强制）
                    ├─ 候选源不可用（web 占位）→ vector_store（降级，不中断）
                    └─ 设置 o.DataSource / o.AllowedDataSources，记录 RoutingData.DataSource
  └─ prepare()
        └─ o.DataSource ≠ vector_store ？
              ├─ 是：sources.Get(o.DataSource).Search(SearchRequest{Query,TopK,Filter})
              └─ 否：HyDE / retriever.Search（现有路径，行为不变）
        └─ buildContext → Source.SourceType（来自 Metadata["source_type"]）
  └─ llm.Generate → RAGResult{Answer, Sources[{SourceType}]}
```

## 文件组织

```
internal/datasource/
├── source.go         — Source 接口、SearchRequest、名称常量
├── registry.go       — Registry 接口 + defaultRegistry（仿 loader/registry.go）
├── vectorstore.go    — vectorStoreSource（包装 retriever.Retriever）
├── websource.go      — webSearchSource（占位，Available()==false）
├── source_test.go    — 接口/常量测试
└── registry_test.go  — 注册中心测试

internal/rag/
├── engine.go         — AskOptions.DataSource/AllowedDataSources、resolveDataSource、
│                        prepare 检索分派、NewEngine WithSources、默认 registry 构建
├── routing.go        — routeResult.DataSource、routeQuery 接收 allowed
├── prompt.go         — defaultRoutingTemplate 扩展 data_source、routeData.AllowedText
├── strategy.go       — EffectiveStrategy.DataSources、ResolveStrategy 合并
├── context.go        — Source.SourceType、buildContext 读取 Metadata
├── thinking.go       — RoutingData.DataSource

internal/config/config.go  — StrategyConfig.DataSources
internal/app/rebuild.go     — 创建/注入 registry（WithSources）
configs/config.yaml         — rag.strategy.data_sources 说明注释
docs/23-multi-datasource/   — spec.md / plan.md / task.md / checklist.md / 决策记录.md
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 数据源抽象位置 | 独立 `internal/datasource` 包 | 与检索器解耦；仿 `loader/registry.go` 先例；后续 MCP 可复用同一注册中心 |
| 数据源结果结构 | 复用 `retriever.RetrieveResult` | 与 `buildContext`/上下文组装零转换衔接；来源类型用 `Metadata["source_type"]` 标记，不动 retriever 包 |
| 数据源选择时机 | LLM 路由判定（routing=auto）输出 `data_source` | 用户已确认「仅 LLM 路由判定」，不做空结果兜底 |
| 私有性限定机制 | `StrategyConfig.DataSources` 三层覆盖（请求 > KB > 全局） | 复用现有 `ResolveStrategy` 三层合并，KB/请求/MCP 天然支持，改动最小 |
| 私有性强制方式 | 代码强制：allowed 集合约束路由结果 + 路由模板注入 allowed 说明 | AC4 双层保险，确保限定后不路由到限定外数据源 |
| 不可用降级 | 路由到不可用源 → 降级 `vector_store`，slog.Warn + 思考链路 | 占位 web 源存在时请求不中断（AC5） |
| 策略路径组合 | `data_source ≠ vector_store` 时走常规单次检索，不叠加 multi/decomposition/hyde | 「怎么搜 + 搜哪」组合爆炸，本次范围最小化；向量源行为完全不变 |
| NewEngine 兼容 | 可变参数 `EngineOption` + 默认 registry | 现有调用方（rebuild.go / AssembleEvalDeps）零改动 |
| 路由模板 | `routeData` 注入 allowed 列表渲染可选源说明 | LLM 输出受约束，私有性更稳 |
| 引用来源标记 | `Source.SourceType` 从 `Metadata["source_type"]` 读取 | 前端/MCP 可区分来源，不影响现有上下文模板渲染 |

## spec 覆盖对照

| Spec 需求 | Plan 归属 |
|-----------|-----------|
| F1 数据源抽象接口 | `datasource.Source` 接口（source.go） |
| F2 注册中心 | `datasource.Registry`（registry.go） |
| F3 内置向量库源 | `vectorStoreSource`（vectorstore.go） |
| F4 web 占位源 | `webSearchSource`（websource.go） |
| F5 路由 data_source 维度 | `routeResult.DataSource` + `routeQuery` 扩展（routing.go/prompt.go） |
| F6 私有性限定 | `StrategyConfig.DataSources` 三层合并 + `resolveDataSource` 强制约束 |
| F7 不可用降级 | `resolveDataSource` 内 `Available()` 检查降级 |
| F8 结果与引用集成 | `Source.SourceType` + `buildContext` 读取 |
| F9 路由模板扩展 | `defaultRoutingTemplate` + `routeData.AllowedText` + 自定义路径兼容 |
| N1 回归兼容 | 未配置时默认 vector_store、检索分派走现有路径、NewEngine 签名兼容 |
| N2 扩展成本 | 新源 = 实现 Source + Register 一行 |
| N3 思考链路 | `RoutingData.DataSource` |
| N4 配置兼容 | 新增字段均可选 |
