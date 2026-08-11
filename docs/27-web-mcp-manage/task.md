# Web 端 MCP 管理 Tasks（v2：用户维度 MCP 凭据）

> 依据：已批准的 spec.md(v2) + plan.md(v2)。
> v1 任务（系统级管理：handler_config/ApiKeysView/SettingsView/config_mcp_test）**已实现**（代码在位，未提交）；本清单聚焦 **v2 新增**，并在回归任务（T6）统一验证 v1+v2。

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/store/schema.go` | `api_keys.owner_id` 列 + 部分唯一索引（幂等） |
| 修改 | `internal/store/apikey.go` | `APIKey.OwnerID`、SQL 补列、`GetAPIKeyByOwner` |
| 修改 | `internal/store/store.go` | 接口加 `GetAPIKeyByOwner` |
| 修改 | `internal/store/store_test.go` / `apikey_test.go` | OwnerID 列断言 + GetAPIKeyByOwner 用例 |
| 修改 | `internal/mcp/auth.go` | `keyCtx.OwnerID` 填充 |
| 修改 | `internal/mcp/server.go` | gateway owner 范围解析（all→自己的 KB；allowlist→∩自己的） |
| 新建 | `internal/api/handler_mcp_my.go` | 用户自助端点（status/create/toggle/delete/permissions） |
| 修改 | `internal/api/router.go` | `/mcp/my` 路由 |
| 修改 | `internal/api/api_test.go` | 用户接口 + owner 隔离测试 |
| 修改 | `internal/mcp/server_test.go` | MCP 授权 owner 限定测试（fakeStore 支持 owner） |
| 新建 | `frontend/src/api/mcp.ts` | 我的 MCP API |
| 新建 | `frontend/src/views/MyMcpView.vue` | 我的 MCP 页面 |
| 修改 | `frontend/src/router/index.ts` | 路由 + 菜单项 |

## T1: store owner_id 支持

**文件：** `internal/store/schema.go`、`internal/store/apikey.go`、`internal/store/store.go`
**依赖：** 无
**步骤：**
1. schema 迁移（幂等）：
   - `ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id TEXT;`
   - `CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_owner ON api_keys(owner_id) WHERE owner_id IS NOT NULL;`（每用户一个凭据，D2）
   - `Migrate` 追加上述迁移；schema_test.go 的 SQL 序列断言同步
2. `APIKey` 结构加 `OwnerID string`（""=系统级）
3. `CreateAPIKey` INSERT 补 `owner_id` 列（"" → NULL）；`ListAPIKeys`/`GetAPIKeyByHash` SELECT 补列
4. 新增 `GetAPIKeyByOwner(ctx, ownerID) (*APIKey, error)`：`SELECT ... WHERE owner_id = $1`（ErrNoRows → nil）
5. `Store` 接口加 `GetAPIKeyByOwner`
6. 测试：`apikey_test.go` 加 GetAPIKeyByOwner 用例（pgxmock，OwnerID 列绑定）；schema_test.go 断言新迁移 SQL

**验证：** `go build ./internal/store/...`；`go test ./internal/store/ -run 'TestGetAPIKeyByOwner|TestMigrate'`

## T2: mcp 授权 owner 限定

**文件：** `internal/mcp/auth.go`、`internal/mcp/server.go`
**依赖：** T1
**步骤：**
1. `keyCtx` 加 `OwnerID string`；`authenticate` 填充 `OwnerID: key.OwnerID`（keyLookup 最小接口不变）
2. gateway `ServeHTTP`：认证通过后，若 `kc.OwnerID != ""`：
   - `ownerKBs, err := g.st.ListKBsByOwner(ctx, kc.OwnerID)` → ownerID 集
   - `kc.Scope.All == true` → `kc.Scope = KBPermission{IDs: ownerIDs}`（用户 all = 自己的 KB，D5）
   - `len(kc.Scope.IDs) > 0` → `kc.Scope.IDs = intersect(ids, ownerIDs)`（白名单 ∩ 自己的，D6）
   - 解析后 CanAccess/Resolve 逻辑不变

**验证：** `go build ./internal/mcp/...`

## T3: 用户自助端点 handler_mcp_my.go

**文件：** `internal/api/handler_mcp_my.go`（新）、`internal/api/router.go`
**依赖：** T1
**步骤：**
1. 辅助 `requireUser(c) (string, bool)`：`auth.IdentityOf(c)` 取 `Kind == KindUser` 的 `UserID`，否则 403
2. `GET /api/v1/mcp/my/status`：`GlobalEnabled = cfgMgr.Current().Server.MCP.Enabled`；`GetAPIKeyByOwner(userID)` → `MyMCPStatus{GlobalEnabled, Key}`（无凭据 Key=null）
3. `POST /api/v1/mcp/my/key`：已有凭据 → **409**「已有凭据，请吊销后重建」（D4）；无 → 生成（id=uuid、name=`mcp-{userID 前 8 位}`、SHA-256 hash）→ `CreateAPIKey(OwnerID=userID)` → 返回 `{id, key}`（明文仅此一次）
4. `POST /api/v1/mcp/my/key/toggle`：body `{enabled}`；校验凭据存在 → `SetAPIKeyEnabled(id, enabled)`
5. `DELETE /api/v1/mcp/my/key`：校验凭据存在 → `DeleteAPIKey`（吊销）
6. `PUT /api/v1/mcp/my/key/permissions`：body `{mcp_tools, mcp_kb_scope, mcp_kb_ids}`；**校验每个 kb_id ∈ 用户自己的 KB**（`ListKBsByOwner` ID 集，越权 → 400）→ `UpdateAPIKeyPermissions(id, perms)`
7. router：`v1` 组下挂 `mcp/my` 5 个路由（swag 注解；全局 Auth 已保护）
8. 复用 `orEmptyStrings`/错误码（CodeConflict=409 需检查是否存在该常量）

**验证：** `go build ./internal/api/...`

## T4: 后端测试（用户接口 + owner 隔离 + MCP 授权）

**文件：** `internal/api/api_test.go`、`internal/mcp/server_test.go`、`internal/mcp/integration_test.go`
**依赖：** T2、T3
**步骤：**
1. api 测试（fakeStore 需支持 owner + GetAPIKeyByOwner；会话 JWT 调 mcp/my）：
   - status：无凭据 → `key=null`；创建后 → 返回凭据信息
   - create：首次 200（含明文）；再次 409
   - toggle：停用后 status 显示 enabled=false；再启用
   - delete：吊销后 status key=null；再次 create 成功
   - permissions：合法 kb_ids → 200；**越权 kb_id（非用户 KB）→ 400**
   - 非会话（API Key）调 mcp/my → 403（requireUser）
2. MCP 授权 owner 限定（integration_test 或独立用例）：
   - 用户 Key（owner 非空）scope=all：`list_knowledge_bases` 只返回自己的 KB；`get_knowledge_base` 他人 KB → -32001
   - 用户 Key allowlist 含他人 KB id：被过滤，调用他人 KB → -32001
   - 系统级 Key（owner 空）：行为不变（可访问全部）
   - fakeStore：Key 支持 OwnerID；`ListKBsByOwner` 已有

**验证：** `go test ./internal/api/ ./internal/mcp/ -count=1`

## T5: 前端「我的 MCP」页

**文件：** `frontend/src/api/mcp.ts`（新）、`frontend/src/views/MyMcpView.vue`（新）、`frontend/src/router/index.ts`、`frontend/src/api/types.ts`
**依赖：** 无（后端接口定义已定，可并行；验证用类型检查）
**步骤：**
1. `types.ts`：`MyMCPStatus`/`MyMCPKey`/`CreateMyKeyResult{id, key}` 类型
2. `api/mcp.ts`：`myMCPStatus()`/`createMyKey()`/`toggleMyKey(enabled)`/`deleteMyKey()`/`updateMyPermissions(perms)`（对应 `/api/v1/mcp/my/*`）
3. `MyMcpView.vue`：
   - `onMounted`：`Promise.all([myMCPStatus(), getConfig(), listKbs()])`
   - 全局状态：`global_enabled=false` → `el-alert` warning「MCP 服务未开启，请联系管理员启用」，其余控件禁用
   - 凭据区：无 → 生成按钮（成功后明文弹窗一次性展示 + 复制）；有 → `el-switch`（enabled）+ 吊销按钮（`ElMessageBox.confirm`）+ 权限编辑区
   - 权限编辑：Tool 勾选（6 个）+ 范围单选（无/全部/白名单）+ KB 多选（`listKbs()` 数据，filterable）；保存 `updateMyPermissions` → 刷新
   - 连接信息：endpoint（`location.origin + getConfig().mutable.mcp.path`）+ Bearer（使用我的凭据）+ mcpServers 示例可复制
   - 非会话（API Key 登录）时页面提示「请使用账号登录后使用」
4. `router/index.ts`：加 `{ path: 'my-mcp', name: 'my-mcp', component: ... }` + 侧边菜单项「我的 MCP」（查看现有 keys 菜单项结构对齐）

**验证：** `npx vue-tsc --noEmit`；`npm run build`

## T6: 前后端回归（v1 + v2）

**文件：** 无新增
**依赖：** T1–T5 全部
**步骤：**
1. 后端：`go build ./...`、`go vet ./internal/api/ ./internal/store/ ./internal/mcp/`、`go test -race ./internal/api/ ./internal/store/ ./internal/mcp/ ./internal/config/ -count=1`
2. 前端：`cd frontend && npm run build`
3. 冒烟（可选）：本地服务 → OIDC 会话调 `GET /mcp/my/status` → 生成凭据 → MCP 调用验证 owner 隔离（scope=all 只检索自己的 KB）

**验证：** 后端全绿 + 前端 build 通过

## 执行顺序与依赖

```
T1（store owner）──→ T2（mcp 授权 owner）──→ T4（后端测试）
                  └→ T3（用户端点）─────────┘
T5（前端我的 MCP，与后端并行）
T1..T5 ──→ T6（回归 v1+v2）
```

关键路径：`T1 → T2/T3 → T4 → T6`；T5 前端独立推进（类型定义先行）。
