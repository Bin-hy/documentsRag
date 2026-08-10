# MCP Server（只读 RAG 能力）Checklist

> 每一项通过运行代码或观察行为验证，聚焦系统行为。MCP 层授权失败以 JSON-RPC error `-32001` 表达（HTTP 200）；「403」仅指 REST 管理接口的真实 HTTP 403。

## 实现完整性

- [ ] `mcp.enabled=false`（默认）时 `/mcp` 不挂载（验证：GET /mcp → 404）
- [ ] `mcp.enabled=true` 时 `/mcp` 挂载且受既有 RateLimit 保护（验证：开启后请求 /mcp 不 404）
- [ ] `tools/list` 返回全部 6 个 Tool（验证：MCP 客户端 tools/list，逐个核对名称）
- [ ] `list_knowledge_bases` 返回凭据可访问的知识库（验证：调用并与 DB 数据对比）
- [ ] `get_knowledge_base` 返回知识库详情（验证：对有效 kb_id 调用，字段齐全）
- [ ] `retrieve` 返回召回 chunk 与来源信息（验证：对已建索引的知识库调用，观察 content/score/metadata）
- [ ] `ask` 返回 `answer` 与 `sources`，**不含 thinking 字段或内部推理**（验证：调用后检查响应 JSON 字段集合）
- [ ] `list_documents` 返回文档列表（验证：调用，与 DB 对比）
- [ ] `get_task` 返回任务状态与错误信息（验证：对 completed/failed 任务各调用一次）
- [ ] 配置默认值正确：`Path=/mcp`、`AuditParamLimit=2000`、`Enabled=false`（验证：`go test ./internal/config/...`）

## 认证与授权

- [ ] 缺失 `Authorization` 头 → HTTP 401，不进入 JSON-RPC（验证：无头请求）
- [ ] 无效 / 已停用 API Key → HTTP 401（验证：伪造 Key、停用 Key 各请求一次）
- [ ] **bootstrap Key 不能作为 MCP 调用凭证**（验证：用 bootstrap Key 调 tool → 无权限拒绝，不绕过）
- [ ] 未配置任何 MCP 权限的 Key（含**迁移前的历史 Key**）调用任何 tool → `-32001` 无权限（验证：历史 Key 调 retrieve/ask）
- [ ] 配置 Tool 白名单后：白名单内 tool 可调用、白名单外 tool → `-32001`（验证：同一 Key 调两个 tool）
- [ ] `mcp_kb_scope=allowlist`：白名单内 kb_id 可访问、白名单外 kb_id → 「知识库不存在或无权限」（验证：retrieve/ask 各一次）
- [ ] `mcp_kb_scope=all`：可访问全部知识库（验证：跨多个库 retrieve）
- [ ] `mcp_kb_scope=''`（无知识库权限）：即使有 tool 权限，未指定 kb_id 的 retrieve/ask → `-32001`（验证）
- [ ] `get_task` 对无权限知识库的任务 → 「任务不存在」（验证：越权任务 ID 调用，与真实不存在的任务响应一致，不泄露存在性）

## 审计

- [ ] 成功 tool 调用产生审计记录：`api_key_id`、`tool_name`、`status=success`（验证：查 `mcp_audit_logs`）
- [ ] 失败 tool 调用产生审计记录：`status=error` + `error_message`（验证：越权调用后查表）
- [ ] 超长参数：`params` 长度 ≤ 2000、`params_len` = 截断前原始长度（验证：传超长 query 后查表比对）
- [ ] 审计记录**不含任何 API Key Secret / Authorization Token**（验证：查表字段集无 secret 列；参数中不出现明文 Key）
- [ ] 审计不阻塞主请求：队列满时丢弃并 warn，请求正常完成（验证：`go test -race` AuditSink 满队列用例）
- [ ] `Shutdown` flush：停止服务前已投递的审计全部落库（验证：测试断言 Shutdown 后 worker 消费完，无 goroutine 泄漏）

## REST 管理接口

- [ ] 系统级 API Key 可查询任意 Key 的 MCP 权限（验证：`GET /api/v1/api-keys` 返回 `mcp_tools/mcp_kb_scope/mcp_kb_ids`）
- [ ] 系统级 API Key 可更新 Key 的 MCP 权限（验证：`PUT /api/v1/api-keys/:id/permissions` 后再次 GET 确认生效）
- [ ] 会话 JWT（非系统级身份）更新权限 → HTTP 403（验证）
- [ ] 迁移后历史 Key 的权限字段为空（无任何 MCP 权限）（验证：迁移后 `SELECT` 权限列）
- [ ] 现有 REST API 行为不受影响（验证：既有 api/auth/kb 测试全绿）

## 编译与测试

- [ ] `go build ./...` 无错误
- [ ] `go test ./... -race` 全部通过
- [ ] `go vet ./...` 无告警
- [ ] migration 幂等：连续执行两次 `Migrate` 均不报错（验证：真实 PG 上重跑迁移 / 重启服务）
- [ ] mcp-go 探针结论已落实：`-32001` 实际返回路径与 T6 实现一致（验证：tools/call 越权响应体）

## 端到端场景

- [ ] **场景 1（完整链路）**：`mcp.enabled=true` 启动服务 → 创建 API Key → 配置 Tool 白名单（`list_knowledge_bases`、`retrieve`、`ask`）+ `mcp_kb_scope=all` → MCP 客户端 `initialize` → `tools/list` → `ask`（对已建知识库）→ 返回回答与 `sources` → 查 `mcp_audit_logs` 存在对应记录（含 `api_key_id`、截断参数、`status=success`）
- [ ] **场景 2（安全链路）**：`mcp.enabled=true` → 历史 Key（无权限字段）调 `retrieve` → `-32001`；无效 Key → HTTP 401；allowlist 外 `kb_id` → 「知识库不存在或无权限」；无权限知识库的 `get_task` → 「任务不存在」
- [ ] **场景 3（默认关闭）**：默认配置（`mcp.enabled=false`）启动 → `GET /mcp` → 404，REST API 正常可用

> 注：场景 1/2/3 与 migration 幂等需真实 PostgreSQL（本地 docker-compose 或 CI 环境）；环境不可用时应明确报告降级验证方式，不得以「应该没问题」代替。
