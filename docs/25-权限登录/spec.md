# 权限交验登录（API Key + 多 Provider 登录 + 用户知识库隔离）Spec

## 背景

BinRag 当前仅有 API Key 认证（`Authorization: Bearer <key>`，SHA-256 hash 校验），知识库全局共享、无用户概念。Web 版（`cmd/server`）与桌面版（`cmd/desktop`，Wails 内嵌）共用同一装配（`internal/app`）。

本次引入用户体系：**保留 API Key 登录，增加多登录 Provider（自定义 Provider 使用 OIDC、GitHub 使用 OAuth2 适配），并按用户隔离知识库**。

## 目标

- **G1** 保留现有 API Key 登录，语义不变：API Key 是系统级服务凭据，可访问所有知识库。
- **G2** 支持一个或多个登录 Provider 三方登录：自定义 Provider 使用 OIDC（授权码 + ID Token），GitHub 使用 OAuth2 适配；登录成功后由后端签发 JWT 会话凭据。
- **G3** 知识库按 owner 用户隔离：登录用户只能看到/操作自己创建的知识库；系统级 API Key 可访问全部。
- **G4** Web 版支持三方登录；桌面版本次不接入（保持 API Key 登录）。

## 功能需求

### 用户与登录 Provider

- **F1** 系统引入用户概念；登录用户首次成功登录时自动创建用户记录（自动注册，无审批流程）。
- **F2** 用户身份唯一性按「登录方式 + 身份标识」确定：API Key 不产生用户；登录用户按 (provider 标识, subject) 唯一，同一自然人用不同 provider 登录视为不同用户（第一版简化）。GitHub 的 subject 为其稳定数字用户 ID。
- **F3** 支持在配置文件中声明多个登录 Provider，每个 provider 独立配置 type（`oidc` / `oauth2`）、client_id、client_secret、issuer（仅 OIDC）、scope 等；运行期可通过公开接口查询已启用的 provider 列表（供前端渲染登录按钮，不含任何机密）。
- **F4** OIDC provider：提供授权码流程入口，重定向到 issuer 授权页并携带一次性随机 state 与 nonce（均服务端存储校验，防 CSRF 与 ID Token 重放）；自定义 OIDC provider 支持任意符合规范的 issuer。
- **F5** GitHub provider：OAuth2 授权码流程，重定向携带一次性随机 state（服务端校验）；回调后以 access token 调 GitHub API 获取稳定数字 ID 作为 subject；GitHub 不校验 nonce / ID Token（无 OIDC 端点）。
- **F6** 回调处理须校验：OIDC——state 一致、ID Token 签名有效、issuer 与 audience 匹配、nonce 一致、subject 非空；GitHub——state 一致、GitHub API 返回非 2xx 或缺少数字 ID 即失败。校验失败返回明确错误且不创建任何会话。
- **F7** 提供会话信息接口：返回当前身份（登录方式：apikey/oidc、用户 ID、provider 名、是否系统级 Key、展示名）。未认证时返回 401。

### 认证与授权

- **F8** 认证中间件同时接受两种凭据：系统级 API Key（现有行为，含 bootstrap 标记）与会话 JWT；任一有效即放行，无效/过期返回 401。
- **F9** JWT 由后端自签（HS256），密钥来自配置项，配置缺失时自动生成随机密钥（重启后旧会话失效，可接受）；JWT 有效期可配置。
- **F10** 会话 JWT 仅用于 Web 前端交互，**不通过 URL 传递**：回调成功后后端签发一次性 ticket，前端用 ticket 换取 JWT；JWT 不参与 API Key 的创建/启停/删除（API Key 管理仅系统级凭据可操作）。
- **F11** `auth_enabled=false` 时（本地开发）所有接口放行，行为与现状一致。

### 知识库隔离

- **F12** 知识库新增 owner 属性（nullable，NULL=系统级）；创建时由 API 层按当前身份显式决定：登录用户 → owner=user.ID；系统级 API Key → owner=NULL。
- **F13** 知识库列表、详情、更新、删除、文档操作均按 owner 过滤：登录用户只能操作自己的知识库；系统级 API Key 可操作全部（含系统级与用户级知识库）。
- **F14** 用户访问他人知识库时返回 404（不泄露存在性）。
- **F15** 无 owner 的历史知识库保持可访问：由系统级 API Key 可见可操作；登录用户不可见。

### 前端

- **F16** 登录页保留 API Key 输入登录，同时动态展示可用的登录 Provider 按钮；点击按钮跳转对应授权页。
- **F17** 三方登录回调后前端拿到一次性 ticket，调用换取接口获得 JWT，存储后请求统一携带 `Authorization: Bearer <JWT>`；401 时清除凭据并回登录页。
- **F18** 提供退出登录：清除本地 JWT（后端可选提供会话作废接口）。

## 非功能需求

- **N1 兼容性**：现有 API Key 登录全流程（创建/列表/启停/删除/认证）行为不变；`auth_enabled`、`bootstrap_api_key` 配置语义不变。
- **N2 数据兼容**：数据库迁移幂等，已有表数据不丢失；历史知识库（无 owner）与历史 API Key 无需人工迁移即可继续使用。
- **N3 安全**：OIDC 回调全链路校验（state、nonce、签名、issuer、audience、client_id）；会话 JWT 不通过 URL 传递（一次性 ticket 交换）；ticket 高熵、短 TTL、一次性消费；GitHub access token 不入日志、不落库；provider 机密只存在于服务端配置。
- **N4 配置驱动**：所有登录 Provider 通过配置文件声明（type 区分 oauth2/oidc），不硬编码 provider 类型；新增 Provider 只需改配置（+ 前端无需改动）。
- **N5 性能**：认证中间件不产生外部网络调用；JWT 为本地验签；API Key 至多执行现有的一次凭据查询。
- **N6 可观测**：认证失败、回调校验失败、JWT 签发/校验失败均输出结构化日志（含 provider 名、错误原因，不含机密与 access token）。

## 不做的事

- **不引入**用户管理界面、用户列表、禁用/审批用户流程。
- **不引入**知识库共享/协作授权（严格私有）。
- **不引入**API Key 与用户的绑定关系（API Key 保持系统级）。
- **不引入**角色权限体系（admin/user 等）——bootstrap API Key 保持现有「高权限标记」语义。
- **不做**桌面版三方登录。
- **不做**刷新令牌 / JWT 自动续期 / 服务端会话作废列表（JWT 短有效期兜底）。
- **不做**除 GitHub 外的非 OIDC/OAuth2 provider 支持（GitHub 为唯一内置 OAuth2 适配；其余 Provider 必须为符合规范的 OIDC）。
- **不持久化**任何 provider 的 access token。

## 验收标准

- **AC1**（F1/F2/F8/F9）用某登录 Provider 完成首次登录 → 自动创建用户、签发 JWT；再次登录同一 (provider, subject) → 复用同一用户。
- **AC2**（F3）配置两个 Provider（GitHub + 自定义 OIDC issuer）→ 登录页展示两个按钮；公开接口返回两者信息且不含 secret。
- **AC3**（F4/F5/F6）完整授权码流程：跳转 → 授权 → 回调 → 一次性 ticket → 换 JWT；篡改 state / 伪造 ID Token / 错误 audience 时回调失败且不签发任何会话。
- **AC4**（F7/F8/F11）API Key、JWT 两种凭据均可访问受保护接口；无效/过期凭据返回 401；`auth_enabled=false` 时全部放行。
- **AC5**（F10）系统级 API Key 可创建/删除/启停 API Key；登录用户调用 Key 管理接口被拒绝（403 或 401）。
- **AC6**（F12/F13）用户 A 创建知识库 → 仅 A 与系统级 Key 可见；用户 B 列表/详情/文档接口看不到该库。
- **AC7**（F14）用户 B 直接 GET/PUT/DELETE 用户 A 的知识库 → 404。
- **AC8**（F15）迁移前已有的无 owner 知识库 → 系统级 Key 可访问；登录用户不可见。
- **AC9**（F16/F17/F18）登录页展示 Provider 按钮并可完成登录；登录后请求携带 JWT；401 自动回登录页；退出后本地凭据清除。
- **AC10**（N1/N2）升级前创建的 API Key 与知识库在升级后功能不回退；全部迁移幂等可重复执行。
- **AC11**（F10）ticket 一次性与时效：第一次 exchange 成功获得 JWT；同一 ticket 第二次 exchange 必须失败；过期 ticket exchange 必须失败；ticket 本身不能直接作为 JWT 使用。
- **AC12**（F4/F6）OIDC 回调中 nonce 被篡改/不匹配 → 登录失败且不创建会话。
- **AC13**（F5/F6）GitHub 登录成功 → 用 GitHub 数字 ID 创建/复用 (github, numeric_id) 用户；伪造/篡改 state 登录失败；GitHub API 返回非 2xx 或缺少数字 ID 时不创建会话。
