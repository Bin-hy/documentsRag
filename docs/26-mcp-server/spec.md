# MCP Server（只读 RAG 能力）Spec

## 背景

当前项目的核心能力是 RAG（Retrieval-Augmented Generation）：知识库管理、文档上传、解析与 Chunk、Embedding、Vector Store、BM25/Hybrid 检索、Reranker、Query 召回、RAG Pipeline。这些能力目前通过 REST API 与 Web 前端服务 Mew Spec 平台自身。

现在需要在现有 RAG Core 之上增加一层 MCP Server，使其他平台、Agent、AI 应用可以通过 MCP 协议（streamable HTTP）调用 Mew Spec 的 RAG 能力。

**首版定位为只读 RAG 能力服务**：只暴露查询/检索/问答/任务状态类 Tool，知识库创建、文档上传等管理类写入操作继续通过现有 REST/Web 管理端完成。

## 目标

- 提供 streamable HTTP 传输的 MCP Server，与现有服务同进程嵌入，复用现有 RAG Core、存储、认证与限流能力
- 提供 6 个只读 Tool，覆盖知识库查看、检索、问答、文档列表与任务状态查询
- 建立完整服务体系：API Key 认证、Tool 权限控制、知识库资源权限、调用审计
- 向后兼容：新增 API Key 权限字段不影响现有 REST API 行为；MCP 权限需显式授予，历史 Key 不自动获得

## 功能需求

- F1: MCP Server 通过 streamable HTTP 协议提供 MCP 服务（支持 2025-03-26 及以后协议版本），端点路径可配置（默认 `/mcp`），与现有服务同进程
- F2: 提供以下 6 个只读 Tool，全部按当前凭据（API Key）的可访问范围过滤数据：
  - F2.1 `list_knowledge_bases`：列出当前凭据可访问的知识库（全部或白名单）
  - F2.2 `get_knowledge_base`：按 ID 返回单个知识库详情；无权限按不存在处理
  - F2.3 `retrieve`：纯检索。输入查询文本 + 可选知识库范围，返回召回的 chunk 及来源信息（复用现有检索/Reranker 链路）
  - F2.4 `ask`：RAG 问答。输入问题 + 可选知识库范围，返回回答与引用来源；不提供 `thinking` 参数，不暴露任何内部推理（见「不做的事」）
  - F2.5 `list_documents`：列出指定知识库（或当前凭据可访问范围）内的文档
  - F2.6 `get_task`：按任务 ID 查询入库任务状态（pending/processing/completed/failed）与错误信息；**必须**先按任务所属知识库校验当前凭据的资源权限，无权限按不存在处理（错误语义见 F5）
- F3: 认证——MCP 请求使用 `Authorization: Bearer <API Key>` 认证（复用现有 API Key SHA-256 校验）；缺失、无效、已停用的 Key 一律拒绝，返回 **401（认证失败）**；支持 bootstrap Key
- F4: Tool 权限控制——每个 API Key 可配置允许调用的 Tool 集合（白名单）；权限未配置（空）即无任何 MCP Tool 权限，必须显式配置后才可调用；调用未授权 Tool 返回 **403（授权失败）**
- F5: 知识库资源权限——每个 API Key 可配置知识库范围（全部 或 指定白名单）；`retrieve`/`ask`/`list_documents`/`get_knowledge_base`/`get_task` 涉及的知识库必须在凭据可访问范围内，越权返回 **403（授权失败，不泄露存在性）**；不指定 `kb_id` 时自动使用凭据可访问的范围（未配置则无知识库可访问）
- F6: API Key 管理扩展——API Key 新增权限配置（Tool 白名单、知识库范围），提供查看/更新权限的管理接口（仅系统级 API Key 可操作）；权限未配置即无 MCP 权限，必须显式授予；**历史 API Key 不会因 schema 迁移自动获得任何 MCP 权限**；启用/停用/删除复用现有能力
- F7: 调用审计——每次 Tool 调用记录：`api_key_id`、Tool 名、参数（截断）、结果状态、耗时、时间，供事后追溯；**绝不记录 API Key Secret（明文），仅记录 Key ID 引用**
- F8: 异步任务状态查询——`get_task` 复用现有任务系统，且**必须执行资源权限校验**（与现有 REST `GET /api/v1/tasks/:id` 一致：先查任务、按其知识库校验凭据权限，越权按不存在处理）

## 非功能需求

- N1: 与现有服务同进程，复用 Store、RAG Engine、认证中间件、限流
- N2: 权限判断与现有 API 一致：知识库越权一律按「不存在/无权限」处理，不泄露存在性
- N3: 审计写入不阻塞主流程（低开销、失败仅告警）
- N4: 审计参数截断：单个参数值截断至可配置长度（默认 2000 字符），避免敏感内容与超大参数入库
- N5: 认证路径无外部网络调用
- N6: MCP 端点受现有 RateLimit 保护

## 不做的事

- 不做用户启用/禁用（本版）
- 不提供知识库管理与文档写入类 Tool（create_kb、upload_document、delete_kb、delete_document 等）
- 不支持 stdio 传输
- 不支持 MCP 流式问答（`ask` 以完整回答返回，不转发流式事件）
- 不暴露内部思考/推理（`ask` 不提供 `thinking` 参数）
- 不做 API Key 与用户的绑定
- 不暴露联网搜索增强（enhanced）等非只读扩展

## 验收标准

- AC1: 服务启动后，MCP 端点可完成 `initialize` → `tools/list` → `tools/call` 完整握手
- AC2: 6 个 Tool 全部注册且可被调用，输出符合预期
- AC3: 缺失 / 无效 / 已停用的 API Key 的 MCP 请求被拒绝（401）
- AC4: 未配置 Tool 权限的 Key 无法调用任何 MCP Tool；配置白名单后，调用未授权 Tool 被拒绝（403）
- AC5: 配置了知识库白名单的 Key，访问白名单外知识库被拒绝（403）
- AC6: 管理接口可查看 / 更新 API Key 的 Tool 白名单与知识库范围（仅系统级 Key）；迁移后历史 Key 无任何 MCP 权限，须显式配置
- AC7: 每次 Tool 调用产生一条审计记录，含 `api_key_id`、截断参数与结果状态，且不含任何 API Key Secret
- AC8: `get_task` 返回正确的任务状态与错误信息；对无权限知识库的任务按不存在处理（资源权限校验生效）
- AC9: 现有 REST API 测试全部通过（向后兼容）
