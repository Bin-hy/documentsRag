# 权限交验登录（API Key + 多 Provider + 用户知识库隔离）Plan

## 架构概览

新增 **`internal/auth`** 包承载全部身份与 OAuth/OIDC 逻辑，**不依赖 store**；`internal/api`（HTTP 层）负责授权范围判断与 `/auth/me` 的用户展示信息查询；`internal/store` 负责用户与知识库持久化。

| 组件 | 职责 |
|------|------|
| `auth.Manager` | 持有 provider 注册表、JWT 密钥、stateStore/ticketStore 票据存储；提供授权码流程入口、凭证校验、JWT 签发/验签、身份模型 |
| `store` 扩展 | `users` 表；`knowledge_bases.owner_id`（nullable，NULL=系统级）；`GetOrCreateUser`、`ListAllKBs`/`ListKBsByOwner` |
| `api` 改造 | `Auth` 中间件双凭据；新增 `/auth/*` 路由；既有 KB/文档/任务/chat/chunk handler 接入知识库归属校验 |
| 前端 | `LoginView` 渲染 Provider 按钮、ticket 换取 JWT；`stores/auth` 双凭据；新增 `api/auth.ts` |

**登录数据流（OIDC）**：`按钮 → GET /auth/oidc/:provider/login（生成 state+nonce 存 stateStore）→ 302 issuer 授权页 → 回调 /auth/oidc/:provider/callback?code&state（原子消费 state → 验 id_token.nonce → 换 token → 验 id_token → GetOrCreateUser → 生成一次性 ticket）→ 302 /login?ticket=xxx → 前端 POST /auth/exchange{ticket} → 后端消费 ticket 签发 JWT → 前端存 JWT 并 history.replaceState 清除 ticket`。**JWT 永不出现在 URL**。

**登录数据流（GitHub，OAuth2）**：`按钮 → GET /auth/github/login（stateStore.New(provider="github", nonce="") → 302 github 授权页）→ 回调 /auth/github/callback?code&state（原子消费 state → ExchangeAndVerify(code) → GitHub API 取数字 id → GetOrCreateUser → 生成一次性 ticket）→ 302 /login?ticket=xxx → /auth/exchange 换 JWT`。GitHub 无 nonce/id_token，**必须校验 state**。

**认证数据流**（N5）：`取 Bearer token → 无歧义判别（见技术决策）→ JWT 本地验签（一次）或 API Key 凭据查询（至多一次）。无外部网络调用`。

## 核心数据结构

```go
// internal/auth —— 会话 JWT 载荷（不含展示性 Name）
type SessionClaims struct {
    UserID   string `json:"uid"`
    Provider string `json:"provider"`
    jwt.RegisteredClaims // exp/iat/iss
}

// internal/auth —— 当前请求身份（中间件写入 gin.Context，不持有 *store.User）
type Identity struct {
    Kind        string // "apikey" | "oidc"
    APIKeyID    string // Kind=apikey
    IsBootstrap bool   // 是否 bootstrap Key
    UserID      string // Kind=oidc
    Provider    string // Kind=oidc
}

// internal/auth —— Provider 抽象（OIDC 与 GitHub OAuth2 统一接口；GitHub 不是 OIDC）
type Provider interface {
    Name() string
    DisplayName() string
    AuthCodeURL(state, nonce string) string // GitHub 忽略 nonce
    ExchangeAndVerify(ctx context.Context, code, nonce string) (*UserInfo, error)
}
type UserInfo struct { Subject, Name, Email string }

// internal/auth —— state 票据（OIDC/GitHub 授权码流程防 CSRF）
type stateEntry struct { Provider string; Nonce string; ExpiresAt time.Time } // 仅 OIDC 使用 Nonce
type stateStore struct { mu sync.Mutex; m map[string]stateEntry }
func (s *stateStore) New(provider, nonce string, ttl time.Duration) (state string, err error) // crypto/rand 32 字节 base64url
func (s *stateStore) Consume(state string) (provider, nonce string, ok bool)                  // 原子读取+删除；过期/不存在即失败

// internal/auth —— ticket 票据（callback 成功后换 JWT，绑定已认证用户）
type ticketEntry struct { UserID string; Provider string; ExpiresAt time.Time }
type ticketStore struct { mu sync.Mutex; m map[string]ticketEntry }
func (s *ticketStore) New(userID, provider string, ttl time.Duration) (ticket string, err error)
func (s *ticketStore) Consume(ticket string) (userID, provider string, ok bool) // 一次性，成功后立即删除

// internal/store —— 登录用户（users 表，provider+subject 唯一）
type User struct {
    ID        string
    Provider  string // "github" / 自定义 OIDC 标识
    Subject   string // GitHub=数字用户 ID；OIDC=sub
    Name      string
    Email     string
    CreatedAt time.Time
}

// internal/config —— 登录 Provider 配置
type OIDCConfig struct {
    Enabled          bool     `yaml:"enabled"`
    PublicURL        string   `yaml:"public_url"`         // Enabled 时必填，缺失 → 启动失败
    JWTSecret        string   `yaml:"jwt_secret"`         // 空 = 自动生成随机
    JWTExpireMinutes int      `yaml:"jwt_expire_minutes"` // 默认 120
    Providers        []ProviderConfig `yaml:"providers"`
}
type ProviderConfig struct {
    Name         string   `yaml:"name"`          // 标识：github | 自定义
    Type         string   `yaml:"type"`          // "oidc"（默认）| "oauth2"（内置 GitHub 适配）
    DisplayName  string   `yaml:"display_name"`
    ClientID     string   `yaml:"client_id"`
    ClientSecret string   `yaml:"client_secret"`
    Issuer       string   `yaml:"issuer"`        // 仅 type=oidc
    Scope        []string `yaml:"scope"`         // OIDC 默认 ["openid","profile","email"]；GitHub 默认 ["read:user"]（最小权限，不请求 email）
    RedirectURL  string   `yaml:"redirect_url"`  // 可选；默认 <public_url>/api/v1/auth/oidc/{provider}/callback（{provider} 为 Name）
}
```

**数据库变更**（幂等迁移）：

```sql
CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS owner_id TEXT; -- NULL = 系统级
```

**Store 接口变更**：`CreateKB(ctx, kb)` 持久化 `owner_id`（由 API 层显式决定，Store 不推断）；`ListAllKBs(ctx)`（系统级全量）；`ListKBsByOwner(ctx, ownerID)`；新增 `GetOrCreateUser(ctx, User) (*User, error)`（`INSERT ... ON CONFLICT (provider, subject) DO UPDATE SET name=EXCLUDED.name, email=EXCLUDED.email RETURNING *`）。`KnowledgeBase.OwnerID *string`（pgx 对 NULL 列扫描到 `*string` 得 nil；Go 结构体统一 `*string`，杜绝空串混淆）。

## 模块设计

### internal/auth（新建，无 store 依赖）

- **auth.go** — `NewManager(cfg)`：先调 `config.Validate()`（静态校验），再按配置构建 provider 注册表、JWT 密钥（`jwt_secret` 为空时启动期一次性生成 32 字节随机密钥，仅进程内持有）、stateStore/ticketStore。**运行时校验**：`type=oidc` 的 provider 用带超时 context 执行 issuer 动态发现并缓存 JWKS，失败即装配失败；计算并固定每个 provider 的 redirect URI（显式配置优先）：`type=oidc` → `<public_url>/api/v1/auth/oidc/{provider}/callback`，`type=oauth2`（github）→ `<public_url>/api/v1/auth/github/callback`；`type=oauth2` 仅允许 `name=github`（内置适配）。
- **oidc.go** — oidcProvider（`coreos/go-oidc/v3` + `golang.org/x/oauth2`）：`AuthCodeURL` 携带 state+nonce；`ExchangeAndVerify` 完整校验：JWKS 签名、issuer（仅信任配置 issuer，不信任回调动态指定）、audience=client_id、exp/nbf、nonce 与登录时绑定值一致、subject 非空；不调用 userinfo 端点（N5）。
- **github.go** — githubProvider（`golang.org/x/oauth2` 授权码 + `GET https://api.github.com/user`）：无 id_token/nonce，仅 state 防 CSRF；**必须校验 state**；用带超时（10s）的 http client 调用 GitHub API；非 2xx → 登录失败；响应缺数字 `id` → 登录失败；`subject=github 数字 id`、`name=login`；access token 不入日志、不落库、不持久化。
- **jwt.go** — `Sign(uid, provider, ttl)` / `Verify(token)`；HS256；密钥为配置 `jwt_secret` 或 `crypto/rand` 生成（进程内）。
- **ticket.go** — stateStore（state：TTL 10 分钟）与 ticketStore（ticket：TTL 2 分钟）两个独立存储：state 绑定 (provider, nonce)；ticket 绑定已认证的 (userID, provider)，**不保存 nonce**。均 `crypto/rand` 高熵、`Consume` 原子「读取+删除」、`sync.Mutex` 并发安全、一次性。
- **identity.go** — `Identity` + `SetIdentity/IdentityOf`（gin context）。

### internal/store 扩展

- **user.go**（新建）— `GetOrCreateUser`。
- **kb.go**（修改）— `CreateKB` 带 owner_id；`ListAllKBs` / `ListKBsByOwner`；`GetKB/UpdateKB/DeleteKB` 原样并返回 `OwnerID`；权限判断不在此层。
- **schema.go**（修改）— users DDL + owner_id 幂等迁移。
- **store.go / store_test.go** — Store 接口增方法、`KnowledgeBase.OwnerID *string` 变更、**兼容性排查**（见技术决策）。

### internal/api 改造

- **handler_auth.go**（新建）：
  - `GET /api/v1/auth/providers`（公开）→ `[{name, type, display_name}]`，无机密
  - `GET /api/v1/auth/oidc/:provider/login`（公开）→ OIDC 类 provider 不存在 404 → 生成 state+nonce 绑定存储 → 302 `AuthCodeURL(state, nonce)`
  - `GET /api/v1/auth/oidc/:provider/callback`（公开）→ **先原子 `ConsumeState(state)`**（不存在/过期立即失败）→ `ExchangeAndVerify(code, nonce)` → 任一失败：**不创建会话**，302 `/login?error=...`（明确但不泄露机密）→ 成功：`GetOrCreateUser` → 生成一次性 ticket → 302 `/login?ticket=xxx`
  - `GET /api/v1/auth/github/login`（公开）→ `stateStore.New(provider="github", nonce="")` → 302 `AuthCodeURL(state)`
  - `GET /api/v1/auth/github/callback`（公开）→ **先原子 `ConsumeState(state)`** → `ExchangeAndVerify(code)` → 成功路径同上（GetOrCreateUser → ticket → 302）
  - `POST /api/v1/auth/exchange`（公开，body `{ticket}`）→ `ConsumeTicket(ticket)` → `Sign(userID, provider)` → 返回 `{token}`；ticket 消费后立即删除，重放失败
  - `GET /api/v1/auth/me`（需认证）→ apikey：`{kind:"apikey", is_bootstrap}`；oidc：`{kind:"oidc", user_id, provider, name}`（name 按 `Identity.UserID` 调 Store 查展示信息，不注入 auth.Manager）
- **middleware.go** — `Auth` 双凭据 + 无歧义判别（见技术决策）；写入 `Identity`，保留 `is_bootstrap`。
- **handler_kb.go** — `CreateKB`：oidc → `OwnerID=&UserID`，apikey → `OwnerID=nil`（NULL）；`ListKBs`：oidc → `ListKBsByOwner(UserID)`，apikey → `ListAllKBs`；`Get/Update/DeleteKB` 前 `canAccessKB`（不匹配一律 404）。
- **handler_doc.go / handler_task.go / handler_chat.go / handler_chunk.go** — `canAccessKB` 调用点：upload 与 ListDocuments 校验 kb_id；DeleteDocument 经 `doc.KBID`；GetTask/RetryTask 经 `task.KBID`；Chat 的 `kb_id` 非空时校验；GetChunk 经 `document_id → GetDocument → KBID`。
- **handler_key.go** — 4 个 Key 管理接口入口校验 `Identity.Kind=="apikey"`，否则 403。
- **KB 对外 DTO** — 列表/详情返回时剔除 `owner_id`（不暴露归属字段到前端）。

### 前端

- **api/auth.ts**（新建）— `listProviders()`、`exchangeTicket(ticket)`、`getMe()`。
- **api/client.ts** — `getStoredToken/setStoredToken/clearStoredToken`（`binrag_token`）；请求拦截器优先附 Bearer Token，无则附 API Key。
- **stores/auth.ts** — `state.token`；`loginWithOIDC(ticket)`、`loginWithAPIKey(key)`、`isAuthenticated` 双凭据判断。
- **LoginView.vue** — 加载 providers 渲染按钮（`window.location` 跳 login）；`onMounted` 检测 `?ticket=` → 调 exchange 存 JWT → `history.replaceState` 清除；检测 `?error=` 提示。
- **router/index.ts** — 守卫：token 或 apiKey 任一存在即已登录。

## 模块交互

```
登录（OIDC）：前端按钮 → /auth/oidc/:provider/login（state+nonce）→ issuer 授权页 → /auth/oidc/:provider/callback（ConsumeState → 验 nonce/签名/issuer/aud → GetOrCreateUser → ticket）→ /login?ticket → /auth/exchange（ConsumeTicket → Sign JWT）→ Bearer 携带

登录（GitHub）：前端按钮 → /auth/github/login（stateStore.New(provider="github", nonce="")）→ github 授权页 → /auth/github/callback（ConsumeState → ExchangeAndVerify → GitHub API 数字 id → GetOrCreateUser → ticket）→ /login?ticket → /auth/exchange（ConsumeTicket → Sign JWT）→ Bearer 携带

认证：
  Auth 中间件 → 判别 token 形态 → [JWT 验签（一次）| API Key 查库（至多一次）] → Identity → handler

知识库隔离：
  CreateKB → oidc: OwnerID=UserID / apikey: OwnerID=NULL（NULL 由 API 层显式传 nil）
  ListKBs → oidc: ListKBsByOwner(UserID) / apikey: ListAllKBs
  Get/Update/Delete/文档/任务/chat/chunk → canAccessKB(Identity, kbID)，不匹配一律 404
```

## 文件组织

```
internal/auth/            （新建包）
├── auth.go               — Manager、NewManager、启动校验、redirect URI 计算
├── oidc.go               — oidcProvider（nonce/签名/issuer/aud 全校验）
├── github.go             — githubProvider（OAuth2 + GitHub API，超时/错误处理）
├── jwt.go                — SessionClaims、Sign、Verify
├── ticket.go             — stateStore + ticketStore（独立结构）
└── identity.go           — Identity、context 存取

internal/store/
├── schema.go             — +users 表、owner_id 迁移
├── user.go               — GetOrCreateUser（新建）
├── kb.go                 — ListAllKBs / ListKBsByOwner / CreateKB(owner_id)
├── store.go              — Store 接口、KnowledgeBase.OwnerID *string
└── store_test.go         — 测试 fixture/mock 兼容性修正

internal/config/config.go — OIDCConfig、ProviderConfig（type 字段）、defaults

internal/api/
├── router.go             — 公开子组 + /auth/* 路由
├── middleware.go         — Auth 双凭据 + 无歧义判别
├── handler_auth.go       — providers / oidc+github login+callback / exchange / me（新建）
├── handler_kb.go         — owner 过滤 + KB DTO 剔除 owner_id
├── handler_doc.go        — upload/list/delete 访问校验
├── handler_task.go       — get/retry 访问校验
├── handler_chat.go       — kb_id 访问校验
├── handler_chunk.go      — chunk → document → kb 校验
└── handler_key.go        — 仅系统级

internal/app/app.go       — 构建 auth.Manager 注入 Dependencies

frontend/src/
├── api/auth.ts           — listProviders / exchangeTicket / getMe（新建）
├── api/client.ts         — token 双凭据存储与附加
├── stores/auth.ts        — 双凭据登录态
├── views/LoginView.vue   — provider 按钮 + ?ticket= 流程
└── router/index.ts       — 守卫双凭据

configs/config.yaml       — 新增 oidc: 段示例（GitHub type=oauth2 + 自定义 type=oidc）

go.mod                    — +coreos/go-oidc/v3、golang.org/x/oauth2、golang-jwt/jwt/v5
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| GitHub 语义 | **GitHub 是 OAuth2 provider，不是 OIDC**（实测：github.com discovery 404；token.actions.githubusercontent.com 仅 Actions 用）→ 内置 githubProvider：OAuth2 授权码 + `GET api.github.com/user`，subject=数字 id；不校验 nonce/id_token，**必须校验 state** | 概念与实现一致；User 表统一 (provider, subject) |
| Provider 配置 | 统一 `providers` 列表 + `type: oidc\|oauth2` 字段；`type=oauth2` 仅允许 name=github；`type=oidc` 要求 Issuer 必填、默认 scope openid profile email；`type=oauth2` 不要求 Issuer、scope 由具体 provider 决定 | 前端渲染与扩展统一，不混淆概念 |
| Provider 路径 | OIDC 用 `/api/v1/auth/oidc/:provider/login\|callback`；GitHub 用 `/api/v1/auth/github/login\|callback`；ticket 交换统一 `/api/v1/auth/exchange`；redirect URI 启动时按 type 计算并固定，callback 不随请求参数变化 | GitHub 是 OAuth2，不塞进 oidc 路径（用户要求） |
| 配置校验职责 | `config.Load → ApplyDefaults → Validate`（静态：Name 重复/非空、type 合法、oidc issuer 必填、oauth2 仅 github、ClientID/Secret 非空、Name 可安全用于 URL path、redirect URL 合法）；`auth.NewManager` 只做运行时依赖（OIDC discovery/JWKS） | 职责清晰不分散（用户要求） |
| 票据清理 | stateStore/ticketStore 在 New/Consume 时顺带清理过期项（无后台 goroutine） | 简单可靠，避免生命周期复杂度（YAGNI） |
| JWT 规则 | Sign 显式设置 iss=binrag、iat=now、exp=now+ttl；Verify 严格 HS256、校验签名/iss/exp/nbf（存在时）、拒绝 alg=none 与其他算法 | 标准 claims 校验完整（用户要求） |
| JWT Secret 时机 | `jwt_secret` 为空时 `NewManager` 启动期一次性生成 32 字节随机密钥，仅进程内持有；**不**在每次 Sign 生成 | 保证同进程已签发 JWT 可持续验证（用户要求） |
| OIDC discovery | 启动阶段用带 timeout 的 context 执行 `oidc.NewProvider`；启用 provider 的 discovery 失败 → 装配失败；认证 middleware 不重新 discovery；JWKS 由 go-oidc provider 机制缓存/刷新 | 满足 N5（认证路径零网络） |
| GitHub HTTP | githubProvider 持有专用 `*http.Client`（10s 超时）；生产 baseURL=`https://api.github.com`，测试注入 httptest server；subject=`strconv.FormatInt(id,10)`，name=login，不用 email 作身份 | 隔离真实网络、稳定身份（用户要求） |
| OIDC 库 | `coreos/go-oidc/v3` + `golang.org/x/oauth2` | 动态发现、标准 id_token 校验 |
| JWT 库 | `golang-jwt/jwt/v5` | 事实标准，HS256 本地验签 |
| **nonce** | OIDC 登录生成高熵 nonce 与 state 绑定存储，授权请求携带，callback 校验 id_token.nonce；state/nonce 均 TTL 10 分钟 + 一次性消费；GitHub 无 nonce | 防 ID Token 重放/混淆（用户要求） |
| **JWT 传递** | **禁止 URL 携带 JWT**：callback 生成一次性 ticket（高熵、TTL 2 分钟、绑定已认证 userID）→ 302 `/login?ticket` → `POST /auth/oidc/exchange` 换 JWT → `history.replaceState` 清除 | 防 JWT 进入历史/日志/Referer（用户要求） |
| **认证判别** | 判别顺序：① token 严格匹配 JWT 三段式结构 `^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$` → JWT 本地验签，成功 → OIDC Identity，**失败直接 401，不再查 API Key**；② 其余（含 bootstrap 任意格式、系统生成的 binrag_ Key）→ 按现有 API Key 流程 SHA-256 查库（至多一次）；**不使用 `binrag_` 前缀作为认证分支条件** | API Key 格式完全兼容（含非 binrag_ 前缀，N1）；JWT 验签失败不产生多余 DB 查询；系统生成的 Key（binrag_ 前缀、不含点）永不误入 JWT 分支；bootstrap 若恰好配成三段式形态按 JWT 处理并 401——视为配置错误，plan 明示 |
| **认证失败语义** | JWT 形态验签失败 → 直接 401（不落 API Key 查询）；非 JWT 形态 → API Key 至多一次查询 | 避免无效 JWT 造成无谓 DB 查询（用户要求） |
| state/ticket 分离 | stateStore（绑定 provider+nonce）与 ticketStore（绑定 userID+provider）两个独立结构 | 职责清晰，ticket 不承载 OIDC 语义（用户要求） |
| callback 顺序 | 先原子 `ConsumeState(state)`（读取+删除一体）→ 再 `ExchangeAndVerify(code, nonce)`；state 无效立即失败 | 防并发回调重放（用户要求） |
| `owner_id` | `KnowledgeBase.OwnerID *string`，NULL=系统级；OIDC→UserID、API Key→nil；Store 不推断，API 层显式决定；KB 对外 DTO 剔除 owner_id | 杜绝空串误表达；不暴露归属到前端（用户要求） |
| 启动校验 | `config.Validate()` 静态校验（含 Enabled 时 public_url 必填、provider Name 不重复且非空、ClientID/ClientSecret 非空、type 合法、oidc issuer 必填、oauth2 仅 github、Name 可安全用于 URL path、redirect URL 合法）；`NewManager` 运行时校验（discovery/JWKS） | 配置错误尽早暴露，职责分层（用户要求） |
| id_token 校验 | JWKS 签名 + issuer（仅配置 issuer）+ audience=client_id + exp/nbf + nonce + sub 非空；不信任回调动态指定 issuer | 完整校验范围（用户要求） |
| GitHub API | 带 10s 超时 http client；非 2xx 登录失败；缺数字 id 失败；access token 不入日志、不落库 | 外部调用健壮性（用户要求） |
| GitHub scope | 默认 `["read:user"]` 最小权限，不请求 email | 最小权限（用户要求） |
| 认证路径网络 | 认证中间件零外部网络；仅授权码流程内调用 provider | N5 |
| 票据存储 | 进程内存双 store：crypto/rand 高熵、TTL、一次性消费、sync.Mutex | 单实例够用（YAGNI） |
| JWT 密钥 | 配置 `jwt_secret` 或自动生成随机 | 零配置可跑；重启旧会话失效可接受 |
| `/auth/me` 边界 | middleware 只写 Identity；/me handler 按 UserID 查 Store 取展示名；**不把 Store 注入 auth.Manager** | 保持 auth 无 store 依赖（用户要求） |
| N5 表述 | 认证中间件不产生外部网络调用；JWT 本地验签；API Key 至多现有的一次凭据查询（不宣称严格 O(1)） | 表述准确（用户要求） |
| `OwnerID *string` 兼容 | task.md 列出全部受影响点：`KnowledgeBase{...}` 构造、OwnerID 引用、KB JSON DTO、store mock、测试 fixture、序列化 | 避免编译/API 行为回归（用户要求） |
| 桌面版 | 不接入 | 已确认 |
