# WebUI 配置管理界面 Plan

## 架构概览

新增**配置管理子系统**，分三层：

1. **ConfigManager（核心）**：持有运行时配置的原子快照，支持读取当前快照、原子替换新快照、持久化写文件、失败回滚。
2. **API 层**：`GET /api/v1/config`（读）、`PUT /api/v1/config`（写，bootstrap key 校验）——返回/接收分组配置。
3. **前端层**：`SettingsView.vue` 配置管理页（可修改组表单 + 只读组展示），路由 `/settings`。

### 关键设计：请求级配置快照（snapshot）

- **ConfigManager 持有一个原子指针**（`atomic.Pointer[config.Config]`），`Get()` 返回当前快照（不可变副本），`Update(newCfg)` 原子替换指针。
- **每个请求进入时**（handler 层）调用 `cfgMgr.Get()` 取一份快照，**贯穿整个 RAG pipeline**（检索/策略/生成）使用该快照——不重新读 ConfigManager。
- 管理员 `PUT /config` 更新 → `ConfigManager.Update` 原子替换 → **新请求 Get() 拿到新快照**；已开始执行的请求持旧快照引用，不受影响。
- **RAGEngine 改造**：当前 `NewEngine(cfg.RAG, ...)` 绑定一份配置。改为 engine 内部不持有可变配置，而是**每次 Ask/StreamAsk 接收 config 快照参数**（或通过 handler 传入的快照初始化 engine 上下文）。

### 关键设计：热重载组件重建

可修改配置影响以下运行时组件（在 ConfigManager.Update 时重建）：
- `LLM 客户端`（llm.NewLLM）
- `Embedder`（embedding.NewEmbedder）
- `Retriever`（retriever.NewRetriever，含 Reranker）
- `Reranker`（reranker.NewReranker）

**重建策略**：Update 先**试构建**新组件（不替换旧组件）→ 全部成功才原子替换 → 失败回滚（保留旧组件）。这样重载失败不影响运行中服务（AC6）。

启动级组件（vectorstore、BM25、store、worker）不重建——这些配置（Postgres/VectorStore/Worker）在 UI 只读展示。

## 核心数据结构

### config.ConfigManager（internal/config/manager.go 新建）
```go
// ConfigManager 运行时配置管理器：原子快照 + 持久化 + 回滚
type ConfigManager struct {
    path  string              // 配置文件路径
    mu    sync.Mutex          // 保护 Update 的写操作（试构建+替换+写文件 串行）
    cur   atomic.Pointer[config.Config] // 当前生效快照
    prev  *config.Config      // 上一份有效快照（回滚用）
}

func NewConfigManager(path string, cfg *config.Config) *ConfigManager
func (m *ConfigManager) Get() *config.Config        // 返回当前快照（请求级用，只读）
func (m *ConfigManager) Update(newCfg *config.Config, rebuild func(*config.Config) error) error
    // 1. 校验 newCfg（枚举/数值范围/策略组合）
    // 2. rebuild(newCfg) 试构建新组件 → 失败返回（不替换）
    // 3. 写配置文件（持久化）→ 失败返回
    // 4. 原子替换 cur ← newCfg；prev ← 旧 cfg
func (m *ConfigManager) Current() *config.Config    // 当前快照（只读查询）
```

### 可修改配置 DTO（internal/api/handler_config.go）
```go
// ConfigUpdateRequest 可修改字段（白名单）
type ConfigUpdateRequest struct {
    LLM         *config.LLMConfig       `json:"llm,omitempty"`
    Embedder    *config.EmbedderConfig  `json:"embedder,omitempty"`
    Retriever   *config.RetrieverConfig `json:"retriever,omitempty"`
    Reranker    *config.RerankerConfig  `json:"reranker,omitempty"`
    RAGStrategy *config.StrategyConfig  `json:"rag_strategy,omitempty"`
    Loader      *config.LoaderConfig    `json:"loader,omitempty"`
}
// 响应：完整配置视图（可修改组 + 只读组 + 标记）
```

## 模块设计

### ConfigManager（internal/config/manager.go）
**职责：** 原子快照、更新、回滚、持久化。
**对外接口：** `NewConfigManager`、`Get`、`Update`、`Current`。
**依赖：** config 包（无外部依赖）。
**实现要点：** `atomic.Pointer[config.Config]` 保证 `Get` 无锁；`Update` 用 mutex 串行化（校验→试构建→写文件→替换）。

### 组件重建（internal/app/rebuild.go 或 app.go 扩展）
**职责：** 按新配置重建 LLM/Embedder/Retriever/Reranker。
```go
// RuntimeComponents 可热重建组件
type RuntimeComponents struct {
    LLM     llm.LLM
    Embedder embedding.Embedder
    Retriever retriever.Retriever
    Engine  rag.Engine  // 用新组件+新 RAG 配置构建
}
// BuildRuntime(cfg *config.Config, vs vectorstore.VectorStore, bm25 retriever.BM25Index, history rag.HistoryStore) (*RuntimeComponents, error)
```
`App` 持有 `components atomic.Pointer[RuntimeComponents]`；`Update` 时 `BuildRuntime` 试构建 → 成功替换。

### RAGEngine 请求级快照
**改造：** `NewEngine` 签名不变（构建时绑定初始配置）；但 engine 内 `cfg.RAG` 改为**从请求快照读取**。方案：
- `Ask(ctx, sessionID, question, opts...)` 已接收 opts——新增 `WithConfigSnapshot(cfg *config.Config)` AskOption，handler 层 `cfgMgr.Get()` 后传入。
- engine 内部 `effective(o)` 从 `o.CfgSnapshot` 读 RAG 配置（替代 `e.cfg`），未提供时用构建时的默认（兼容现状）。

### API（internal/api/handler_config.go、router.go）
**职责：** 读/写配置接口。
- `GET /api/v1/config`：返回分组配置视图（可修改组含当前值、只读组含当前值+需重启标记）
- `PUT /api/v1/config`：接收 ConfigUpdateRequest（白名单字段），合并到当前快照 → 校验 → 重建 → 写文件 → 替换；**要求 bootstrap key**（Auth 中间件扩展：bootstrap key 允许写，普通 key 403）
- router：注册 `v1.GET("/config")`、`v1.PUT("/config")`

### 前端（frontend/src/）
- `api/config.ts`：getConfig / updateConfig
- `types.ts`：ConfigView 类型
- `views/SettingsView.vue`：配置管理页——可修改组表单（LLM/Embedding/Retriever/Reranker/Strategy/Loader）+ 保存；只读组展示（Postgres/VectorStore/Chunker/Server + 需重启标记）
- `router`：`/settings` 路由 + 侧边栏入口

## 模块交互

```
管理员 PUT /api/v1/config (bootstrap key)
  → handler 校验白名单字段 → 合并到当前快照
  → cfgMgr.Update(newCfg, rebuild)
      ├─ 校验（枚举/范围/策略组合）
      ├─ BuildRuntime(newCfg) 试构建 → 失败 return（不替换）
      ├─ 写 config 文件 → 失败 return
      └─ 原子替换 cur；prev ← 旧
用户 GET /api/v1/chat
  → handler: snap := cfgMgr.Get()          ← 请求级快照
  → engine.Ask(..., WithConfigSnapshot(snap))
      └─ 整个 pipeline 用 snap（策略/检索/生成）
  → 若期间配置被更新：本请求仍用 snap（旧），新请求用新快照
```

## 文件组织

```
internal/config/
├── manager.go            — 新建：ConfigManager（原子快照/更新/回滚/持久化）
├── manager_test.go       — 新建：Get/Update/回滚/并发快照测试
internal/app/
├── rebuild.go            — 新建：RuntimeComponents + BuildRuntime（重建 LLM/Embedder/Retriever/Reranker/Engine）
├── app.go                — 修改：App 持 components atomic.Pointer + cfgMgr
internal/rag/
├── engine.go             — 修改：WithConfigSnapshot AskOption；effective 从快照读
internal/api/
├── handler_config.go     — 新建：GET/PUT /config + 分组视图
├── middleware.go         — 修改：bootstrap key 写权限
├── router.go             — 修改：注册 config 路由
├── api_test.go           — 修改：config 读写用例
frontend/src/
├── api/config.ts         — 新建
├── api/types.ts          — 修改：ConfigView
├── views/SettingsView.vue — 新建：配置管理页
├── router/index.ts       — 修改：/settings
├── components/AppLayout.vue — 修改：侧边栏入口
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 快照机制 | atomic.Pointer[config.Config] + 请求级 Get | 无锁读、原子替换；每请求取一次贯穿 pipeline（N2/N3） |
| Update 串行化 | mutex 包住校验→试构建→写文件→替换 | 避免并发写竞态；读不受影响（atomic） |
| 失败回滚 | 先试构建新组件成功才替换 | AC6：失败不影响运行中服务 |
| 请求注入 | WithConfigSnapshot AskOption | handler 取快照传入 engine；未提供时用构建默认（兼容） |
| 重建范围 | LLM/Embedder/Retriever/Reranker/Engine | 可修改配置影响的组件；向量存储/BM25/store 不动 |
| 写权限 | bootstrap key 才可 PUT | N1：避免普通 key 篡改 |
| 校验复用 | ValidateStrategy + 新增数值/枚举校验 | 单一校验路径 |
| 白名单 DTO | ConfigUpdateRequest 仅含可修改字段 | 防越权改启动级配置 |
| 持久化 | 写回配置文件（原子写：临时文件+rename） | N4：重启仍生效；避免写一半 |
