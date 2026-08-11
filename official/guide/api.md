---
title: API 参考
description: BinRag API 参考 — REST 接口表、MCP 端点、统一响应格式与认证说明。
---

# API 参考

全部 `/api/v1/*` 接口（含 API Key 管理）均需 `Authorization: Bearer <api_key>`；公开的 auth 接口（providers / login / callback / exchange）除外。

## 统一响应格式

```json
{ "code": 0, "message": "ok", "data": { ... } }
```

业务错误码与 HTTP 状态码一致（400 / 401 / 403 / 404 / 409 / 500）。

## REST 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/knowledge-bases` | 创建知识库 |
| GET | `/api/v1/knowledge-bases` | 知识库列表 |
| GET / PUT / DELETE | `/api/v1/knowledge-bases/:id` | 查询 / 更新 / 删除知识库 |
| POST | `/api/v1/documents/upload?kb_id=` | 上传文档（multipart） |
| GET | `/api/v1/documents?kb_id=` | 文档列表 |
| DELETE | `/api/v1/documents/:id` | 删除文档（同步清理向量与索引） |
| GET | `/api/v1/tasks/:id` | 入库任务状态 |
| POST | `/api/v1/tasks/:id/retry` | 手动重试失败任务 |
| POST | `/api/v1/chat` | 问答（`?stream=1` 或 `Accept: text/event-stream` 时流式） |
| GET | `/api/v1/chat/history?session_id=` | 对话历史 |
| POST / GET / DELETE | `/api/v1/api-keys` | API Key 创建 / 列表 / 删除 |
| POST | `/api/v1/api-keys/:id/toggle` | 启用 / 停用 API Key |
| PUT | `/api/v1/api-keys/:id/permissions` | 更新 API Key 的 MCP 权限（bootstrap-only） |
| GET | `/api/v1/config` | 配置视图（含 `mutable.mcp` 分组） |
| PUT | `/api/v1/config` | 更新可修改配置（bootstrap-only） |
| GET | `/api/v1/auth/providers` | 三方登录 Provider 列表（公开） |
| GET | `/api/v1/auth/oidc/:provider/login` | OIDC 授权跳转（公开） |
| GET | `/api/v1/auth/github/login` | GitHub 授权跳转（公开） |
| POST | `/api/v1/auth/exchange` | 登录票据兑换会话 JWT（公开） |
| GET | `/api/v1/auth/me` | 当前登录身份（API Key / 用户） |
| GET | `/api/v1/mcp/my/status` | 「我的 MCP」状态（全局开关 + 我的凭据） |
| POST / DELETE | `/api/v1/mcp/my/key` | 生成（明文仅一次）/ 吊销我的 MCP 凭据 |
| POST | `/api/v1/mcp/my/key/toggle` | 启用 / 停用我的 MCP 凭据 |
| PUT | `/api/v1/mcp/my/key/permissions` | 配置我的 MCP 权限（知识库限自己的） |

## MCP 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/mcp` | MCP streamable HTTP 端点（initialize / tools / call） |

详见 [MCP Server](/guide/mcp)。

## Swagger

运行时服务提供交互式 Swagger UI：

- `/swagger/index.html` — Swagger UI（公开）
- `/swagger/doc.json` — OpenAPI 3 文档 JSON（公开）

接口实测仍需 API Key。
