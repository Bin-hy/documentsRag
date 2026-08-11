# Web 端 MCP 管理 Checklist（v2：用户维度 MCP 凭据）

> 每一项通过运行代码或观察行为验证。v1 条目（系统级管理）已通过上轮验证；v2 条目（用户维度）为本轮新增。

## 实现完整性（系统级管理，v1）

- [ ] 设置页「MCP Server」卡片显示全局 enabled/path/audit_param_limit；保存提示重启生效（验证：部署新构建后打开设置页 + bootstrap 保存）
- [ ] API Key 列表显示 MCP 权限列（Tool tag、范围徽标、空 =「未配置」）（验证：ApiKeysView）
- [ ] bootstrap Key 可编辑系统级 Key 的 MCP 权限；非 bootstrap 按钮禁用 + tooltip（验证：ApiKeysView 两种凭据）
- [ ] `GET /api/v1/config` 返回 `mutable.mcp`；`PUT /api/v1/config` 可改 server.mcp（bootstrap-only，非法 path 400）（验证：curl/测试）

## 实现完整性（用户维度 MCP，v2）

- [ ] 「我的 MCP」页（/my-mcp）可访问，显示全局状态、凭据区、权限配置、连接信息（验证：OIDC 会话打开页面）
- [ ] 全局 `mcp.enabled=false` 时页面显示「MCP 服务未开启」提示且操作禁用（验证：关闭全局后刷新页面）
- [ ] 无凭据时显示生成按钮；点击生成后一次性展示明文 Key + 复制（验证：首次生成）
- [ ] 再次生成 → 409 提示「已有凭据，请吊销后重建」（验证：重复点击生成）
- [ ] 凭据启用开关：停用后凭据 enabled=false；MCP 调用该 Key → 401（停用拒绝）（验证：开关 + MCP 调用）
- [ ] 吊销凭据：确认后删除，页面恢复「生成」态；原 Key MCP 调用 → 401（验证：吊销 + MCP 调用）
- [ ] 权限配置：Tool 勾选 + 范围单选 + KB 多选（数据 = 自己的知识库）（验证：编辑并保存）
- [ ] 保存权限后 status 返回新权限；MCP 调用按新权限执行（白名单内成功、外 -32001）（验证：端到端）
- [ ] 连接信息展示 endpoint + 自己的凭据 + mcpServers 示例可复制（验证：页面 + 复制）

## 集成与隔离

- [ ] 用户凭据 `scope=all`：`list_knowledge_bases` 只返回自己的知识库；`get_knowledge_base`/`retrieve` 他人/系统级 KB → -32001（验证：MCP 调用）
- [ ] 用户凭据 `allowlist` 含他人 KB id：被过滤，调用该 KB → -32001（验证：MCP 调用）
- [ ] 用户配置权限传非自己 KB 的 id → 400（验证：PUT permissions 越权 id）
- [ ] 用户看不到/操作不了他人凭据（status 只返回自己的；接口按 UserID 过滤）（验证：两个用户交叉验证）
- [ ] 非会话（API Key）访问 /mcp/my/* → 403（验证：curl API Key）
- [ ] 每用户凭据唯一：owner_id 部分唯一索引生效（重复创建被 409 拦截；数据库层唯一）（验证：测试 + 建表检查）
- [ ] 系统级 Key（owner 空）行为不变：scope=all 可访问全部 KB（验证：MCP 调用系统级 Key 回归）

## 编译与测试

- [ ] 后端 `go build ./...`；`go vet` 无告警
- [ ] 后端 `go test -race ./internal/api/ ./internal/store/ ./internal/mcp/ ./internal/config/ -count=1` 全绿
- [ ] 前端 `npm run build` 通过（vue-tsc + vite）
- [ ] `gofmt -l internal/` 空
- [ ] migration 幂等：真实 PG 重跑 Migrate 不报错（owner_id 列 + 唯一索引存在）

## 端到端场景

- [ ] **场景 1（用户自助闭环）**：OIDC 用户登录 → 「我的 MCP」页 → 生成凭据（明文复制）→ 配置 Tool `retrieve`/`ask` + `allowlist`（选 2 个自己的 KB）→ 启用 → 用该 Key 调 MCP `tools/call retrieve`（自己 KB）成功、`get_knowledge_base`（他人 KB）→ -32001 → 吊销 → 原 Key 调用 401
- [ ] **场景 2（双层开关）**：全局 `mcp.enabled=false` → 页面提示不可用、`POST /mcp` → 404；全局开启 + 用户停用自己的凭据 → MCP 401；全局开启 + 用户启用 → MCP 可用
- [ ] **场景 3（v1 回归）**：bootstrap Key 打开设置页 MCP 卡片 → 保存提示重启生效；ApiKeysView 编辑系统级 Key 权限 → 生效；系统级 Key MCP 调用（scope=all）仍可访问全部
