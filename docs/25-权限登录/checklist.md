# 权限交验登录（API Key + 多 Provider + 用户知识库隔离）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] users 表与 owner_id 迁移已实现且幂等（验证：`go test ./internal/store/...` 通过，迁移重复执行无错）
- [ ] `GetOrCreateUser` 按 (provider, subject) 自动注册与复用（验证：单测同身份二次调用返回同一 ID）
- [ ] 知识库 owner 过滤正确：`ListAllKBs` 全量、`ListKBsByOwner` 仅本人（验证：单测 4 断言，见 task.md T5）
- [ ] JWT 签发/验签严格（HS256 + iss/exp/nbf + 拒 alg=none）（验证：T7 单测全失败用例）
- [ ] oidcProvider 全校验：签名/issuer/audience/nonce/sub（验证：T9 测试矩阵 8 个失败用例 + 正常通过）
- [ ] githubProvider：OAuth2 授权码 + GitHub API 数字 id 为 subject、专用 client、token 不落库（验证：T10 httptest 单测）
- [ ] Auth 中间件双凭据 + JWT 三段式判别、不使用 binrag_ 前缀（验证：T12 中间件测试 6 项）
- [ ] handler_auth 接口：providers / oidc+github login+callback / exchange / me（验证：T13 httptest 单测）
- [ ] KB/文档/任务/chat/chunk 访问校验 + KB DTO 剔除 owner_id（验证：T15/T16 api 测试，他人库一律 404）
- [ ] Key 管理仅系统级凭据（验证：T17 oidc JWT 调 Key 接口 → 403）
- [ ] 前端双凭据存储 + provider 按钮 + ticket 流程 + 守卫（验证：`npm run test` / `npm run build` 通过）

## 集成

- [ ] auth.Manager 由 app 装配注入，启动校验生效（验证：`go build ./...`；无 OIDC 配置时 Enabled=false 不初始化 provider、启动正常）
- [ ] 认证路径零外部网络：认证中间件不调 provider/userinfo（验证：代码审查 + 中间件测试无网络 mock）
- [ ] `config.Validate()` 静态校验生效：重复 Name / oidc 缺 issuer / oauth2 非 github / 缺 public_url 均启动失败（验证：T2 单测）
- [ ] 公开接口（providers/login/callback/exchange）无凭据可访问，`/me` 及业务接口需认证（验证：T14 路由测试）

## 编译与测试

- [ ] `go build ./...` 无错误
- [ ] `go test ./...` 全部通过
- [ ] `npm run test`（vitest）通过
- [ ] `npm run build` 通过且 `internal/webui/dist` 重新嵌入
- [ ] swagger 注解同步（`swag init` 生成 docs.go，含 /auth/* 接口）

## 端到端场景（对应验收标准）

- [ ] **场景 1（AC1/AC6/AC7/AC13）GitHub 登录 + 知识库隔离**：GitHub 授权（mock）→ 自动建 (github, 数字id) 用户 → 用户 A 创建知识库 → 用户 B 登录后列表不可见、GET/PUT/DELETE 均 404 → 系统级 API Key 可见全部
- [ ] **场景 2（AC3/AC9/AC11）OIDC ticket 流程**：自定义 OIDC provider 授权（mock）→ 回调成功 → `/login?ticket` → exchange 得 JWT → 业务请求带 JWT 成功 → 同一 ticket 二次 exchange 失败（重放）→ 过期 ticket 失败 → 401 时前端清凭据回登录页 → URL 无残留 ticket/JWT
- [ ] **场景 3（AC12）nonce 篡改**：OIDC 回调携带与 stateStore 绑定不一致的 nonce → 登录失败且不创建用户/会话
- [ ] **场景 4（AC5）Key 管理权限**：系统级 API Key 可创建/启停/删除 Key；OIDC 用户调用这些接口 → 403
- [ ] **场景 5（AC8/AC10）历史数据兼容**：升级前已有的无 owner 知识库与旧 API Key → 系统级 Key 可访问、OIDC 用户不可见；迁移幂等可重复执行
- [ ] **场景 6（AC2/AC4）双 Provider 配置**：配置 GitHub + 自定义 OIDC → 登录页两按钮、公开接口返回两者（无机密）；API Key 与 JWT 均可访问受保护接口；`auth_enabled=false` 全部放行
