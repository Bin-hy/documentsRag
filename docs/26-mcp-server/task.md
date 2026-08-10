# MCP Server（只读 RAG 能力）Tasks

> 依据：已批准的 spec.md + plan.md。严格按 plan 范围，不扩大。

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建（临时） | `/tmp/mcp-probe/` | mcp-go 行为探针（不入仓库，T1 用完即弃） |
| 修改 | `internal/config/config.go` | `MCPConfig` + 默认值 |
| 修改 | `configs/config.yaml` | `mcp` 配置节示例（enabled: false） |
| 修改 | `internal/store/schema.go` | api_keys 3 列 + mcp_audit_logs 表（幂等） |
| 修改 | `internal/store/apikey.go` | APIKey 3 字段、SQL 补列、UpdateAPIKeyPermissions |
| 修改 | `internal/store/kb.go` | ListKBsByIDs（allowlist 查询） |
| 新建 | `internal/store/audit.go` | AppendAuditLog |
| 修改 | `internal/store/store.go` | Store 接口 + 3 方法 |
| 修改 | `internal/store/*_test.go` | pgxmock 测试 |
| 新建 | `internal/mcp/errors.go` | 错误码与统一消息 |
| 新建 | `internal/mcp/permission.go` | KBPermission / ToolAllowed |
| 新建 | `internal/mcp/auth.go` | Bearer 认证 → HTTP 401 / keyCtx |
| 新建 | `internal/mcp/audit.go` | AuditSink（channel + worker + Shutdown） |
| 新建 | `internal/mcp/tools.go` | 6 个 Tool handler |
| 新建 | `internal/mcp/server.go` | NewHandler、mcp-go 初始化、注册 |
| 新建 | `internal/mcp/*_test.go` | mcp 包单元/集成测试 |
| 修改 | `internal/app/app.go` | 挂载 /mcp + AuditSink 生命周期 |
| 修改 | `internal/api/handler_key.go` | keyView 扩展 + UpdateAPIKeyPermissions |
| 修改 | `internal/api/router.go` | PUT /api-keys/:id/permissions |
| 修改 | `internal/api/api_test.go`（fakeStore） | 补 Store 新方法 + 管理接口测试 |

## T1: mcp-go 行为探针（先行）

**文件：** `/tmp/mcp-probe/`（临时，不入仓库）
**依赖：** 无
**步骤：**
1. `go get github.com/mark3labs/mcp-go`（确认版本与 go.mod 兼容）
2. 最小 streamable HTTP server：`server.NewServer` + `NewStreamableHTTPServer`，注册 1 个 tool，handler 返回一个实现 `JSONRPCError() JSONRPCError` 接口的自定义 error（code -32001）
3. 用 HTTP 客户端模拟 `initialize` → `tools/list` → `tools/call`，打印原始 JSON-RPC 响应
4. **记录结论**：`tools/call` handler 返回 error 时 SDK 的实际 JSON-RPC 映射（isError 结果 vs JSON-RPC error）；自定义 -32001 是否可行；同时确认未认证请求的行为

**验证：** 探针输出展示 tools/call 响应体；结论写入本任务结果，作为 T6 的实现依据（若 SDK 不支持自定义 error code，T6 改用「isError 结果 + 统一消息」回退方案，错误语义不变）

## T2: config 扩展 MCPConfig

**文件：** `internal/config/config.go`、`internal/config/config_test.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. 定义 `MCPConfig{Enabled bool; Path string; AuditParamLimit int}`（yaml 标签 `enabled/path/audit_param_limit`）
2. `ServerConfig` 增加 `MCP MCPConfig \`yaml:"mcp"\``
3. `applyDefaults`：`Path` 空 → `"/mcp"`；`AuditParamLimit` ≤0 → `2000`；**Enabled 零值即 false，不显式赋值**（安全默认，D7）
4. `configs/config.yaml` 加 `server.mcp` 节示例（`enabled: false` 注释说明需显式开启）

**验证：** `go build ./internal/config/...`；config_test 断言默认 `Path=/mcp`、`AuditParamLimit=2000`、`Enabled=false`

## T3: store schema 迁移

**文件：** `internal/store/schema.go`、`internal/store/schema_test.go`（新增）
**依赖：** 无
**步骤：**
1. api_keys 追加 3 列（幂等 `ADD COLUMN IF NOT EXISTS`）：
   - `mcp_tools TEXT[] NOT NULL DEFAULT '{}'`
   - `mcp_kb_scope TEXT NOT NULL DEFAULT ''`
   - `mcp_kb_ids TEXT[] NOT NULL DEFAULT '{}'`
2. 新建 `mcp_audit_logs` 表（`CREATE TABLE IF NOT EXISTS`）：`id BIGSERIAL PRIMARY KEY`、`api_key_id TEXT NOT NULL`、`tool_name TEXT NOT NULL`、`params TEXT NOT NULL DEFAULT ''`、`params_len INT NOT NULL DEFAULT 0`、`status TEXT NOT NULL`、`error_message TEXT NOT NULL DEFAULT ''`、`duration_ms BIGINT NOT NULL DEFAULT 0`、`created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
3. `Migrate` 追加两个迁移语句（在既有迁移之后）
4. **历史 Key 默认无 MCP 权限**：新列默认空 → 迁移后历史 Key `mcp_tools='{}'`、`mcp_kb_scope=''`，即无任何 MCP 权限（spec F6 / 用户约束 4）

**验证：** pgxmock 测试 `Migrate` 执行了预期的 ALTER/CREATE 语句；**migration 重复执行不报错**（幂等性由 `IF NOT EXISTS` 保证，测试断言 DDL 含幂等关键字 + checklist 用真实 PG 双跑验证）

## T4: store APIKey 权限字段

**文件：** `internal/store/apikey.go`、`internal/store/apikey_test.go`（扩展）
**依赖：** T3
**步骤：**
1. `APIKey` 结构加 `MCPTools []string`、`MCPKBScope string`、`MCPKBIDs []string`
2. `CreateAPIKey` / `ListAPIKeys` / `GetAPIKeyByHash` SQL 补 3 列（INSERT/SELECT 列对齐）
3. 新增 `UpdateAPIKeyPermissions(ctx, id, APIKeyPermissions)`：`UPDATE api_keys SET mcp_tools=$2, mcp_kb_scope=$3, mcp_kb_ids=$4 WHERE id=$1`；`APIKeyPermissions` 的 nil 语义（nil = 不修改该字段）在 handler 层处理，store 层直接按传入值更新

**验证：** pgxmock 测试：Create/List/GetByHash 列绑定正确（含 `{}` 空数组默认）；UpdateAPIKeyPermissions SQL 与参数正确

## T5: store 其余扩展（KB allowlist / AuditLog / 接口）

**文件：** `internal/store/kb.go`、`internal/store/audit.go`（新）、`internal/store/store.go`、`internal/api/api_test.go`（fakeStore）
**依赖：** T4
**步骤：**
1. `Store` 接口加：`ListKBsByIDs(ctx, ids) ([]KnowledgeBase, error)`、`UpdateAPIKeyPermissions(...)`、`AppendAuditLog(ctx, AuditLog) error`
2. `ListKBsByIDs`：`SELECT ... FROM knowledge_bases WHERE id = ANY($1) ORDER BY created_at DESC`（allowlist 显式授权，不过滤 owner）
3. `AppendAuditLog`：INSERT 进 `mcp_audit_logs`（不含 Secret 字段——表结构即保证）
4. task → KB 关联：现有 `GetTask` 已返回 `KBID`，**无需新增 store 方法**（权限校验在 mcp 层完成）
5. **同步补 `internal/api/api_test.go` 的 `fakeStore` 3 个新方法**（否则 api 包编译失败）：ListKBsByIDs 按 map 过滤、UpdateAPIKeyPermissions 更新 keys map、AppendAuditLog 追加到内存 slice

**验证：** `go build ./...`（fakeStore 补齐后全仓编译通过）；pgxmock 测试 ListKBsByIDs（ANY 参数绑定）与 AppendAuditLog

## T6: mcp errors.go（依 T1 结论）

**文件：** `internal/mcp/errors.go`（新）
**依赖：** T1
**步骤：**
1. 定义错误码常量与统一消息：
   - `ErrPermissionDenied`（code -32001，消息「无权限执行该操作」）
   - 知识库消息「知识库不存在或无权限」、任务消息「任务不存在」（越权与不存在统一，不泄露存在性，spec N2）
2. 按 T1 结论实现：SDK 支持自定义 error → 定义实现 `JSONRPCError() JSONRPCError` 的错误类型；不支持 → 定义返回 isError 结果的辅助函数（消息一致，仅传输形态差异）

**验证：** `go build ./internal/mcp/...`；错误类型与 T1 结论一致

## T7: mcp permission.go

**文件：** `internal/mcp/permission.go`（新）`、internal/mcp/permission_test.go`（新）
**依赖：** 无（纯逻辑）
**步骤：**
1. `KBPermission{All bool; IDs []string}`：
   - `CanAccess(kbID)`：All 或 kbID ∈ IDs
   - `Resolve(reqKBID)`：指定 → 校验（越权 ErrPermissionDenied）→ [kbID]；未指定 + All → nil；未指定 + allowlist → IDs；未指定 + 无权限（空 scope）→ ErrPermissionDenied
2. `ToolAllowed(tools []string, name string)`：空 tools → false（无任何 MCP Tool 权限）；否则 name ∈ tools
3. `ParseScope(scope string, ids []string) KBPermission`：`""` → {All:false, IDs:nil}；`"all"` → {All:true}；`"allowlist"` → {All:false, IDs}

**验证：** `go test ./internal/mcp/ -run 'TestPermission|TestResolve|TestToolAllowed'` 覆盖全部三态分支

## T8: mcp auth.go

**文件：** `internal/mcp/auth.go`（新）`、internal/mcp/auth_test.go`（新）
**依赖：** T4（GetAPIKeyByHash 读新字段）、T7（权限解析）
**步骤：**
1. `authenticate(w, r, store) (*keyCtx, bool)`：解析 `Authorization: Bearer` → SHA-256 → `GetAPIKeyByHash` → `Enabled` 校验
2. 缺失 / 无效 / 停用 → **HTTP 401**（JSON body `{"error":"认证失败"}`），不进入 JSON-RPC（spec F3）
3. 成功 → `keyCtx{KeyID, Scope}`（Scope 由权限字段解析）写入 request context
4. **bootstrap 不进入特殊逻辑**：不识别 bootstrap 标记，一律按普通 Key 权限字段授权（plan D6）

**验证：** `go test ./internal/mcp/ -run TestAuth`：401 三分支（缺失/无效/停用）+ 成功分支 + 权限解析正确

## T9: mcp audit.go（AuditSink）

**文件：** `internal/mcp/audit.go`（新）`、internal/mcp/audit_test.go`（新）
**依赖：** T5（AppendAuditLog）
**步骤：**
1. `AuditSink{ch chan AuditLog; done chan struct{}; store store.Store; paramLimit int}`，buffered channel（容量可配，默认 1024）
2. `Submit(log AuditLog)`：**非阻塞**投递（`select { case ch <- log: default: warn 丢弃 }`，不阻塞主请求）；截断 `Params` 至 paramLimit、记录 `ParamsLen`（截断前原始长度，spec N4/用户约束 7）
3. 后台 worker goroutine：`for log := range ch { AppendAuditLog }`，写入失败仅 `slog.Warn`
4. `Shutdown(ctx)`：`close(ch)` → worker 消费完剩余（flush）→ 退出；App 生命周期调用（防 goroutine 泄漏，plan D8）
5. **不记录 Secret/Token**：AuditLog 无 secret 字段（类型即保证）

**验证：** `go test ./internal/mcp/ -run TestAudit -race`：Submit 非阻塞（满队列不 hang）、worker 写入收到（fake store 断言）、Shutdown flush 全部、无 goroutine 泄漏

## T10: mcp tools.go（6 个 Tool）

**文件：** `internal/mcp/tools.go`（新）
**依赖：** T6、T7、T8、T9
**步骤：**
1. 每个 tool 先 `ToolAllowed(keyCtx, name)`（未授权 → ErrPermissionDenied，spec F4），再知识库范围检查
2. `list_knowledge_bases`（无参）：scope.All → `ListAllKBs`；allowlist → `ListKBsByIDs`
3. `get_knowledge_base`（kb_id）：`CanAccess` 校验 → `GetKB` → 详情；不存在/越权统一「知识库不存在或无权限」
4. `retrieve`（query 必填、kb_id/top_k 可选）：`Resolve` → filter → `retriever.Search` → `[{id,content,score,metadata:{filename,kb_id}}]`
5. `ask`（question 必填、kb_id/session_id 可选）：`Resolve` → `engine.Ask`（WithKBID/WithKBIDs + `WithConfigSnapshot(cfgMgr.Get())`；session_id 缺省生成新 uuid；**不传 thinking，不暴露内部推理**，spec F2.4）→ `{answer,sources}`
6. `list_documents`（kb_id 可选）：指定 → 校验 → `ListDocuments`；未指定 → 遍历可访问 KB 的文档
7. `get_task`（task_id 必填）：`GetTask` → **`CanAccess(task.KBID)` 校验**（spec F2.6/F8）→ 状态；不存在/越权统一「任务不存在」
8. 每个 handler 结束 `auditSink.Submit`：tool 名、截断参数、status（success/error）、error_message、duration_ms

**验证：** `go build ./internal/mcp/...`；fake store/retriever/engine 单测覆盖各 tool 成功与越权分支（越权在 T14 集成测试再验证端到端）

## T11: mcp server.go（NewHandler + 注册）

**文件：** `internal/mcp/server.go`（新）
**依赖：** T6、T10
**步骤：**
1. `NewHandler(deps Dependencies) http.Handler`：认证层包装（T8）→ mark3labs `server.NewServer`（Name/Version）→ `NewStreamableHTTPServer`
2. 注册 6 个 tool（inputSchema 按 plan Tool 规格：string/number/boolean 类型）
3. **tools/list 返回全部 6 个已注册 Tool**；未授权拦截在 tools/call 调用层（F4，不按 Key 过滤列表）
4. 从 request context 读取 keyCtx 注入 tool 调用（mcp-go 的 ctx 传递方式按 T1 探针确认）
5. Dependencies 含 `Audit *AuditSink`

**验证：** `go build ./internal/mcp/...`；httptest 冒烟：带有效 Key 完成 initialize → tools/list（6 个）→ tools/call 一个 tool

## T12: app.go 挂载 + AuditSink 生命周期

**文件：** `internal/app/app.go`
**依赖：** T2、T5、T11
**步骤：**
1. `cfg.Server.MCP.Enabled` 为 true 时：创建 `mcp.NewAuditSink(st, 1024, cfg.Server.MCP.AuditParamLimit)` → `mcp.NewHandler` → `router.Any(cfg.Server.MCP.Path, gin.WrapH(handler))`（挂在既有 Logger/CORS/RateLimit 中间件链之后，spec N6）
2. `App` 持有 `*mcp.AuditSink`；`Close()` 时 `Shutdown(ctx)` flush（plan D8）
3. `Engine` provider 复用现有 `components` 原子指针

**验证：** `go build ./...`；config 单测确认 Enabled 默认 false（未显式开启不挂载 /mcp，404 行为在 checklist 集成验证）

## T13: REST API Key 管理接口扩展

**文件：** `internal/api/handler_key.go`、`internal/api/router.go`、`internal/api/api_test.go`
**依赖：** T4、T5
**步骤：**
1. `keyView` 加 `MCPTools`/`MCPKBScope`/`MCPKBIDs`；`ListAPIKeys` 填充（JSON 标签 `mcp_tools/mcp_kb_scope/mcp_kb_ids`）
2. `UpdateAPIKeyPermissions` handler：`requireSystemKey`（仅系统级 Key，spec F6）→ 绑定 `APIKeyPermissions` 请求体（JSON 标签同上；nil 语义：缺省字段不修改）→ `store.UpdateAPIKeyPermissions`
3. router：`v1.PUT("/api-keys/:id/permissions", h.UpdateAPIKeyPermissions)`
4. **现有 REST 行为不受影响**：仅新增字段与新增端点，不改既有端点语义

**验证：** `go build ./...`；api 测试：系统级 Key 可查/改权限；非系统级（会话 JWT）→ 403；ListAPIKeys 返回权限字段

## T14: 测试补齐与全量回归

**文件：** `internal/mcp/*_test.go`、`internal/api/*_test.go`、`internal/store/*_test.go`、`internal/config/config_test.go`
**依赖：** T10、T11、T13 全部
**步骤：**
1. mcp 集成测试（httptest + fake store/retriever/engine）：
   - initialize / tools/list / tools/call 正常流程
   - 401：缺失 / 无效 / 停用 Key
   - Tool 越权：未配置权限 Key 调任何 tool → 拒绝；白名单外 tool → 拒绝
   - KB 越权：allowlist 外 kb_id → 拒绝；`all` 分支可访问全部
   - Task 越权：无权限知识库的任务 → 按不存在处理
   - **历史 Key**：无权限字段 → 无任何 MCP 权限（spec F6/用户约束 4）
   - 审计：成功/失败调用各产生一条记录（api_key_id、截断参数、无 Secret）
   - AuditSink：队列满丢弃 warn（不阻塞）、Shutdown flush 全部（-race）
2. REST 回归：`go test ./...`（fakeStore 新方法、既有 auth/kb 隔离测试全绿）
3. migration 幂等：pgxmock 断言 DDL 含 `IF NOT EXISTS`；重复执行测试

**验证：** `go test ./... -race` 全绿；`go vet ./...` 无告警

## 执行顺序与依赖

```
T1（mcp-go 探针，先行）
  └→ T6（errors.go）
T2（config）  ─┐
T3（schema）───┼→ T4（apikey store）→ T5（store 其余 + fakeStore）
               └────────────────────────┘
T6 → T7（permission）→ T8（auth）
T5 → T9（audit）
T6+T7+T8+T9 → T10（tools）→ T11（server）
T2+T5+T11 → T12（app 挂载）
T4+T5 → T13（REST 管理接口）
T10+T11+T13 → T14（测试回归）
```

可并行组：
- T1 与 T2/T3 无相互依赖（T1 先行但独立）
- T2 ∥ T3 ∥ T7（互不依赖）
- T6/T7/T8/T9 在各自依赖就绪后可并行（T6 依赖 T1；T7 无依赖；T8 依赖 T4+T7；T9 依赖 T5）
- T12 ∥ T13（依赖不同上游）

串行关键路径：`T1 → T6 → T7 → T8 → T10 → T11 → T12 → T14`
