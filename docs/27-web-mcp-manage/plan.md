# Web 端 MCP 管理 Plan（v2：用户维度 MCP 凭据）

> v1（系统级管理：ApiKeysView 权限编辑、SettingsView 全局卡片、handler_config MCP 支持）已实现；
> v2 新增用户维度 MCP 凭据自助管理。

## 架构概览

**后端三层**：
1. **系统级管理（v1，保留）**：`/api/v1/api-keys`（bootstrap-only）+ `handler_config.go`（server.mcp）+ SettingsView/ApiKeysView
2. **用户自助（v2 新增）**：新端点组 `/api/v1/mcp/my/*`（JWT 会话，`Identity.Kind == KindUser`），每用户一个 MCP 凭据（`api_keys.owner_id` 非空唯一）
3. **MCP 授权（v2 增强）**：`authenticate` 填充 `keyCtx.OwnerID`；gateway 在认证后按 owner 解析**实际知识库范围**（用户 Key 的 `scope=all` → 仅自己的知识库；allowlist → 白名单 ∩ 自己的知识库）

```
前端
├── MyMcpView.vue（新路由 /my-mcp，JWT 会话）
│     ├── GET /api/v1/mcp/my/status（全局 enabled + 我的凭据）
│     ├── POST /api/v1/mcp/my/key（生成凭据，幂等 409 已存在）
│     ├── POST /api/v1/mcp/my/key/toggle（启停）
│     ├── DELETE /api/v1/mcp/my/key（吊销）
│     ├── PUT /api/v1/mcp/my/key/permissions（Tool + 范围 + KB ids）
│     └── GET /api/v1/knowledge-bases（自己的 KB 可选项，JWT 已按 owner 过滤）
├── ApiKeysView.vue（v1 保留，bootstrap）
└── SettingsView.vue（v1 保留，bootstrap 全局卡片）
```

## 核心数据结构

### store.APIKey（扩展）

```go
type APIKey struct {
    // ...既有字段...
    OwnerID string // 用户归属："" = 系统级（bootstrap/全局 Key）；非空 = 用户 MCP 凭据（users.id）
    MCPTools   []string
    MCPKBScope string
    MCPKBIDs   []string
}
```

### mcp.keyCtx（扩展）

```go
type keyCtx struct {
    KeyID   string
    Tools   []string
    Scope   KBPermission
    OwnerID string // "" = 系统级 Key；非空 = 用户 Key（gateway 按 owner 解析实际范围）
}
```

### 用户自助响应

```go
// GET /api/v1/mcp/my/status
type MyMCPStatus struct {
    GlobalEnabled bool       `json:"global_enabled"` // cfg.Server.MCP.Enabled
    Key           *MyMCPKey  `json:"key"`            // 我的凭据；无则 null
}
type MyMCPKey struct {
    ID         string   `json:"id"`
    Enabled    bool     `json:"enabled"`
    MCPTools   []string `json:"mcp_tools"`
    MCPKBScope string   `json:"mcp_kb_scope"`
    MCPKBIDs   []string `json:"mcp_kb_ids"`
}
// POST /api/v1/mcp/my/key → { id, key（明文仅此一次）}
```

## 模块设计

### internal/store

- `schema.go`：`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id TEXT;` + 部分唯一索引 `CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_owner ON api_keys(owner_id) WHERE owner_id IS NOT NULL;`（每用户一个 MCP 凭据，D2）
- `apikey.go`：`APIKey.OwnerID`；Create/List/GetByHash SQL 补列（INSERT 带 owner_id，系统级传 "" → NULL）；新增 `GetAPIKeyByOwner(ctx, ownerID string) (*APIKey, error)`（`WHERE owner_id = $1`）
- `store.go`：接口加 `GetAPIKeyByOwner`

### internal/mcp（授权 owner 限定，D1/D5/D6）

- `auth.go`：`keyCtx` 加 `OwnerID`；`authenticate` 填充 `OwnerID: key.OwnerID`（keyLookup 最小接口不变）
- `server.go`（gateway）：认证通过后，若 `kc.OwnerID != ""` 执行 owner 范围解析：
  ```go
  ownerIDs, _ := g.st.ListKBsByOwner(r.Context(), kc.OwnerID) // 复用现有方法，取 ID 集
  switch {
  case kc.Scope.All:                       // 用户 scope=all → 仅自己的 KB
      kc.Scope = KBPermission{IDs: ownerIDs}
  case len(kc.Scope.IDs) > 0:              // allowlist → 白名单 ∩ 自己的 KB（防御越权配置）
      kc.Scope.IDs = intersect(kc.Scope.IDs, ownerIDs)
  }
  ```
  此后 CanAccess/Resolve 逻辑不变（Scope.IDs 即实际可访问范围）

### internal/api（用户自助端点，D3/D4）

新文件 `handler_mcp_my.go`：
- 辅助 `requireUser(c) (string, bool)`：`IdentityOf(c)` 取 `Kind == KindUser` 的 `UserID`，否则 403
- `GET /api/v1/mcp/my/status`：`GlobalEnabled = cfgMgr.Current().Server.MCP.Enabled`；`GetAPIKeyByOwner(userID)` → MyMCPStatus
- `POST /api/v1/mcp/my/key`：`GetAPIKeyByOwner` 已有 → **409「已有凭据，请吊销后重建」**（D4）；无 → 生成（id=uuid、name=`mcp-{用户前8位}`、SHA-256 hash）→ `CreateAPIKey(OwnerID=userID)` → 返回明文（仅此一次）
- `POST /api/v1/mcp/my/key/toggle`：body `{enabled}`；校验我的凭据存在 → `SetAPIKeyEnabled`
- `DELETE /api/v1/mcp/my/key`：校验存在 → `DeleteAPIKey`（吊销）
- `PUT /api/v1/mcp/my/key/permissions`：body `{mcp_tools, mcp_kb_scope, mcp_kb_ids}`；**校验 kb_ids 均 ∈ 用户自己的 KB**（`ListKBsByOwner` ID 集，越权 id → 400）→ `UpdateAPIKeyPermissions`
- `router.go`：`v1.Group("mcp/my")` 下挂 5 个路由（全局 Auth 中间件已保护；requireUser 校验会话）

### frontend

- `api/mcp.ts`（新）：`myMCPStatus()`/`createMyKey()`/`toggleMyKey(enabled)`/`deleteMyKey()`/`updateMyPermissions(perms)`
- `views/MyMcpView.vue`（新）：
  - 全局状态 `el-alert`（global_enabled=false → warning「MCP 服务未开启，请联系管理员启用」）
  - 凭据区：无 → 「生成我的 MCP Key」按钮；有 → 明文展示（仅创建后一次）+ 复制 + 启用开关 + 吊销（`ElMessageBox.confirm`）
  - 权限配置：Tool 勾选（6 个）+ 范围单选（无/全部/白名单）+ KB 多选（`listKbs()` 自己的 KB，filterable）
  - 连接信息：endpoint（`location.origin + path`，path 从 `getConfig()` 读）+ Bearer 说明 + mcpServers 示例
- `router/index.ts`：加 `/my-mcp` 路由（标题「我的 MCP」）+ 侧边菜单项（与 keys 同级）

## 模块交互

```
用户（JWT）→ MyMcpView
  → GET /mcp/my/status → {global_enabled, key}（全局关 → 页面提示不可用）
  → POST /mcp/my/key → 明文（一次）→ 复制
  → PUT /mcp/my/key/permissions（kb_ids 校验 ∈ 自己的 KB）
  → 开关/吊销
MCP 调用方 → POST /mcp（Bearer 用户 Key）
  → authenticate（OwnerID 入 keyCtx）
  → gateway 按 OwnerID 解析实际范围（all → 自己的 KB；allowlist → ∩ 自己的 KB）
  → 授权检查（-32001）/ 检索（限于自己的 KB）
```

## 文件组织

```
docs/27-web-mcp-manage/
├── spec.md / plan.md

internal/store/schema.go           — owner_id 列 + 部分唯一索引（迁移幂等）
internal/store/apikey.go           — OwnerID 字段、SQL 补列、GetAPIKeyByOwner
internal/store/store.go            — 接口加 GetAPIKeyByOwner
internal/mcp/auth.go               — keyCtx.OwnerID
internal/mcp/server.go             — gateway owner 范围解析
internal/api/handler_mcp_my.go     — 用户自助端点（新）
internal/api/router.go             — /mcp/my 路由
internal/api/api_test.go           — 用户接口 + owner 隔离测试
internal/mcp/*_test.go             — MCP 授权 owner 限定测试
frontend/src/api/mcp.ts            — 新
frontend/src/views/MyMcpView.vue   — 新
frontend/src/router/index.ts       — 路由 + 菜单
```

## 技术决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| D1 | owner 范围解析位置 | gateway（认证后、授权前） | authenticate 保持最小接口；范围解析需完整 store |
| D2 | 每用户凭据数量 | 每用户最多 1 个（owner_id 部分唯一索引） | 符合「自己的 MCP」直觉，简化面板 |
| D3 | 用户端点形态 | 独立 `/api/v1/mcp/my/*`（JWT），系统级保持 `/api-keys` | 权限语义清晰分离（会话 vs bootstrap） |
| D4 | 重复生成 | 已有凭据 → 409（吊销后重建） | 避免明文无法二次展示造成混淆 |
| D5 | 用户 scope=all | 实际范围 = 自己的知识库（ListKBsByOwner） | spec F9：用户不可越权访问他人/系统级 KB |
| D6 | 用户 allowlist | 白名单 ∩ 自己的知识库（防御过滤） | 防配置越权 id（N1 双路径隔离） |
| D7 | 系统级 Key | owner="" 行为完全不变 | N2：v1 全量权限、bootstrap 管理不受影响 |
| D8 | 界面 | 独立 MyMcpView（/my-mcp） | 用户反馈确认；与 ApiKeysView 职责分离 |

## Spec 覆盖

| Spec | 落点 |
|------|------|
| F1–F6（v1 保留） | ApiKeysView/SettingsView/handler_config（已实现，不改） |
| F7 我的 MCP 页 | MyMcpView + mcp.ts + 路由（D8） |
| F8 用户自助接口 | handler_mcp_my.go（D3/D4） |
| F9 知识库范围限定 | gateway owner 解析（D5/D6） |
| F10 双层开关 | status.global_enabled（全局）+ Key.enabled（用户开关） |
| N1 用户数据隔离 | 用户接口按 UserID 过滤 + MCP 授权 owner 限定 + kb_ids 校验 |
| N2 系统级不变 | D7 |
| N3 鉴权复用 | requireUser（会话）/ requireSystemKey（bootstrap） |
| N4 空权限「未配置」 | MyMcpView 展示空态 |
| N6 后端聚焦 | schema 加列 + 新端点 + gateway 解析，不动 MCP 核心 |
