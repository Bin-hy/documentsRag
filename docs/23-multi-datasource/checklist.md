# 多数据源与数据源注册中心 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] 数据源接口可被实现（验证：`internal/datasource` 包编译通过；自定义 mock 源实现 `Source` 接口并被注册）
- [ ] 注册中心支持注册/按名获取/列出（验证：`go test ./internal/datasource/...` 通过，覆盖 Register/Get/List/Names/重名覆盖）
- [ ] 默认注册 vector_store（可用）与 web_search（不可用）（验证：引擎默认 registry `Get("vector_store")` 可用、`Get("web_search").Available()==false`；相关单测通过）
- [ ] vector 源检索透传并标记来源（验证：vectorStoreSource 单测断言结果 `Metadata["source_type"]=="vector_store"`）
- [ ] web 占位源检索返回明确错误（验证：webSearchSource 单测断言错误包含「未实现」）
- [ ] routing=auto 时路由判定输出含 data_source（验证：路由模板渲染测试断言含 `data_source`；routeQuery JSON 解析测试通过）
- [ ] 不可用数据源自动降级（验证：engine 单测——mock 路由返回 web_search 且默认 registry → 实际走向量库，不报错）
- [ ] 引用来源带来源类型（验证：context 单测断言 `Source.SourceType` 从 Metadata 透传）

## 私有性（spec AC4 核心）

- [ ] 限定仅 vector_store 时，即使路由判定返回 web_search 也不会路由到 web（验证：engine 单测——`WithStrategy` 传 `DataSources=["vector_store"]` + mock 路由返回 `data_source=web_search` → 实际检索源为 vector_store）
- [ ] 三层合并：请求级 DataSources 覆盖 KB/全局（验证：`go test ./internal/rag/ -run TestResolveStrategy` 新增用例通过）
- [ ] 未配置 DataSources 时默认仅 vector_store（验证：ResolveStrategy 全空 → `DataSources==nil`；engine 默认路径不变）

## 集成

- [ ] RAGEngine 正确调用数据源（验证：engine 单测注入 mock registry 自定义源，断言其 Search 被调用并进入上下文）
- [ ] NewEngine 现有调用方零改动编译通过（验证：`go build ./...` 通过，rebuild.go/AssembleEvalDeps 未改签名）
- [ ] 数据源选择出现在思考链路（验证：engine 测试/行为观察——RoutingData 含 `data_source` 字段并随 thinking 输出）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...`）
- [ ] 所有单元测试通过（验证：`go test ./internal/...` 全绿）
- [ ] gofmt 无未格式化文件（验证：`gofmt -l internal/` 输出为空）

## 端到端场景

- [ ] 场景 1（回归兼容）：不配置任何新字段，服务启动后正常提问（向量库检索 → 回答），行为与本次改动前一致（验证：现有 engine 集成测试全绿 + 手动走一遍 Ask 正常返回）
- [ ] 场景 2（私有性强制）：配置 routing=auto + 请求级 `DataSources=["vector_store"]`，提问内容知识库未覆盖 → 返回「未找到相关资料。」而非任何 web 结果（验证：engine 测试断言空结果兜底文案）
- [ ] 场景 3（MCP 预留）：通过请求参数（`WithStrategy` 的 `DataSources=["vector_store"]`）强制纯向量库 RAG——即使全局允许 web_search，本次请求也不路由到 web（验证：engine 单测覆盖请求级限定优先级）
