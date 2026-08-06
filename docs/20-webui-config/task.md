# WebUI 配置管理界面 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/config/manager.go` | ConfigManager（原子快照/更新/回滚/持久化） |
| 新建 | `internal/config/manager_test.go` | Get/Update/回滚/并发快照测试 |
| 新建 | `internal/app/rebuild.go` | RuntimeComponents + BuildRuntime |
| 修改 | `internal/app/app.go` | App 持 components atomic.Pointer + cfgMgr |
| 修改 | `internal/rag/engine.go` | WithConfigSnapshot AskOption + effective 从快照读 |
| 新建 | `internal/api/handler_config.go` | GET/PUT /config + 分组视图 |
| 修改 | `internal/api/middleware.go` | bootstrap key 写权限 |
| 修改 | `internal/api/router.go` | 注册 config 路由 |
| 修改 | `internal/api/api_test.go` | config 读写用例 |
| 新建 | `frontend/src/api/config.ts` | getConfig / updateConfig |
| 修改 | `frontend/src/api/types.ts` | ConfigView 类型 |
| 新建 | `frontend/src/views/SettingsView.vue` | 配置管理页 |
| 修改 | `frontend/src/router/index.ts` | /settings 路由 |
| 修改 | `frontend/src/components/AppLayout.vue` | 侧边栏入口 |

## T1: ConfigManager

**文件：** `internal/config/manager.go`、`internal/config/manager_test.go`
**依赖：** 无
**步骤：**
1. `ConfigManager` 结构：`path string`、`mu sync.Mutex`、`cur atomic.Pointer[Config]`、`prev *Config`
2. `NewConfigManager(path string, cfg *Config) *ConfigManager`：Store 初始 cfg
3. `Get() *Config`：返回 `cur.Load()`（请求级快照）
4. `Update(newCfg *Config, rebuild func(*Config) error) error`：
   - mutex 串行化
   - 校验 newCfg（枚举/数值范围/策略组合——校验函数从 config 包导出或内置）
   - `rebuild(newCfg)` 试构建 → 失败返回 error（不替换）
   - 写配置文件（临时文件 + rename 原子写）→ 失败返回
   - `prev = cur.Load()`；`cur.Store(newCfg)`
5. `Current() *Config`：只读查询
6. manager_test：Get 返回当前快照；Update 成功后 Get 新快照；rebuild 失败不替换；写文件失败不替换；并发 Get 始终完整快照（race）

**验证：** `go test ./internal/config/ -run TestConfigManager -v` 全绿

## T2: RuntimeComponents 重建

**文件：** `internal/app/rebuild.go`
**依赖：** T1（用 Config 类型）
**步骤：**
1. `RuntimeComponents` 结构：`LLM llm.LLM`、`Embedder embedding.Embedder`、`Retriever retriever.Retriever`、`Engine rag.Engine`
2. `BuildRuntime(cfg *config.Config, vs vectorstore.VectorStore, bm25 retriever.BM25Index, history rag.HistoryStore) (*RuntimeComponents, error)`：
   - `llm.NewLLM(cfg.LLM)`、`embedding.NewEmbedder(cfg.Embedder)`、`reranker.NewReranker(cfg.Reranker)`、`retriever.NewRetriever(cfg.Retriever, emb, vs, bm25, rr)`、`rag.NewEngine(cfg.RAG, llmClient, rt, history, emb)`
   - 任一失败返回 error（调用方回滚）
3. `App` 结构（app.go）加 `components atomic.Pointer[RuntimeComponents]`、`cfgMgr *config.ConfigManager`

**验证：** `go build ./internal/app/...` 通过

## T3: engine 请求级快照

**文件：** `internal/rag/engine.go`
**依赖：** 无（config 已有）
**步骤：**
1. `AskOptions` 加 `CfgSnapshot *config.Config` 字段（请求级配置快照）
2. 新增 `WithConfigSnapshot(cfg *config.Config) AskOption`
3. `effective(o)` 改造：若 `o.CfgSnapshot != nil`，全局默认从 `o.CfgSnapshot.RAG` 读（替代 `e.cfg`）；否则用 `e.cfg`（兼容现状）
4. `prepare` 内 `e.cfg.HistoryLimit` 等读 `e.cfg` 的改为从快照读（用 `o.CfgSnapshot` 或 `e.cfg`）
5. 确保单测现有行为不变（未提供快照时用构建默认）

**验证：** `go build ./internal/rag/...` + `go test ./internal/rag/` 通过

## T4: API handler_config

**文件：** `internal/api/handler_config.go`、`internal/api/middleware.go`、`internal/api/router.go`
**依赖：** T1、T2
**步骤：**
1. `ConfigUpdateRequest` 白名单 DTO（LLM/Embedder/Retriever/Reranker/RAGStrategy/Loader 可修改字段）
2. `GET /config`：从 cfgMgr.Current() 构造分组视图（可修改组当前值 + 只读组当前值 + 需重启标记）
3. `PUT /config`：bootstrap key 校验 → 合并 DTO 到当前快照 → `cfgMgr.Update(newCfg, rebuild)` → 返回新视图；普通 key 403
4. middleware：区分 bootstrap key 权限（`Auth` 中已校验 key，新增「是否 bootstrap」判断供 handler 用）
5. router：`v1.GET("/config")`、`v1.PUT("/config")`

**验证：** `go build ./internal/api/...` 通过

## T5: app 装配改造

**文件：** `internal/app/app.go`
**依赖：** T2、T4
**步骤：**
1. `New`：构建初始 RuntimeComponents → `components.Store(rt)`；创建 `cfgMgr`（初始 cfg）
2. `App` 暴露 `ConfigManager()` 供 handler 使用；`RebuildComponents()` 内部方法（Update 的 rebuild 回调）
3. router Dependencies 加 `CfgMgr *config.ConfigManager`、`Components *atomic.Pointer[RuntimeComponents]`
4. handler_config 的 rebuild 回调：`BuildRuntime(newCfg, vs, bm25, history)` → 成功则 `components.Store(新)`；handler 读取 components 供请求用

**验证：** `go build ./...` + `go test ./internal/app/` 通过

## T6: API 测试

**文件：** `internal/api/api_test.go`
**依赖：** T4、T5
**步骤：**
1. fake 环境支持 config GET/PUT（fake cfgMgr 或真实 manager + 临时文件）
2. 用例：
   - `TestConfigGet`：GET /config 返回分组视图，可修改组含当前值
   - `TestConfigPutBootstrap`：bootstrap key PUT 成功 → 视图更新
   - `TestConfigPutForbidden`：普通 key PUT → 403
   - `TestConfigPutInvalid`：非法配置（temperature=5）PUT → 400
3. 现有测试不回归

**验证：** `go test ./internal/api/` 通过

## T7: 前端配置页

**文件：** `frontend/src/api/config.ts`、`frontend/src/api/types.ts`、`frontend/src/views/SettingsView.vue`、`frontend/src/router/index.ts`、`frontend/src/components/AppLayout.vue`
**依赖：** T4（API）
**步骤：**
1. `types.ts`：`ConfigView`、`ConfigUpdateRequest` 类型
2. `config.ts`：`getConfig()`、`updateConfig(patch)`（带 bootstrap key）
3. `SettingsView.vue`：配置管理页——可修改组表单（LLM 温度/模型/超时、Embedding 模型/base_url、Retriever top_k/权重、Reranker、RAG strategy 六开关、Loader 阈值）+ 保存；只读组展示（Postgres/VectorStore/Chunker/Server + 需重启标记）
4. `router/index.ts`：`/settings` 路由
5. `AppLayout.vue`：侧边栏加「设置」入口

**验证：** `cd frontend && npm run build` 通过

## T8: 全量验证 + 冒烟

**文件：** 无新增
**依赖：** T1-T7
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿；`cd frontend && npm run build` + `npm test` 通过
2. 冒烟：启动服务 → GET /config 返回视图 → bootstrap key PUT 修改 LLM 温度 → 后续问答日志显示新配置 → 普通 key PUT 403
3. 快照验证：慢请求期间 PUT 修改配置 → 已执行请求旧配置、新请求新配置（观察日志）

**验证：** 上述命令全绿；冒烟日志符合预期

## 执行顺序

```
T1 → T2 → T3 ─┐
              ├→ T4 → T5 → T6 → T7 → T8
T1→T2（并行 T3）┘
```

T1（ConfigManager）先；T2 依赖 T1；T3 依赖 config（可并行）；T4 依赖 T1+T2；T5 依赖 T2+T4；T6 依赖 T4+T5；T7 依赖 T4；T8 收尾。
