# MCP Server（只读 RAG 能力）Plan

## 架构概览

在现有服务同进程内新增 `internal/mcp` 包，作为 MCP Server（streamable HTTP）。请求链路：

```
外部 MCP 客户端（Claude/其他 Agent 平台）
    │  POST /mcp  (Authorization: Bearer <API Key>)
    ▼
gin 路由 /mcp ──→ mcp 认证层（net/http 包装）
    │              · 解析 Bearer → SHA-256 → 查 api_keys → enabled 校验
    │              · 失败 → HTTP 401（不进入 JSON-RPC）
    │              · 成功 → 身份与权限写入 request context
    │              · bootstrap 不作 MCP 凭证、不绕过权限（D6）
    ▼
mark3labs streamable HTTP Server（mcp-go v0.57.0）
    │  initialize / tools/list / tools/call
    ▼
Tool handler（permission 检查 → store/retriever/engine → 投递审计）
```

四个组件：
1. **认证层**（`auth.go`）：MCP 专用认证，只接受 API Key（spec F3），复用现有 SHA-256 查库与 `enabled` 校验；**不识别 bootstrap 特殊语义**，一律按 Key 的权限字段授权（默认空 = 无 MCP 权限，见技术决策 D6）。
2. **权限层**（`permission.go`）：从 `api_keys` 权限字段解析 Tool 白名单与知识库范围；提供 Tool 授权检查与知识库范围解析（spec F4/F5）。
3. **Tool 层**（`tools.go`）：6 个只读 Tool，调用现有 Store / Retriever / RAG Engine。
4. **审计层**（`audit.go`）：每个 Tool 调用结束后将审计事件投递到 buffered channel，由后台 worker 异步写入数据库（截断参数 + 原始长度，spec F7；见技术决策 D8）。

## 核心数据结构

### store.APIKey（扩展 3 字段）

```go
type APIKey struct {
    ID         string
    Name       string
    KeyHash    string
    Enabled    bool
    LastUsedAt *time.Time
    CreatedAt  time.Time
    // —— 以下为 MCP 权限（新增）——
    MCPTools    []string // 允许调用的 MCP Tool 白名单；空 = 无任何 MCP 权限
    MCPKBScope  string   // ""（无 MCP 知识库权限）| "all"（全部）| "allowlist"（仅 MCPKBIDs）
    MCPKBIDs    []string // MCPKBScope=="allowlist" 时的知识库白名单
}
```

### store.APIKeyPermissions（更新请求体）

```go
type APIKeyPermissions struct {
    MCPTools   []string // nil = 不修改；空数组 = 清空
    MCPKBScope string   // "" | "all" | "allowlist"
    MCPKBIDs   []string // nil = 不修改
}
```

### store.AuditLog（审计记录）

```go
type AuditLog struct {
    ID           int64
    APIKeyID     string // 仅 Key ID 引用，绝不存 Secret
    ToolName     string
    Params       string // 截断后参数 JSON（默认 ≤2000 字符）
    ParamsLen    int    // 原始参数长度（截断前，审计容量评估用）
    Status       string // "success" | "error"
    ErrorMessage string
    DurationMS   int64
    CreatedAt    time.Time
}
```

### mcp 包内身份与权限（request context 传递）

```go
// keyCtx 认证后写入 request context
type keyCtx struct {
    KeyID string // 仅 Key ID；不含 Secret/Authorization Token
    Scope KBPermission // 解析后的知识库权限（空 = 无 MCP 知识库权限）
}

// KBPermission 知识库权限（三态）
type KBPermission struct {
    All bool     // scope == "all"
    IDs []string // scope == "allowlist" 时的白名单
}

// Scope 授权检查
func (p KBPermission) CanAccess(kbID string) bool
// Resolve 把请求级 kb_id（可空）解析为检索范围 IDs：
//   指定 kb_id → 校验在可访问范围内（否则 ErrPermissionDenied）→ [kbID]
//   未指定 + All  → nil（表示不过滤全部）
//   未指定 + 白名单 → 白名单 IDs
//   未指定 + 无权限  → ErrPermissionDenied
func (p KBPermission) Resolve(reqKBID string) ([]string, error)
```

### store.Store 接口扩展

```go
// 知识库
ListKBsByIDs(ctx context.Context, ids []string) ([]KnowledgeBase, error) // 白名单查询
// API Key 权限
UpdateAPIKeyPermissions(ctx context.Context, id string, p APIKeyPermissions) error
// 审计
AppendAuditLog(ctx context.Context, log AuditLog) error
```

## 模块设计

### internal/mcp

**职责：** MCP Server 装配、认证、授权、6 个 Tool 实现、审计。
**对外接口：**

```go
// Dependencies MCP 层依赖
type Dependencies struct {
    Config config.ServerConfig
    Store  store.Store
    Engine func() rag.Engine      // 复用 app 的 EngineProvider（热重载）
    RT     retriever.Retriever    // retrieve Tool 用（与 RAG 引擎同实例）
    CfgMgr *config.ConfigManager  // 配置快照（热重载一致性）
    Audit  *AuditSink             // 异步审计（app 装配并管理生命周期）
}

// NewHandler 创建 MCP HTTP handler（含认证，认证失败返回 HTTP 401）
func NewHandler(deps Dependencies) http.Handler
```

**依赖：** store、rag、retriever、config、mcp-go、gin（仅挂载方）。

#### audit.go（异步审计，D8）

```go
// AuditSink 异步审计：buffered channel + 后台 worker。
// Submit 非阻塞投递（队列满仅 warn，不阻塞 MCP 主请求）；
// Shutdown 停止接收 → flush 剩余 → 退出 worker（防 goroutine 泄漏，由 App 管理生命周期）。
type AuditSink struct { /* ch chan AuditLog; done chan struct{}; ... */ }

func NewAuditSink(s store.Store, bufSize, paramLimit int) *AuditSink
func (s *AuditSink) Submit(log AuditLog)          // 截断 + 投递；失败仅 warn
func (s *AuditSink) Shutdown(ctx context.Context) // flush + 退出
```

### internal/store

- `apikey.go`：`APIKey` 增加 3 字段；`CreateAPIKey`/`ListAPIKeys`/`GetAPIKeyByHash` SQL 补列；新增 `UpdateAPIKeyPermissions`。
- `audit.go`（新文件）：`AppendAuditLog` 插入 `mcp_audit_logs`。
- `kb.go`：新增 `ListKBsByIDs`（`WHERE id = ANY($1)`）。
- `schema.go`：追加迁移——`api_keys` 3 列（默认空 = 历史 Key 无 MCP 权限）+ `mcp_audit_logs` 表。

### internal/api（REST 管理扩展）

- `handler_key.go`：`keyView` 增加 `MCPTools`/`MCPKBScope`/`MCPKBIDs`；新增 `UpdateAPIKeyPermissions`（`requireSystemKey` 保护，仅系统级 Key）。
- `router.go`：`v1.PUT("/api-keys/:id/permissions", h.UpdateAPIKeyPermissions)`。

### internal/config

```go
type MCPConfig struct {
    Enabled          bool   `yaml:"enabled"`            // 默认 false（安全默认，显式开启）
    Path             string `yaml:"path"`               // 默认 "/mcp"
    AuditParamLimit  int    `yaml:"audit_param_limit"`  // 默认 2000 字符
}
// ServerConfig 增加 MCP MCPConfig `yaml:"mcp"`
```

### internal/app

- `app.go`：`router` 构建后，若 `cfg.Server.MCP.Enabled` 则 `router.Any(cfg.Server.MCP.Path, gin.WrapH(mcp.NewHandler(...)))`。

## 模块交互

```
MCP 客户端
  → POST /mcp
  → mcp 认证层：401（认证失败） | 通过 → context 写入 keyCtx
  → 授权网关层（server.go gateway）：解析 tools/call body → Tool/KB/Task 授权检查 → 越权直接写 -32001
  → mark3labs server 分发 tools/call（handler 内仅防御性 isError 兜底 + 业务 + 审计）
  → tool handler：
      1. 解析知识库范围（list/get 校验 CanAccess；retrieve/ask 用 Resolve，网关层已拦截越权）
      2. 调 store/retriever/engine
      3. auditSink.Submit（异步投递；队列满/失败仅 warn，不阻塞）
  → JSON-RPC 响应
```

> **实现说明（探针结论 T1）**：mcp-go v0.57.0 的 `tools/call` handler 返回 error 固定映射 `-32603`，无法经 handler/middleware 返回自定义错误码。因此授权失败（Tool/KB/Task 越权）在**网关层**解析 JSON-RPC body 后直接构造 `-32001` 响应（spec F4/F5/F8）；handler 内不重复授权拒绝，仅保留防御性 isError 兜底。认证失败保持 HTTP 401（不进入 JSON-RPC）。

## 文件组织

```
docs/26-mcp-server/
├── spec.md
└── plan.md

internal/mcp/
├── server.go        — NewHandler、tool 注册（6 个）、gin 挂载适配
├── auth.go          — Bearer 解析 → SHA-256 查库 → enabled → keyCtx（bootstrap 不特殊处理）
├── permission.go    — KBPermission、Tool 白名单检查
├── tools.go         — 6 个 Tool handler
├── audit.go         — AuditSink：buffered channel + 后台 worker + Shutdown flush
└── errors.go        — ErrPermissionDenied(-32001) 等错误码与类型

internal/store/
├── apikey.go        — 3 字段 + UpdateAPIKeyPermissions
├── audit.go         — 新增（AppendAuditLog）
├── kb.go            — 新增（ListKBsByIDs）
└── schema.go        — 迁移（3 列 + mcp_audit_logs 表）

internal/api/
├── handler_key.go   — keyView 扩展 + UpdateAPIKeyPermissions
└── router.go        — PUT /api/v1/api-keys/:id/permissions

internal/config/config.go — MCPConfig + applyDefaults
internal/app/app.go        — 挂载 /mcp（Enabled 时）；审计 worker 生命周期（Close 时 Shutdown flush）
```

## Tool 规格

| Tool | 参数 | 数据来源 | 返回 |
|------|------|----------|------|
| `list_knowledge_bases` | 无 | `ListAllKBs` / `ListKBsByIDs` | `[{id,name,description,strategy,created_at,updated_at}]` |
| `get_knowledge_base` | `kb_id`（必填） | `GetKB` + `CanAccess` | 知识库详情；不存在/越权 → ErrPermissionDenied「知识库不存在或无权限」 |
| `retrieve` | `query`（必填）、`kb_id`（可选）、`top_k`（可选） | `retriever.Search`（带 kb filter） | `[{id,content,score,metadata:{filename,kb_id}}]` |
| `ask` | `question`（必填）、`kb_id`（可选）、`session_id`（可选） | `engine.Ask`（WithKBID/WithKBIDs + 配置快照） | `{answer,sources}`（不含 thinking） |
| `list_documents` | `kb_id`（可选） | `ListDocuments`（遍历可访问 KB） | `[{id,kb_id,filename,format,size,status,created_at}]` |
| `get_task` | `task_id`（必填） | `GetTask` + `CanAccess(task.KBID)` | `{id,kb_id,document_id,status,retry_count,error_message,created_at,updated_at}` |

`ask` 的 `session_id` 为可选透传：直接复用现有 `engine.Ask` 的 session 语义（现有 Engine 已支持），缺省生成新 uuid（无历史）；**不为 MCP 新建任何 session/history 存储**。`retrieve`/`ask` 未指定 `kb_id` 时按 `KBPermission.Resolve` 自动使用可访问范围。

## 错误语义

| 场景 | HTTP | JSON-RPC |
|------|------|----------|
| 认证失败（缺失/无效/停用 Key） | **401**（直接拒绝，不进入 JSON-RPC） | — |
| 授权失败（Tool 白名单外 / 知识库越权 / 任务越权） | 200 | error code `-32001`（ErrPermissionDenied） |
| 参数不合法 | 200 | error code `-32602`（InvalidParams，库内置） |
| 内部错误 | 200 | error code `-32603`（InternalError，库内置） |

> 说明：MCP 协议层无 HTTP 403 概念；「403 = 授权失败」在 JSON-RPC error 层表达（code -32001，消息明确）。认证失败保持 HTTP 401 语义。越权与不存在统一返回同一错误与消息（不泄露存在性）。

## 技术决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| D1 | MCP 库 | mark3labs/mcp-go v0.57.0 | 社区主流；streamable HTTP（2025-03-26 兼容）；gin 集成路径文档明确（`gin.WrapH`）；自定义 JSON-RPC 错误码一等支持 |
| D2 | 传输 | streamable HTTP（单端点 `/mcp`） | 用户指定；库实现 2025-11-25 协议并向后兼容 2025-03-26 |
| D3 | 认证实现 | 自写认证层（复用 SHA-256 查库） | MCP 只接受 API Key；现有 `api.Auth` 中间件含 JWT 分支且绑定 gin.Context，MCP 用独立实现更干净 |
| D4 | 授权失败表达 | JSON-RPC error `-32001` | MCP 协议层无 403；语义区分「认证 401 vs 授权 403」通过 HTTP/JSON-RPC 分层实现 |
| D5 | 权限存储 | `api_keys` 3 新列，默认空 | 历史 Key 迁移后无任何 MCP 权限（spec F6）；不建关联表，查询简单 |
| D6 | bootstrap Key | 不作 MCP 调用凭证、不绕过 MCP 权限：仅用于系统级 REST 管理接口（管理/授予 API Key 的 MCP 权限）；MCP 认证层不识别 bootstrap，一律按权限字段授权（默认空 = 无 MCP 权限） | 职责单一：bootstrap 只管系统级管理，MCP 访问统一走正常认证与授权流程 |
| D7 | MCP 默认开关 | `mcp.enabled` 默认 false | 安全默认；显式开启后才暴露 `/mcp` |
| D8 | 审计写入 | 异步：buffered channel + 后台 worker 写入；队列满/失败仅 warn，不阻塞主请求；Shutdown 时 flush | 审计不得阻塞 MCP 主请求（N3）；worker 生命周期由 App 管理，避免 goroutine 泄漏 |
| D9 | 参数截断 | 参数 JSON 序列化后截断至 `audit_param_limit`（默认 2000），同时记录原始参数长度 | 防敏感内容与超大参数入库（N4）；保留原始长度便于容量评估 |
| D10 | ask 会话 | 支持可选 `session_id`，缺省生成新会话 | 无状态 MCP 调用友好，历史能力可选用 |

## Spec 覆盖

| Spec | 落点 |
|------|------|
| F1 streamable HTTP 同进程 | D1/D2 + app.go 挂载 |
| F2.1–F2.6 六个 Tool | tools.go 规格表 |
| F3 API Key 认证 401 | auth.go（HTTP 401） |
| F4 Tool 白名单 403 | permission.go + tools.go 检查 |
| F5 知识库范围 403 | KBPermission.Resolve/CanAccess |
| F6 管理接口 + 历史 Key 不自动授权 | handler_key.go + schema 迁移（默认空） |
| F7 审计（api_key_id、截断、无 Secret） | audit.go（AuditSink 异步投递）+ mcp_audit_logs |
| F8 get_task 资源权限校验 | tools.go get_task（CanAccess） |
| N1 复用 | Dependencies 复用 Store/Engine/RT/CfgMgr |
| N2 不泄露存在性 | 越权与不存在统一错误 |
| N3 审计不阻塞 | D8 |
| N4 截断 | D9 |
| N5 认证无外部网络 | auth.go 仅本地查库 |
| N6 RateLimit | `/mcp` 挂在既有 gin 中间件链（Logger/CORS/RateLimit）之后 |
