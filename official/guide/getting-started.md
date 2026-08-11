---
title: 快速开始
description: BinRag 安装、配置与启动指南 — 前置条件、基础设施、构建启动与第一个请求。
---

# 快速开始

## 前置条件

- Go 1.26+
- Docker（提供 Qdrant 与 PostgreSQL）
- Node.js 22+（仅构建前端 / 官网时需要）

## 1. 启动基础设施

```bash
docker compose up -d
```

启动 Qdrant（向量库）与 PostgreSQL（元数据存储）。

## 2. 准备配置

```bash
cp configs/config.yaml configs/config.local.yaml
```

编辑 `configs/config.local.yaml`，填入模型服务信息（OpenAI 兼容地址均可）：

> `configs/config.local.yaml` 是本地私有覆盖文件（已被 .gitignore 忽略）。启动时自动合并——local 中出现的字段覆盖 `config.yaml`，未出现的保留主配置值。

```yaml
embedder:        # 向量模型（如 text-embedding-v4 / bge-m3）
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"
  model: "text-embedding-v4"
  dimension: 1024

llm:             # 问答模型
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"
  model: "gpt-4o"

reranker:        # 重排序模型（可选）
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"
  model: "bge-reranker-v2-m3"

server:
  bootstrap_api_key: "sk-your-bootstrap-key"   # 首次启动种子密钥
```

可选启用 MCP Server 与三方登录，见 [MCP Server](/guide/mcp) 与 [登录认证](/guide/auth)。

## 3. 构建并启动

Web 形态（单一可执行文件，自带前端界面）：

```bash
# 首次需构建前端产物（输出到 internal/webui/dist，被 go:embed 打包进二进制）
cd frontend && npm install && npm run build && cd ..

# 编译并启动
go build -o binrag-server ./cmd/server
./binrag-server -c configs/config.local.yaml
```

启动后浏览器访问 `http://localhost:8085`（端口见配置 `server.port`），输入 API Key 登录使用。

其他启动方式：

```bash
go run ./cmd/server                       # 默认 ./configs/config.yaml
BINRAG_CONFIG=configs/prod.yaml go run ./cmd/server
```

启动时可用 `bootstrap_api_key` 获取首个访问密钥（使用后建议从配置中移除）。

## 4. 第一个请求

```bash
# 创建知识库
curl -X POST http://localhost:8085/api/v1/knowledge-bases \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"产品文档库","description":"产品手册与 FAQ"}'

# 上传文档（异步入库，返回 task_id）
curl -X POST "http://localhost:8085/api/v1/documents/upload?kb_id=<kb_id>" \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -F "file=@docs/intro.md"

# 查询任务状态
curl http://localhost:8085/api/v1/tasks/<task_id> \
  -H "Authorization: Bearer sk-your-bootstrap-key"

# 普通问答
curl -X POST http://localhost:8085/api/v1/chat \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"demo","question":"产品支持哪些文档格式？","kb_id":"<kb_id>"}'

# 流式问答（SSE）
curl -N -X POST "http://localhost:8085/api/v1/chat?stream=1" \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"demo","question":"如何部署？","kb_id":"<kb_id>"}'
```
