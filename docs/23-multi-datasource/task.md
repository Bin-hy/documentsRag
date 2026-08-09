# 多数据源与数据源注册中心 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/datasource/source.go` | Source 接口、SearchRequest、名称常量 |
| 新建 | `internal/datasource/registry.go` | Registry 接口 + defaultRegistry（仿 loader/registry.go） |
| 新建 | `internal/datasource/vectorstore.go` | vectorStoreSource（包装 retriever.Retriever） |
| 新建 | `internal/datasource/websource.go` | webSearchSource（占位不可用） |
| 新建 | `internal/datasource/registry_test.go` | 注册中心 + 内置源测试 |
| 修改 | `internal/config/config.go` | StrategyConfig 增加 DataSources 字段 |
| 修改 | `internal/rag/strategy.go` | EffectiveStrategy.DataSources + ResolveStrategy 合并 |
| 修改 | `internal/rag/strategy_test.go` | DataSources 合并测试 |
| 修改 | `internal/rag/prompt.go` | defaultRoutingTemplate 扩展 data_source、routeData.AllowedText |
| 修改 | `internal/rag/routing.go` | routeResult.DataSource、routeQuery 接收 allowed |
| 修改 | `internal/rag/thinking.go` | RoutingData.DataSource |
| 修改 | `internal/rag/engine.go` | AskOptions 字段、resolveDataSource、NewEngine WithSources、Ask/StreamAsk 接入、prepare 分派 |
| 修改 | `internal/rag/context.go` | Source.SourceType + buildContext 读取 |
| 修改 | `internal/app/rebuild.go` | 创建/注入 registry（WithSources） |
| 修改 | `configs/config.yaml` | rag.strategy.data_sources 说明注释 |
| 新建 | `docs/23-multi-datasource/决策记录.md` | 关键决策记录 |

## T1: datasource 包基础（接口 + 注册中心）

**文件：** `internal/datasource/source.go`、`internal/datasource/registry.go`
**依赖：** 无
**步骤：**
1. `source.go`：定义 `SearchRequest{Query string; TopK int; Filter map[string]any}`、`Source` 接口（`Name() string` / `Available() bool` / `Search(ctx, req) ([]retriever.RetrieveResult, error)`）、常量 `SourceVectorStore="vector_store"`、`SourceWebSearch="web_search"`，包注释说明扩展方式
2. `registry.go`：定义 `Registry` 接口（`Register(s Source)` / `Get(name) (Source, bool)` / `List() []Source` / `Names() []string`）与 `defaultRegistry{map[string]Source}` 实现，`NewRegistry()` 构造；Register 时重名直接覆盖

**验证：** `go build ./internal/datasource/...` 编译通过

## T2: 内置数据源（vector_store + web 占位）

**文件：** `internal/datasource/vectorstore.go`、`internal/datasource/websource.go`、`internal/datasource/registry_test.go`
**依赖：** T1
**步骤：**
1. `vectorstore.go`：`vectorStoreSource{retriever retriever.Retriever}`，`Name()="vector_store"`、`Available()=true`、`Search` 调 `retriever.Search(RetrieveRequest{Query, TopK, Filter})` 并对每个结果补 `Metadata["source_type"]="vector_store"`（保留原 Metadata 键值）
2. `websource.go`：`webSearchSource{}`，`Name()="web_search"`、`Available()=false`、`Search` 返回 `fmt.Errorf("web_search 数据源未实现")`
3. `registry_test.go`：注册中心 Register/Get/List/Names、重名覆盖；vectorStoreSource 用 httptest mock retriever 验证 Search 透传与 source_type 标记；webSearchSource Available=false、Search 报错

**验证：** `go test ./internal/datasource/...` 通过

## T3: config 增加 DataSources 字段

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. `StrategyConfig` 增加 `DataSources []string \`yaml:"data_sources" json:"data_sources,omitempty"\``，注释说明「允许的数据源，空=默认仅 vector_store（私有性默认），三层覆盖」
2. `configs/config.yaml` 的 `rag.strategy` 段增加 `data_sources: []` 及注释示例（如 `[vector_store, web_search]`）

**验证：** `go build ./internal/config/...` 通过；`go test ./internal/config/...` 通过

## T4: EffectiveStrategy.DataSources + 三层合并

**文件：** `internal/rag/strategy.go`、`internal/rag/strategy_test.go`
**依赖：** T3
**步骤：**
1. `EffectiveStrategy` 增加 `DataSources []string`（注释：允许的数据源，空=nil 默认仅 vector_store）
2. `ResolveStrategy`：新增合并逻辑——`req.DataSources` 非空用之，否则 `kb.DataSources` 非空用之，否则 `global.DataSources`；全空 → nil；不参与 `ValidateStrategy` 校验（未知源名由使用方容错）
3. `strategy_test.go`：新增用例——请求级覆盖 KB/全局、KB 覆盖全局、全空 → nil

**验证：** `go test ./internal/rag/ -run TestResolveStrategy -v` 通过

## T5: 路由判定扩展（模板 + routeResult + routeQuery）

**文件：** `internal/rag/prompt.go`、`internal/rag/routing.go`、`internal/rag/prompt_test.go`
**依赖：** T1（仅引用常量名，可在 T2 后）
**步骤：**
1. `prompt.go`：`routeData` 增加 `AllowedText string`；`defaultRoutingTemplate` 增加数据源段——输出 `"data_source": "vector_store|web_search"`，并说明「可选数据源（仅能在以下范围内选择）：{{.AllowedText}}」；`renderRouting(question, allowedText, tpl)` 签名调整（保持旧调用兼容可加变参或改调用点）
2. `routing.go`：`routeResult` 增加 `DataSource string`；`routeQuery(ctx, question, allowedText string)` 解析 JSON 增加 `data_source` 字段
3. `prompt_test.go`：默认模板渲染含 data_source 与 AllowedText；路由 JSON 解析含 data_source

**验证：** `go test ./internal/rag/ -run 'Routing|Prompt' -v` 通过

## T6: 思考链路 RoutingData.DataSource

**文件：** `internal/rag/thinking.go`
**依赖：** 无
**步骤：**
1. `RoutingData` 增加 `DataSource string \`json:"data_source,omitempty"\`` 字段

**验证：** `go build ./internal/rag/...` 通过

## T7: engine 接入（AskOptions + resolveDataSource + NewEngine）

**文件：** `internal/rag/engine.go`
**依赖：** T1、T2、T4、T5、T6
**步骤：**
1. `AskOptions` 增加内部字段 `DataSource string`、`AllowedDataSources []string`（注释：engine 内部设置，不对外配置）
2. `RAGEngine` 增加字段 `sources datasource.Registry`；`NewEngine` 改为可变参数 `opts ...EngineOption`，新增 `WithSources(reg datasource.Registry)`；`sources` 为 nil 时构造内 `datasource.NewRegistry()` 并注册 `NewVectorStoreSource(retriever)` + `NewWebSearchSource()`
3. 新增方法 `resolveDataSource(allowed []string, candidate string) (string, Source)`：candidate 空/未知 → `SourceVectorStore`；allowed 非空且候选不在 allowed → 取 allowed 首项；返回 (源名, 源实例)；源实例为 nil 时返回 `SourceVectorStore` 兜底
4. 新增辅助 `allowedText(allowed []string) string`：渲染 allowed 列表为模板文本（空 → 「仅向量知识库」）

**验证：** `go build ./internal/rag/...` 通过

## T8: 引用来源标记 SourceType

**文件：** `internal/rag/context.go`、`internal/rag/context_test.go`
**依赖：** 无
**步骤：**
1. `Source` 增加 `SourceType string \`json:"source_type,omitempty"\``
2. `buildContext` 构造 `Source` 时从 `chunk.Metadata["source_type"]` 读取（`metaString`），无则空
3. `context_test.go`：补充用例验证 source_type 透传

**验证：** `go test ./internal/rag/ -run TestBuildContext -v` 通过

## T9: Ask/StreamAsk 路由接入 + prepare 检索分派

**文件：** `internal/rag/engine.go`
**依赖：** T7、T8
**步骤：**
1. `Ask`（engine.go:227-258 附近）与 `StreamAsk`（engine.go:329-357 附近）路由段：
   - `routeQuery` 调用传 `allowedText(effective.DataSources)`
   - 判定成功后 `resolveDataSource`：`srcName, src := e.resolveDataSource(eff.DataSources, route.DataSource)`；若 src 不可用（`!src.Available()`）→ `slog.Warn` + 降级 `SourceVectorStore`
   - 设置 `o.DataSource = srcName`、`o.AllowedDataSources = eff.DataSources`
   - `RoutingData{Complexity, Strategy, DataSource: srcName, Reasoning}` 记录思考链路
2. `prepare` 检索分派（engine.go:560-572 附近）：
   - `o.DataSource` 非空且 ≠ `SourceVectorStore` 时：`src, ok := e.sources.Get(o.DataSource)`；ok 且可用 → `src.Search(SearchRequest{Query: query, TopK: ragCfg.TopK, Filter: kbFilter(o.KBID)})`；否则走现有路径
   - 现有 HyDE / `retriever.Search` 路径保持不变（o.DataSource 为空或 vector_store）
3. `engine_test.go` 补充：mock 路由返回 web_search 且 allowed 为空（默认）→ 实际 vector_store；allowed=[vector_store] 且路由返回 web_search → vector_store；mock registry 注入自定义源

**验证：** `go test ./internal/rag/ -run 'Engine|Ask' -v` 通过；`go test ./internal/rag/...` 全量通过

## T10: app 装配注入

**文件：** `internal/app/rebuild.go`
**依赖：** T9
**步骤：**
1. `BuildRuntime` 中 `rag.NewEngine(...)` 调用追加 `rag.WithSources(...)`：创建 `datasource.NewRegistry()` 并注册内置源（或直接用 NewEngine 默认；显式传入便于未来动态注册），注释说明后续 MCP 可在此注册自定义数据源

**验证：** `go build ./...` 通过

## T11: 全量回归 + 决策记录

**文件：** `docs/23-multi-datasource/决策记录.md`
**依赖：** T10
**步骤：**
1. 运行 `go build ./...` 与 `go test ./internal/...` 全量回归（确认 N1：未配置新字段时行为不变）
2. `决策记录.md`：记录关键决策（独立 datasource 包、复用 retriever.RetrieveResult、StrategyConfig.DataSources 三层合并、代码强制约束、不可用降级、策略组合最小化、NewEngine Option 兼容）

**验证：** `go test ./internal/...` 全部 ok；决策记录内容完整

## 执行顺序

```
T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9 → T10 → T11
         ↘ T8（可与 T3-T7 并行）↗
T5 依赖 T1 常量名（可在 T2 后实施）
```
