# RAG 路由 + HyDE 假设文档 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | 5 字段 + RoutingOn/HyDEOn + applyDefaults |
| 修改 | `configs/config.yaml` | rag 段加 routing/hyde 注释示例 |
| 修改 | `internal/retriever/retriever.go` | SearchByVector + 抽出 vectorSearchByVec |
| 修改 | `internal/rag/engine.go` | RAGEngine 加 embedder、NewEngine 签名、Ask/StreamAsk 路由分支 |
| 新建 | `internal/rag/routing.go` | routeQuery / hydeSearch / 分流执行 |
| 修改 | `internal/rag/prompt.go` | routing/hyde 模板 + render |
| 修改 | `internal/app/app.go` | NewEngine 传 emb（两处装配） |
| 修改 | `internal/rag/engine_test.go` | 7 新用例 + fakeRetriever/fakeLLM 扩展 |

## T1: config 配置项

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. `RAGConfig` 增加字段：
   - `RoutingEnabled *bool \`yaml:"routing_enabled"\``
   - `RoutingFallback string \`yaml:"routing_fallback"\``
   - `HyDEEnabled *bool \`yaml:"hyde_enabled"\``
   - `HyDESkipSimple *bool \`yaml:"hyde_skip_simple"\``
   - `RoutingTemplatePath`、`HyDETemplatePath`（string）
2. 新增方法 `RoutingOn()`、`HyDEOn()`（nil 视为关闭）
3. `applyDefaults`：`RoutingFallback` 默认 "multi_query"；`HyDESkipSimple` 默认 true（nil 视为 true）
4. config.yaml rag 段加注释示例（routing_enabled / hyde_enabled 默认 false）

**验证：** `go build ./internal/config/...` 通过

## T2: retriever SearchByVector

**文件：** `internal/retriever/retriever.go`
**依赖：** 无（与 T1 并行）
**步骤：**
1. 抽出 `vectorSearchByVec(ctx, vec []float32, topK int, filter map[string]any) ([]RetrieveResult, error)`：现有 vectorSearch 的向量检索部分（跳过 query embed，直接 vectorstore.Search）
2. `Search` 的 vectorSearch 内部改用 `vectorSearchByVec`（Embed 后调用）
3. 新增公开方法 `SearchByVector(ctx, vector []float32, topK int, filter map[string]any) ([]RetrieveResult, error)`（HyDE 用，无 BM25/重排）
4. `Retriever` 接口加 `SearchByVector` 签名

**验证：** `go build ./internal/retriever/...` 通过（fake 需补——T6 处理）

## T3: prompt 模板

**文件：** `internal/rag/prompt.go`
**依赖：** T1
**步骤：**
1. 新增 2 模板：
   - `defaultRoutingTemplate`：复杂度判定，输出 JSON `{"complexity": "simple|medium|complex", "strategy": "direct|multi_query|decomposition", "reasoning": "..."}`
   - `defaultHyDETemplate`：生成假设文档（详细、即使不确定），输出假设文档文本
2. `promptTemplates` 增加 `routing`、`hyde` 字段；`loadPromptTemplates` 加载
3. render 函数：`renderRouting(question)`、`renderHyDE(question)`

**验证：** `go build ./internal/rag/...` 通过

## T4: routing.go 路由判定与 HyDE

**文件：** `internal/rag/routing.go`
**依赖：** T2、T3
**步骤：**
1. `routeResult{Complexity, Strategy, Reasoning}` 类型
2. `routeQuery(ctx, question) (routeResult, bool, error)`：LLM 判定 → 解析 JSON；失败返回 (zero, false, err)（外层回退）
3. `hydeSearch(ctx, query, o) ([]retriever.RetrieveResult, error)`：
   - 生成假设文档（LLM）→ `e.embedder.Embed([假设文档])`
   - `e.retriever.SearchByVector(hypoVec, topK, kbFilter)`（HyDE 路）
   - `e.retriever.Search(query)`（原查询路）
   - `FuseMultiQuery([hyde路, 原查询路], 60, topK)` 融合
   - Embedding 失败 → 降级返回原查询 Search 结果
4. `shouldHyde(ctx, route)`：`HyDESkipSimple` 且 complexity=simple → false
5. 日志：假设文档、检索路数、融合数、耗时

**验证：** `go build ./internal/rag/...` 通过

## T5: engine 路由分支 + embedder 注入

**文件：** `internal/rag/engine.go`、`internal/app/app.go`
**依赖：** T4
**步骤：**
1. `RAGEngine` 加 `embedder embedding.Embedder` 字段；`NewEngine` 加参数 `emb embedding.Embedder`
2. `Ask` 开头（现有策略分支之前）加路由分支：
   ```go
   if e.cfg.RoutingOn() {
       route, ok, err := e.routeQuery(ctx, question)
       strategy := route.Strategy
       if err != nil || !ok {
           strategy = e.cfg.RoutingFallback // 默认 multi_query
           slog.Warn("路由判定失败，回退默认策略", "fallback", strategy, "err", err)
       }
       // 按 strategy 分流：direct→常规；multi_query→阶段一；decomposition→阶段二
       // 各路径检索环节用 hydeSearch 替代直接 Search（若 HyDE 启用且不 skip）
   }
   ```
3. `StreamAsk` 同理（路由判定同步、HyDE 同步、结果流式）
4. `app.go` 两处 `NewEngine` 调用传 `emb`

**验证：** `go build ./...` 通过

## T6: 测试

**文件：** `internal/rag/engine_test.go`
**依赖：** T5
**步骤：**
1. `fakeRetriever` 补 `SearchByVector`（记录调用、返回固定结果）
2. 新增 fakeEmbedder（`Embed` 返回固定向量，可配置错误）
3. fakeLLM 按 user 消息特征返回路由 JSON / 假设文档
4. 新增用例：
   - `TestAsk_RoutingDirect`：simple/direct → 常规路径（Search 被调、SearchMulti 未调）
   - `TestAsk_RoutingMultiQuery`：medium/multi_query → SearchMulti 被调
   - `TestAsk_RoutingDecomposition`：complex/decomposition → 分解路径（子问题检索）
   - `TestAsk_RoutingFallback`：判定失败 → 回退 multi_query
   - `TestAsk_HyDE`：启用 → 假设文档生成 + SearchByVector + 融合
   - `TestAsk_HyDESkipSimple`：simple 查询跳过 HyDE（SearchByVector 未调）
   - `TestAsk_HyDEEmbedFail`：embedding 失败 → 降级原查询
5. 现有测试不回归（策略默认关）

**验证：** `go test ./internal/rag/ -v` 全部通过

## T7: 全量验证 + 冒烟

**文件：** 无新增
**依赖：** T1-T6
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿
2. 临时配置开启 `routing_enabled: true`，启动服务，问答观察日志（复杂度判定/strategy 分流）
3. 冒烟后恢复默认配置

**验证：** 上述命令全绿；冒烟日志符合预期

## 执行顺序

```
T1 ─┐
    ├→ T3 → T4 → T5 → T6 → T7
T2 ─┘
```

T1（config）与 T2（SearchByVector）并行；T3 依赖 T1；T4 依赖 T2+T3；T5 依赖 T4；T6 依赖 T5；T7 收尾。
