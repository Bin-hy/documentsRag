# Web 端 MCP 管理 Spec（v2：用户维度 MCP 凭据）

## 背景

MCP Server（只读 RAG 能力服务）后端已实现：streamable HTTP 端点、6 个只读 Tool、API Key 认证（401）/授权（-32001）、Key 级权限、异步审计。

**v1 迭代**完成了 Web 端基础 MCP 管理：API Key 的 MCP 权限展示与编辑（bootstrap-only）、设置页 MCP 全局开关卡片与连接信息、后端 config 支持 `server.mcp`。

**用户反馈（v2 驱动）**：
1. 部署的 Web 界面看不到 MCP 配置（旧构建）
2. MCP 配置启用受 bootstrap 权限限制，普通用户无法自助使用
3. 需求：**每个用户都具备自己的 MCP，自己控制开启/关闭**——用户维度 MCP 凭据自助管理

## 目标

- 每个登录用户（OIDC）在独立「我的 MCP」页面自助生成/管理自己的 MCP 访问凭据（绑定用户的 API Key）
- 用户可自行配置自己的 MCP 权限（Tool 白名单 + 知识库范围，限于自己的知识库）与启停开关，**不依赖 bootstrap**
- 双层开关：全局 `mcp.enabled`（bootstrap 管部署级，决定 /mcp 路由挂载）+ 用户级开关（用户控制自己的凭据启用）
- 系统级 API Key 管理（ApiKeysView）与全局设置（SettingsView MCP 卡片）保留，归 bootstrap 管理

## 功能需求

### A. 系统级管理（v1 保留）

- F1: API Key 列表展示每个 Key 的 MCP 权限（Tool 白名单、知识库范围徽标、白名单数量；空 =「未配置」）
- F2: 系统级 Key 的 MCP 权限编辑对话框（6 Tool 勾选 + 范围单选 + 知识库多选），保存调 `PUT /api/v1/api-keys/:id/permissions`
- F3: 系统级权限编辑仅 bootstrap API Key 可用（复用 `GET /config` 的 `is_bootstrap`），否则禁用并提示
- F4: 设置页「MCP Server」卡片：全局 `enabled`/`path`/`audit_param_limit` 编辑（bootstrap-only），保存提示重启生效
- F5: 设置页展示连接信息（endpoint、Bearer 认证、6 Tool 列表、mcpServers 示例可复制）
- F6: 后端 config 管理支持 `server.mcp`（GET /config mutable.mcp 分组 + 更新接口，bootstrap-only）

### B. 用户维度 MCP（v2 新增）

- F7: 「我的 MCP」独立页面（新路由，登录用户可访问）：
  - 展示全局 MCP 状态（`mcp.enabled`，只读提示「服务未开启时不可用」）
  - 用户自己的启用开关（控制自己的 MCP 凭据是否生效）
  - 自己的 MCP 凭据：一键生成（绑定当前用户）/ 复制明文（仅一次）/ 吊销
  - 自己的 Tool 权限配置（6 个 Tool 勾选）
  - 自己的知识库范围配置（范围单选 + 白名单多选，**可选项限于自己的知识库**）
  - 连接信息（endpoint + 自己的凭据）
- F8: 用户自助管理接口——登录用户可创建（owner=自己）、查看（仅自己的）、启停、删除、配置权限自己的 MCP 凭据，**不要求 bootstrap**
- F9: 知识库范围限定——用户 MCP 权限的知识库可选项 = 自己的知识库（owner_id=自己）；`scope=all` 也只检索自己的知识库，无法访问系统级/他人知识库（与现有用户知识库隔离一致）
- F10: 双层开关——全局 `mcp.enabled=false` 时所有用户 MCP 不可用（路由未挂载）；用户级开关 = 自己凭据的启用状态，全局开启时用户凭据停用则不可用

## 非功能需求

- N1: 用户数据隔离——用户只能管理/访问自己的 MCP 凭据与知识库，不可越权查看他人（MCP 授权与 Web 管理双路径）
- N2: 系统级 Key（owner 为空）行为保持不变（全量权限，bootstrap 管理）
- N3: 复用现有 bootstrap 鉴权语义（系统级管理）与用户会话鉴权（用户自助）
- N4: 权限字段展示一致（空 =「未配置」，不出现 null）
- N5: 与既有页面风格一致（Element Plus）
- N6: 后端改动聚焦（schema 加列、接口扩展、MCP 授权按 owner 限定），不动 MCP 运行时核心

## 不做的事

- 不做 MCP 审计日志查看（后续迭代）
- 不做多租户独立 MCP 服务实例（单 Server，用户凭据隔离）
- 不做用户间的知识库共享/授权（共享权限后续迭代）
- 不做用户启停管理（本版）

## 验收标准

- AC1: 部署构建后设置页显示「MCP Server」卡片（全局开关/参数/连接信息）；API Key 页显示 MCP 权限列（v1 全部生效）
- AC2: bootstrap Key 可编辑系统级 Key 的 MCP 权限；非 bootstrap 禁用并提示
- AC3: 登录用户在「我的 MCP」页可生成自己的 MCP 凭据，复制明文（仅一次）、可吊销
- AC4: 用户可配置自己的 Tool 白名单与知识库范围（知识库可选项 = 自己的知识库）；保存后生效
- AC5: 用户可启停自己的 MCP 凭据（用户级开关），停用后 MCP 调用拒绝
- AC6: 用户的 MCP 凭据即使 `scope=all` 也只能访问自己的知识库（越权他人知识库返回 -32001）
- AC7: 用户不能查看/管理他人的 MCP 凭据（接口层隔离）
- AC8: 全局 `mcp.enabled=false` 时「我的 MCP」页提示不可用（MCP 端点 404）
- AC9: 系统级 API Key 行为不变（v1 全量权限）；bootstrap 管理接口不受影响
- AC10: 前端构建通过；后端 api 测试全绿（含用户维度接口与 owner 隔离用例）
