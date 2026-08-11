---
title: 快速开始
description: BinRag 安装、配置与启动指南 — Docker 一键启动 / 本地开发构建，含第一个请求示例。
---

# 快速开始

BinRag 推荐用 **Docker 一键启动**（无需安装 Go / Node.js，几分钟即可跑起来）；也可以本地构建运行，便于热重载开发调试。两条路径任选其一。

## 前置条件

- **Docker 方式**：仅需 Docker（含 Compose 插件）
- **本地开发**：Go 1.26+、Node.js 22+、Docker（提供 Qdrant 与 PostgreSQL）

## 方式一：Docker 快速启动（推荐）

仓库提供三个 Compose 文件，覆盖「一键部署 / 免编译生产 / 开发依赖」三种场景：

| 文件 | 用途 |
|---|---|
| `docker-compose.yml` | 一键部署：现场构建镜像 + 启动 PostgreSQL / Qdrant / binrag-server |
| `docker-compose.prod.yml` | 免编译部署：直接拉取 ghcr.io 已发布镜像 |
| `docker-compose.dev.yml` | 开发用：仅启动 Qdrant / PostgreSQL 依赖 |

### 1. 填写部署配置

```bash
vi deploy/configs/config.docker.yaml
```

该文件由 docker compose 挂载进容器（只读），**必须填写**模型服务信息，其余字段保持默认即可：

```yaml
embedder:        # 向量模型（OpenAI 兼容，如 text-embedding-v4 / bge-m3）
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"
  model: "text-embedding-v4"

llm:             # 问答模型（OpenAI 兼容，如 gpt-4o / deepseek）
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"
  model: "gpt-4o"

reranker:        # 重排序（可选；没有专用服务就把 retriever.enable_reranker 改为 false）
  base_url: "https://api.example.com/v1"
  api_key: "sk-xxx"

server:
  bootstrap_api_key: "sk-your-bootstrap-key"   # 首次启动种子密钥，建完第一个 API Key 后删除
```

> 配置文件已内置 `postgres.dsn`（`postgres:5432`）与 `vectorstore.host`（`qdrant:6334`），指向 compose 服务名，无需修改。

### 2. 一键构建并启动

```bash
docker compose up -d --build
```

- 自动完成：构建 `binrag-server` 镜像（前端已通过 go:embed 嵌入二进制）→ 启动 PostgreSQL + Qdrant + binrag-server。
- 首次构建需拉取基础镜像并安装依赖，耗时几分钟，请耐心等待。

### 3. 验证与访问

```bash
docker compose ps                            # 三个服务应均为 running（postgres 为 healthy）
curl -I http://localhost:8085/swagger/index.html   # 返回 200 即服务就绪
```

浏览器打开 <http://localhost:8085> ，输入 `bootstrap_api_key` 登录使用（完整 API 文档见 `/swagger/`）。

### 4. 免编译部署（可选）

镜像由 GitHub Actions（`.github/workflows/docker-publish.yml`）在 main 分支与 `v*` 标签自动构建并推送到 GitHub Container Registry（`ghcr.io/bin-hy/documentsrag`），无需本地构建：

```bash
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

指定版本：

```bash
BINRAG_IMAGE_TAG=v1.2.0 docker compose -f docker-compose.prod.yml up -d
```

### 5. 仅启动依赖（本地开发准备）

```bash
docker compose -f docker-compose.dev.yml up -d
```

启动 Qdrant（向量库，gRPC 6334）与 PostgreSQL（5432，账号 `binrag/binrag`），供方式二本地开发连接。

## 方式二：本地开发

在宿主机直接运行后端 / 前端（热重载调试）。依赖已由方式一第 5 步启动（或手动 `docker compose -f docker-compose.dev.yml up -d`）。

### 1. 准备配置

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

### 2. 构建并启动

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

### 3. 第一个请求

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
