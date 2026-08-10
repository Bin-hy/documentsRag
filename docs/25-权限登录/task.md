# 权限交验登录（API Key + 多 Provider + 用户知识库隔离）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/auth/auth.go` | Manager、NewManager、运行时校验、redirect URI 按 type 计算 |
| 新建 | `internal/auth/oidc.go` | oidcProvider（nonce/签名/issuer/aud 全校验） |
| 新建 | `internal/auth/github.go` | githubProvider（OAuth2 + GitHub API，专用 http.Client） |
| 新建 | `internal/auth/jwt.go` | SessionClaims、Sign、Verify（严格 HS256 + iss/exp/nbf） |
| 新建 | `internal/auth/ticket.go` | stateStore + ticketStore（独立、顺带清理过期项） |
| 新建 | `internal/auth/identity.go` | Identity、context 存取 |
| 新建 | `internal/store/user.go` | GetOrCreateUser |
| 修改 | `internal/store/schema.go` | users 表 + owner_id 迁移 |
| 修改 | `internal/store/kb.go` | ListAllKBs / ListKBsByOwner / CreateKB(owner_id) |
| 修改 | `internal/store/store.go` | Store 接口、KnowledgeBase.OwnerID *string |
| 修改 | `internal/store/store_test.go` | mock/fixture 兼容性修正 |
| 修改 | `internal/config/config.go` | OIDCConfig、ProviderConfig、Validate()、defaults |
| 修改 | `go.mod` | +coreos/go-oidc/v3、golang.org/x/oauth2、golang-jwt/jwt/v5 |
| 新建 | `internal/api/handler_auth.go` | providers / oidc+github login+callback / exchange / me |
| 修改 | `internal/api/router.go` | 公开子组 + /auth/* 路由 |
| 修改 | `internal/api/middleware.go` | Auth 双凭据 + JWT 三段式判别 |
| 修改 | `internal/api/handler_kb.go` | owner 过滤 + KB DTO |
| 修改 | `internal/api/handler_doc.go` | 访问校验 |
| 修改 | `internal/api/handler_task.go` | 访问校验 |
| 修改 | `internal/api/handler_chat.go` | kb_id 访问校验 |
| 修改 | `internal/api/handler_chunk.go` | chunk → document → kb 校验 |
| 修改 | `internal/api/handler_key.go` | 仅系统级 |
| 修改 | `internal/app/app.go` | 构建 auth.Manager 注入 Dependencies |
| 新建 | `frontend/src/api/auth.ts` | listProviders / exchangeTicket / getMe |
| 修改 | `frontend/src/api/client.ts` | token 双凭据存储与附加 |
| 修改 | `frontend/src/stores/auth.ts` | 双凭据登录态 |
| 修改 | `frontend/src/views/LoginView.vue` | provider 按钮（按 type 拼 login URL）+ ?ticket= 流程 |
| 修改 | `frontend/src/router/index.ts` | 守卫双凭据 |
| 修改 | `configs/config.yaml` | oidc: 段示例（github type=oauth2 + 自定义 type=oidc） |
| 修改 | `internal/api/docs/docs.go` | swagger 注解同步（swag 重新生成，需 Go 环境） |

## T1: 新增 Go 依赖（记录实际版本）

**文件：** `go.mod`
**依赖：** 无
**步骤：**
1. `go get github.com/coreos/go-oidc/v3/oidc`、`go get golang.org/x/oauth2`、`go get github.com/golang-jwt/jwt/v5`（用最新版安装，**最终记录实际写入 go.mod 的版本号**，不把 `@latest` 留成最终形态）
2. `go mod tidy`

**验证：** `go build ./...` + `go test ./...` 通过，依赖锁定稳定

## T2: 登录 Provider 配置与 config.Validate()

**文件：** `internal/config/config.go`
**依赖：** 无
**步骤：**
1. 定义 `OIDCConfig{Enabled, PublicURL, JWTSecret, JWTExpireMinutes, Providers}` 与 `ProviderConfig{Name, Type, DisplayName, ClientID, ClientSecret, Issuer, Scope, RedirectURL}`
2. `applyDefaults`：`JWTExpireMinutes` 默认 120；`Type` 默认 `"oidc"`；**type=oidc** 默认 scope `["openid","profile","email"]`；**type=oauth2（github）** 默认 scope `["read:user"]`（最小权限，不请求 email）
3. **`Validate()`**（静态校验，`Load → ApplyDefaults → Validate` 流程）：`Enabled && PublicURL==""` 报错；provider `Name` 非空且**不重复**且可安全用于 URL path（正则 `^[a-zA-Z0-9_-]+$`）；`ClientID`/`ClientSecret` 非空；`Type` ∈ {oidc, oauth2}；**type=oidc → Issuer 必填**；**type=oauth2 → 仅允许 name=github**、不要求 Issuer；`RedirectURL` 非空时须为合法 URL

**验证：** `go test ./internal/config/...` 通过；单测覆盖：重复 Name 报错、oidc 缺 Issuer 报错、oauth2 非 github 报错、Enabled 缺 public_url 报错、合法配置通过

## T3: users 表与 owner_id 迁移

**文件：** `internal/store/schema.go`
**依赖：** 无
**步骤：**
1. `schemaDDL` 增加 `users` 表（provider+subject UNIQUE）
2. 追加幂等迁移：`ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS owner_id TEXT`
3. `Migrate` 串接新迁移

**验证：** `go test ./internal/store/...` 通过（迁移幂等可重复）

## T4: GetOrCreateUser

**文件：** `internal/store/user.go`（新建）
**依赖：** T3
**步骤：**
1. 定义 `User{ID, Provider, Subject, Name, Email, CreatedAt}`
2. 实现 `GetOrCreateUser(ctx, u)`：`INSERT ... ON CONFLICT (provider, subject) DO UPDATE SET name=EXCLUDED.name, email=EXCLUDED.email RETURNING *`

**验证：** 单测：同 (provider, subject) 二次调用返回同一 ID 且 name 刷新

## T5: 知识库 owner 改造（含 SQL NULL 测试）

**文件：** `internal/store/kb.go`、`internal/store/store.go`、`internal/store/store_test.go`
**依赖：** T3
**步骤：**
1. `KnowledgeBase` 加 `OwnerID *string`
2. `CreateKB` SQL 增 `owner_id` 写入（API 层显式传 nil 或 &UserID）
3. 新增 `ListAllKBs(ctx)`（**无条件全量**：owner_id NULL 与 NOT NULL 均包含）、`ListKBsByOwner(ctx, ownerID)`（仅 `WHERE owner_id=$1`）；Store 接口以这两方法替代原 `ListKBs`
4. **兼容性排查**：grep 全部 `KnowledgeBase{`、`OwnerID`、序列化点、store mock、测试 fixture，逐一修正；`GetKB/UpdateKB/DeleteKB` SELECT 列补 `owner_id`；pgx 扫描约定 owner_id NULL → `*string` nil
5. **SQL NULL 测试**：`CreateKB(ownerID=nil)` → 库中 `owner_id IS NULL`；`CreateKB(&userID)` → `owner_id=userID`；`ListAllKBs` 两者可见；`ListKBsByOwner(userID)` 仅见 `owner_id=userID`

**验证：** `go test ./internal/store/...` 通过（含上述 4 断言）

## T6: Identity 与 context 存取

**文件：** `internal/auth/identity.go`（新建）
**依赖：** 无
**步骤：**
1. `Identity{Kind, APIKeyID, IsBootstrap, UserID, Provider}`（不持有 *store.User）
2. `SetIdentity(c, id)` / `IdentityOf(c)`：gin context 写入/读取

**验证：** `go build ./internal/auth/...` 通过

## T7: JWT 签发与验签（严格规则 + Secret 时机）

**文件：** `internal/auth/jwt.go`（新建）
**依赖：** T6
**步骤：**
1. `SessionClaims{UserID, Provider, jwt.RegisteredClaims}`
2. `NewSigner(secret)`：**密钥由 Manager 启动时一次性传入**（配置 `jwt_secret` 或启动期生成的 32 字节随机，进程内持有）；**Sign 不再生成密钥**
3. `Sign(uid, provider, ttl)`：HS256，**显式设置 iss="binrag"、iat=now、exp=now+ttl**
4. `Verify(token)`：**拒绝 alg=none 及其他算法（仅接受 HS256）** + 签名校验 + iss=="binrag" + exp 未过期 + nbf（存在时）校验

**验证：** 单测：签发→验证通过；篡改 payload / 过期 / 错 iss / alg=none / 换非 HS256 算法 → 均失败

## T8: stateStore 与 ticketStore（职责分离 + 清理策略）

**文件：** `internal/auth/ticket.go`（新建）
**依赖：** T6
**步骤：**
1. `stateEntry{Provider, Nonce, ExpiresAt}`（**state 绑定 nonce**）；`stateStore.New(provider, nonce, 10min)` / `Consume(state)`（原子「读取+删除」，`sync.Mutex`；并发仅一次成功）
2. `ticketEntry{UserID, Provider, ExpiresAt}`（**ticket 不保存 nonce**）；`ticketStore.New(userID, provider, 2min)` / `Consume(ticket)`（一次性）
3. 均用 `crypto/rand` 32 字节 base64url
4. **清理策略**：New/Consume 时顺带删除过期项（无后台 goroutine）

**验证：** 单测：New→Consume 成功；二次 Consume 失败；过期失败；并发 Consume 仅一次成功；New 多次后过期项被顺带清理

## T9: oidcProvider（discovery 网络行为 + nonce + 全测试矩阵）

**文件：** `internal/auth/oidc.go`（新建）
**依赖：** T6、T7、T8
**步骤：**
1. 实现 `Provider` 接口的 `Name/DisplayName/AuthCodeURL/ExchangeAndVerify`
2. `NewOIDCProvider(ctx, cfg)`：**启动阶段**用带 timeout（如 15s）的 context 调 `oidc.NewProvider(ctx, issuer)` 完成 discovery，失败 → 装配失败；JWKS 由 go-oidc 机制缓存/刷新；**认证 middleware 不重新 discovery**
3. `AuthCodeURL(state, nonce)`：携带 state 与 nonce claim
4. `ExchangeAndVerify(ctx, code, nonce)`：`oauth2.Exchange`（带 ctx timeout）→ 验 id_token：JWKS 签名、issuer（仅配置 issuer）、audience=client_id、exp/nbf、**nonce==登录时 stateStore 绑定值**（缺失或不匹配失败）、sub 非空；不调 userinfo
5. 返回 `UserInfo{Subject: sub, Name, Email}`

**验证：** 单测拆纯逻辑（httptest 提供 discovery+JWKS+token 端点）：正常 ID Token 通过；**错误签名 / 错误 issuer / 错误 audience / 过期 exp / nbf 未到 / nonce 不匹配 / sub 空 / 错误 signing algorithm → 全部失败**；discovery 失败 → NewOIDCProvider 返回错误

## T10: githubProvider（state 由统一回调保证 + 专用 client + 稳定 subject）

**文件：** `internal/auth/github.go`（新建）
**依赖：** T6、T8
**步骤：**
1. `NewGithubProvider(cfg, httpClient, baseURL)`：**持有专用 `*http.Client`（10s 超时）**；生产 baseURL=`https://api.github.com`，测试注入 httptest server；`oauth2.Config` endpoint=github，scope 默认 `read:user`
2. `AuthCodeURL(state, _)`：**忽略 nonce，仅携带 state**（state 是全部 provider 的 CSRF 机制，nonce 仅 OIDC 额外要求）
3. **登录/callback 的 state 校验由统一流程保证**：login → `stateStore.New("github", nonce="")` → 302；callback → `ConsumeState(state)`（失败即失败）→ `ExchangeAndVerify(code)`
4. `ExchangeAndVerify(ctx, code)`：`oauth2.Exchange` → 带超时 GET `/user` → 非 2xx 失败 → 解析 `{id, login, name, email}`，**id 缺失或非数字失败**；`subject=strconv.FormatInt(id,10)`、`name=login`（**不用 email 作身份**）；**access token 不入日志、不落库**
5. 返回 `UserInfo{Subject: id 字符串, Name: login}`

**验证：** 单测（httptest mock GitHub API）：2xx 成功取数字 id；500 → 失败；缺 id → 失败；非数字 id → 失败；state 消费失败路径（callback 层测试）

## T11: auth.Manager 装配与运行时校验（验证范围扩大）

**文件：** `internal/auth/auth.go`（新建）
**依赖：** T2、T7–T10
**步骤：**
1. `NewManager(cfg)`：**先调 `config.Validate()`**，再构建：JWT 密钥（配置或启动期生成 32 字节随机，进程内持有）、stateStore/ticketStore、provider 注册表（遍历 `cfg.Providers`：type=oidc → oidcProvider；type=oauth2+name=github → githubProvider；其余 type 报错）
2. **注册表重复 Name → 装配失败**（不后覆盖前）；Name/ClientID/ClientSecret 非空、type 合法、oidc issuer 非空、oauth2 仅 github、Name URL 安全——Validate() 已覆盖，NewManager 依赖其结果
3. 计算并固定每个 provider 的 redirect URI（显式优先）：oidc → `<public_url>/api/v1/auth/oidc/{provider}/callback`；github → `<public_url>/api/v1/auth/github/callback`；**callback 不随请求参数变化**
4. 暴露 `Providers() []ProviderView`（name/type/display_name，无机密）、`Get(name)`、`SignSession(uid, provider)`、`VerifyJWT(token)`

**验证：** `go test ./internal/auth/... ./internal/config/... ./internal/store/... ./internal/api/...` + `go build ./...` 全部通过（auth.Manager 被 app/api 装配，须验证完整依赖链）

## T12: Auth 中间件双凭据 + JWT 三段式判别（不使用 binrag_ 前缀）

**文件：** `internal/api/middleware.go`
**依赖：** T6、T7
**步骤：**
1. `Auth(store, authMgr, enabled, bootstrapKey)` 改造：
   - `!enabled` → 放行（现状不变）
   - 取 `Authorization: Bearer <token>`
   - **先判断 token 是否严格符合 JWT 三段式结构**：`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`
     - 符合 JWT 结构 → `authMgr.VerifyJWT(token)`；成功 → 写 OIDC Identity；**失败 → 直接 401，不再尝试 API Key**
     - 不符合 JWT 结构 → 按现有 API Key 流程计算 SHA-256 并查询数据库；成功 → 写 API Key Identity；失败 → 401
   - **不使用 `binrag_` 前缀作为认证分支条件**
   - bootstrap API Key 无论其具体字符串格式如何，均通过现有 API Key 校验流程处理
2. 命中后写 Identity：apikey → `{Kind:"apikey", APIKeyID, IsBootstrap}`；jwt → `{Kind:"oidc", UserID, Provider}`；保留现有 `is_bootstrap` context 键兼容

**验证：** `go test ./internal/api/...` 通过；新增中间件测试：
- 有效 API Key（**包括非 binrag_ 前缀格式**）→ 认证成功
- 有效 JWT → 认证成功
- 伪造但符合三段式 JWT → 401，**且不查询 API Key**
- 不符合 JWT 三段式的无效凭据 → 按 API Key 流程查询后 401
- 无凭据 → 401
- `auth_enabled=false` → 放行

## T13: handler_auth.go

**文件：** `internal/api/handler_auth.go`（新建）
**依赖：** T8–T11、T4（store.GetOrCreateUser）
**步骤：**
1. `GET /api/v1/auth/providers`（公开）→ `[{name, type, display_name}]`（`authMgr.Providers()`），无机密
2. `GET /api/v1/auth/oidc/:provider/login`（公开）→ provider 不存在 404 → `stateStore.New(provider, nonce)` → 302 `AuthCodeURL(state, nonce)`
3. `GET /api/v1/auth/oidc/:provider/callback`（公开）→ **先 `ConsumeState(state)`**（无效/过期立即失败）→ `ExchangeAndVerify(code, nonce)` → 任一失败：**不创建会话**，302 `/login?error=...` → 成功：`GetOrCreateUser` → `ticketStore.New(userID, provider)` → 302 `/login?ticket=xxx`
4. `GET /api/v1/auth/github/login`（公开）→ `stateStore.New("github", nonce="")` → 302 `AuthCodeURL(state)`
5. `GET /api/v1/auth/github/callback`（公开）→ 同 3（无 nonce）
6. `POST /api/v1/auth/exchange`（公开，body `{ticket}`）→ `ConsumeTicket` → `SignSession(userID, provider)` → `{token}`；重放/过期 → 401
7. `GET /api/v1/auth/me`（需认证）→ apikey：`{kind:"apikey", is_bootstrap}`；oidc：`{kind:"oidc", user_id, provider, name}`（按 `IdentityOf(c).UserID` 调 Store 查展示名；auth 包不依赖 store）

**验证：** `go build ./...` 通过；单测（httptest + mock provider/store）：login 302 带 state、callback 失败不建会话、exchange 一次成功二次失败、me 双身份输出

## T14: 路由挂载

**文件：** `internal/api/router.go`
**依赖：** T12、T13
**步骤：**
1. `Dependencies` 增 `Auth *auth.Manager`
2. 公开子组（不挂 Auth）：`/api/v1/auth/providers`、`/api/v1/auth/oidc/*`、`/api/v1/auth/github/*`、`/api/v1/auth/exchange`
3. 受保护：`/api/v1/auth/me` 放入挂 Auth 的 v1 组
4. `Auth(deps.Store, deps.Auth, ...)` 传入 authMgr

**验证：** `go test ./internal/api/...` 路由注册测试通过；公开接口无凭据可访问、`/me` 无凭据 401

## T15: 知识库 owner 过滤 + KB DTO

**文件：** `internal/api/handler_kb.go`
**依赖：** T5、T6
**步骤：**
1. `CreateKB`：`IdentityOf(c)` 为 oidc → `kb.OwnerID = &identity.UserID`；apikey → `kb.OwnerID = nil`
2. `ListKBs`：oidc → `store.ListKBsByOwner(UserID)`；apikey → `store.ListAllKBs`
3. 新增 `canAccessKB(c, kb) bool`：apikey → true（含系统级与用户级）；oidc → `kb.OwnerID != nil && *kb.OwnerID == UserID`
4. `GetKB/UpdateKB/DeleteKB` 取到 KB 后过 `canAccessKB`，不匹配一律 404
5. **KB 对外 DTO 剔除 `owner_id`**（新建视图结构，不含 OwnerID 字段）

**验证：** `go test ./internal/api/...` 通过；api_test 增补：oidc 用户列表只见自己的库、访问他人库 404、apikey 全量可见

## T16: 文档/任务/chat/chunk 访问校验

**文件：** `internal/api/handler_doc.go`、`internal/api/handler_task.go`、`internal/api/handler_chat.go`、`internal/api/handler_chunk.go`
**依赖：** T15（canAccessKB）
**步骤：**
1. `handler_doc.go`：`UploadDocument` 取到 KB 后 `canAccessKB`（不匹配 404）；`ListDocuments` 校验 kb_id 归属；`DeleteDocument` 经 `doc.KBID` 取 KB 校验
2. `handler_task.go`：`GetTask/RetryTask` 经 `task.KBID` 取 KB 校验
3. `handler_chat.go`：`Chat/ChatStream` 的 `req.KBID` 非空时取 KB 校验（`kbStrategy` 复用同一 KB 查询）
4. `handler_chunk.go`：`GetChunk` 先 `payload.document_id → GetDocument → KBID` 取 KB 校验

**验证：** `go test ./internal/api/...` 通过；补测试：oidc 用户对他人库 upload/list/delete/task/chat/chunk 均 404

## T17: Key 管理仅系统级

**文件：** `internal/api/handler_key.go`
**依赖：** T6、T12
**步骤：**
1. `CreateAPIKey/ListAPIKeys/DeleteAPIKey/ToggleAPIKey` 入口：`IdentityOf(c).Kind != "apikey"` → 403（提示仅系统级凭据可操作）

**验证：** `go test ./internal/api/...` 通过；oidc JWT 调 Key 管理接口 → 403

## T18: app 装配注入 auth.Manager

**文件：** `internal/app/app.go`
**依赖：** T11、T13、T14
**步骤：**
1. `New` 中构建 `authMgr, err := auth.NewManager(&cfg.OIDC)`（失败 → 装配失败）
2. `api.Dependencies.Auth = authMgr`；`NewRouter` 使用

**验证：** `go build ./...` 通过；启动无 OIDC 配置时行为不变（Enabled=false 不初始化 provider）

## T19: 前端 auth API 与凭据存储

**文件：** `frontend/src/api/auth.ts`（新建）、`frontend/src/api/client.ts`
**依赖：** 无（前端独立）
**步骤：**
1. `api/auth.ts`：`listProviders()`、`exchangeTicket(ticket)`、`getMe()`
2. `client.ts`：新增 `getStoredToken/setStoredToken/clearStoredToken`（`binrag_token`）；请求拦截器**优先附 Bearer Token，无则附 API Key**；401 处理同时清 token 与 apiKey

**验证：** `npm run test`（vitest）通过；`npm run build` 通过

## T20: 前端登录态与路由守卫

**文件：** `frontend/src/stores/auth.ts`、`frontend/src/router/index.ts`
**依赖：** T19
**步骤：**
1. `auth store`：`state.token`；`loginWithAPIKey(key)`（原 login）、`loginWithOIDC(ticket)`（exchange → set token）、`logout()` 双清、`isAuthenticated` = token 或 apiKey 任一非空
2. `router` 守卫：`getStoredToken() || getStoredApiKey()` 判定登录

**验证：** `npm run test` 通过

## T21: 登录页 OIDC/GitHub 按钮 + ticket 流程

**文件：** `frontend/src/views/LoginView.vue`
**依赖：** T19、T20
**步骤：**
1. `onMounted`：`listProviders()` 渲染按钮；**按 type 拼 login URL**：`oidc` → `/api/v1/auth/oidc/{name}/login`；`oauth2` → `/api/v1/auth/github/login`；点击 `window.location.href` 跳转
2. 检测 `?ticket=` → `loginWithOIDC(ticket)` → `history.replaceState` 清除 ticket → 跳 redirect
3. 检测 `?error=` → 提示错误
4. 保留 API Key 输入登录

**验证：** `npm run build` 通过；手动/集成：点按钮跳授权页、回调后进入主界面、URL 无残留 ticket

## T22: 配置示例与 swagger 同步

**文件：** `configs/config.yaml`、`internal/api/docs/docs.go`
**依赖：** T13–T17
**步骤：**
1. `configs/config.yaml` 增 `oidc:` 段示例：`github`（type=oauth2）+ `company`（type=oidc，含 issuer）
2. handler 注解更新（`/auth/*`、KB/Key 接口的 Security 描述）→ `swag init` 重新生成 `docs.go`（需 Go 环境，生成后提交）

**验证：** `go build ./...` 通过；swagger 页面含新接口

## 执行顺序

```
后端：
T1 → T2 → T3 → T4 → T5 ──→ T6
                             ↘
        T7 → T8 → T9 → T10 → T11 ──→ T12 → T13 → T14
                                              ↘
              T15 → T16 → T17 ──→ T18 ──→ T22
前端（可与后端并行）：
        T19 → T20 → T21
```
