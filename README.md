# BinRag

**企业级多类型文档知识库问答系统** —— 基于 Go 实现的文档 RAG（Retrieval-Augmented Generation）服务，将你的文档资产转化为可对话的知识库。

[![Go Version](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev/)
[![Qdrant](https://img.shields.io/badge/Vector%20Store-Qdrant-red)](https://qdrant.tech/)
[![PostgreSQL](https://img.shields.io/badge/Metadata-PostgreSQL-336791)](https://www.postgresql.org/)

## ✨ 特性

- **多格式文档解析** — 支持 TXT / Markdown / PDF / DOCX / CSV / Excel / HTML，解析为统一结构后入库
- **混合检索** — 向量语义检索 + BM25 关键词检索，RRF 加权融合，兼顾语义理解与精确匹配
- **重排序精排** — Cross-encoder 重排序（兼容 Jina / Cohere / 本地 bge-reranker 等 API），提升最终召回质量
- **LLM 问答编排** — Query 改写消解指代 → 上下文按 token 预算组装 → 生成回答并标注引用来源
- **流式输出** — 基于 SSE 的流式问答，引用来源先行、正文增量返回
- **多知识库隔离** — 单向量集合 + payload 过滤，检索按知识库范围严格隔离
- **异步入库** — 上传即返回任务 ID，后台 worker 池执行入库，失败自动/手动重试，状态持久化
- **API Key 认证** — 密钥哈希存储、启停管理、使用时间追踪，接口安全可控
- **OIDC / GitHub 三方登录** — 支持任意符合规范的 OIDC Provider 与 GitHub OAuth2，登录后签发会话 JWT；登录用户拥有独立的知识库归属与「我的 MCP」自助凭据，与系统级 API Key 双通道并存
- **MCP Server** — 基于 MCP 协议（streamable HTTP）的只读 RAG 能力服务：外部平台 / Agent / AI 应用可通过 `tools/call` 调用知识库问答、检索等 6 个 Tool；支持 API Key 认证（401）/ 授权（-32001）、Key 级与用户维度权限、双层开关与调用审计
- **Swagger 文档** — 基于 swaggo 自动生成的 OpenAPI 3 文档与交互式 UI（`/swagger/`），全部接口含参数与响应说明
- **RAG 评估** — 独立评估 CLI（`cmd/eval`）：数据集驱动的 Recall@K 检索评估 + LLM-as-Judge 准确性/忠实度评分，为分块/Embedding/Prompt 调优提供量化依据
- **Web 前端界面** — Vue 3 + TypeScript 单页应用：对话问答（流式打字机 / Markdown 渲染 / 引用来源卡片 / 多会话）、知识库管理、文档拖拽上传与任务跟踪、API Key 管理、**「我的 MCP」自助管理页**，由 Go 二进制直接托管（单一可执行文件）
- **桌面应用** — Wails v3 打包 macOS 安装包（.app / .dmg），内嵌完整后端，安装即用；Web 与桌面共用同一套前端代码

## 🏗 架构

```mermaid
graph TB
    subgraph "前端（frontend/）"
        UI[Vue 3 + TypeScript SPA] --> HTTP[HTTP / SSE]
        UI2[Wails 桌面窗口] --> HTTP
    end

    subgraph API层
        HTTP --> AUTH[认证: API Key / OIDC 会话 JWT]
        AUTH --> KB[知识库管理]
        AUTH --> DOC[文档上传]
        AUTH --> CHAT[问答 / SSE 流式]
    end

    subgraph MCP层
        EXT[外部 Agent / 平台] -->|streamable HTTP /mcp| MCP[MCP Server]
        MCP --> MCPAUTH[API Key 认证 / 授权]
        MCPAUTH -->|6 个只读 Tool| RAGM[MCP 调用 RAG 能力]
        RAGM --> R
    end

    subgraph 入库链路
        DOC --> Q[任务队列]
        Q --> P["Pipeline: Load → Chunk → Embed → Store"]
        P --> V[(Qdrant 向量库)]
        P --> B[BM25 内存索引]
        P --> PG[(PostgreSQL 元数据)]
    end

    subgraph 问答链路
        CHAT --> R[RAG Engine]
        R --> QW[Query 改写]
        QW --> RET["混合检索: 向量 + BM25 + RRF"]
        RET --> RR[Cross-encoder 重排序]
        R --> LLM[LLM 生成]
        V --> RET
        B --> RET
        PG --> R
    end
```

- **Web 形态**：`cmd/server` 启动 HTTP 服务并托管前端静态资源（`internal/webui` go:embed），浏览器访问即得完整界面
- **桌面形态**：`cmd/desktop`（Wails v3）启动内嵌后端监听 `127.0.0.1` 随机端口，窗口直接加载该地址——前端代码零差异、同源无代理层
- **装配复用**：`internal/app` 统一装配存储 / worker / 引擎 / 路由，两种形态共享同一套启动逻辑

## 🧰 技术栈

| 层次 | 技术 |
|------|------|
| 语言 | Go 1.26+ |
| Web 框架 | Gin |
| 向量数据库 | Qdrant（gRPC） |
| 元数据存储 | PostgreSQL（pgx / 连接池） |
| Embedding / LLM | OpenAI 兼容接口（GPT / 豆包 / DeepSeek / 本地 vLLM 等，base_url 切换） |
| 检索 | 向量 + BM25 + RRF 融合 + Cross-encoder 重排序 |
| MCP | mark3labs/mcp-go（streamable HTTP，2025-03-26 协议） |
| 评估 | 数据集 + Recall@K + LLM-as-Judge（准确性/忠实度），`cmd/eval` CLI |
| API 文档 | swaggo 生成 OpenAPI 3 + Swagger UI（`/swagger/`） |
| 前端测试 | vitest（SSE 解析 / 流式降级状态机） |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus + Pinia + marked/highlight.js |
| 桌面壳 | Wails v3（macOS .app / .dmg，内嵌 Go 后端） |
| 认证 | API Key（SHA-256 哈希存储）+ OIDC / GitHub 三方登录（会话 JWT） |
| 容器化 | Docker Compose 一键部署（Qdrant + PostgreSQL + 服务镜像，ghcr.io 自动发布） |

## 🚀 快速开始

### 前置条件

- Go 1.26+
- Docker（提供 Qdrant 与 PostgreSQL）

### 1. Docker 方式（推荐，无需本地安装 Go）

仓库提供三个 Compose 文件，覆盖「一键部署 / 免编译生产 / 开发依赖」三种场景：

**① 一键部署（构建镜像 + 数据库 + 运行）**

```bash
# 先编辑部署配置，填入你的模型服务信息（embedder / llm / reranker 与 bootstrap_api_key）
vi deploy/configs/config.docker.yaml

# 构建镜像并启动全部服务（PostgreSQL + Qdrant + binrag-server）
docker compose up -d --build
```

启动后访问 `http://localhost:8085`。构建产物同时打 tag `ghcr.io/bin-hy/documentsrag:latest`，可用 `docker push` 手动发布。

**② 免编译部署（直接拉取 ghcr.io 已发布镜像）**

镜像由 GitHub Actions（`.github/workflows/docker-publish.yml`）在 main 分支与 `v*` 标签发布时自动构建并推送到 GitHub Container Registry：

```bash
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

指定版本：`BINRAG_IMAGE_TAG=v1.2.0 docker compose -f docker-compose.prod.yml up -d`

**③ 仅启动开发依赖（数据库等）**

本地开发时只需 Qdrant 与 PostgreSQL，后端 / 前端跑在宿主机便于热重载：

```bash
docker compose -f docker-compose.dev.yml up -d
```

启动 Qdrant（向量库，gRPC 6334）与 PostgreSQL（元数据存储，5432，账号 `binrag/binrag`）。

### 2. 本地开发方式

以下流程适用于在宿主机直接运行后端 / 前端（热重载调试）。依赖已由方式一 ③ 启动（或手动 `docker compose -f docker-compose.dev.yml up -d`）。

### 3. 准备配置

```bash
cp configs/config.yaml configs/config.local.yaml
```

编辑 `configs/config.local.yaml`，填入你的模型服务信息（OpenAI 兼容地址均可）：

> 约定：`configs/config.local.yaml` 是本地私有覆盖文件（已被 .gitignore 忽略）。
> 启动时**自动合并**——local 中出现的字段覆盖 `config.yaml`，未出现的字段保留主配置值；
> 因此默认启动（桌面版 / 无 `-c` 参数）也会自动应用 local 里的配置（如 `web_search.api_key`、本地模型地址）。
> 若希望完全独立于 `config.yaml`，仍可用 `-c configs/config.local.yaml` 显式指定（此时不再重复合并 local 自身）。

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

oidc:                # 三方登录（可选；不启用则仅 API Key）
  enabled: false
  public_url: "https://rag.example.com"        # 外部可访问基址（拼回调地址）
  jwt_secret: ""                                # 会话 JWT 密钥（留空=启动时随机生成）
  providers:
    - name: github                              # GitHub OAuth2（type 固定 oauth2）
      type: oauth2
      display_name: GitHub
      client_id: ""
      client_secret: ""
    # - name: company                          # 自定义 OIDC Provider
    #   type: oidc
    #   display_name: 公司 SSO
    #   client_id: ""
    #   client_secret: ""
    #   issuer: "https://sso.company.com"
```

### 4. 构建并启动服务

Web 形态（单一可执行文件，自带前端界面）：

```bash
# 首次需构建前端产物（输出到 internal/webui/dist，被 go:embed 打包进二进制）
cd frontend && npm install && npm run build && cd ..

# 编译并启动
go build -o binrag-server ./cmd/server
./binrag-server -c configs/config.local.yaml
```

启动后浏览器访问 `http://localhost:8085`（端口见配置 `server.port`），输入 API Key 即可登录使用完整界面。

或指定其他配置文件 / 环境变量 / 默认路径（`-c` 优先级最高）：

```bash
go run ./cmd/server                       # 默认 ./configs/config.yaml
BINRAG_CONFIG=configs/prod.yaml go run ./cmd/server
```

启动时可用 `bootstrap_api_key` 获取首个访问密钥（使用后建议从配置中移除）。

### 5. 第一个请求

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

## 🛠 构建与打包

### Web 版（多平台交叉编译）

Web 版为纯 Go（前端已嵌入二进制，无 CGO），可交叉编译任意平台：

```bash
# 前端产物只需构建一次（输出到 internal/webui/dist）
cd frontend && npm install && npm run build && cd ..

# 交叉编译示例
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -o bin/binrag-server-linux-amd64   ./cmd/server
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -o bin/binrag-server-linux-arm64   ./cmd/server
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -o bin/binrag-server-darwin-arm64  ./cmd/server
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o bin/binrag-server-windows-amd64.exe ./cmd/server
```

发布包建议包含：二进制 + `configs/config.yaml`（示例配置）+ 启动脚本，见 `.github/workflows/release.yml` 的打包方式。

### 桌面版（macOS）

桌面版依赖 Wails v3（CGO），需在 macOS 本机构建，产物包含完整后端：

```bash
# 一键构建并组装 .app（内部：前端构建 → go build ./cmd/desktop → 组装 + adhoc 签名）
wails3 task package          # 产物：bin/BinRag.app
wails3 task package:dmg      # 可选：再生成 bin/BinRag.dmg 安装映像
```

> 需要先安装 `wails3` CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.4`

桌面应用启动后自动在 `127.0.0.1` 随机端口启动内嵌后端，窗口直接加载界面；数据库 / 向量库 / 模型仍连接外部服务（配置文件可用 `-c` 指定，默认 `./configs/config.yaml`）。

### 自动发布（GitHub Actions）

推送 `v*` 标签自动触发 `.github/workflows/release.yml`：

- **Web 版交叉编译**：Linux（amd64/arm64）、macOS（amd64/arm64）、Windows（amd64），每包含二进制 + 示例配置 + 启动脚本
- **桌面版**：macOS（.app + .dmg）、Windows（.exe，需 MinGW）
- 前端缓存与 Go 构建缓存加速重复构建

## 🔌 MCP Server

基于 MCP 协议（streamable HTTP）向外部平台 / Agent / AI 应用开放只读 RAG 能力（问答、检索、知识库与任务查询），与 REST API 同进程部署、复用同一套认证与权限体系。

### 配置

```yaml
server:
  mcp:
    enabled: false          # 默认关闭，显式开启后才挂载 /mcp
    path: "/mcp"            # MCP 端点路径
    audit_param_limit: 2000 # 审计参数截断长度（字符）
```

> `enabled` / `path` 影响路由挂载，修改后需重启生效；全局开关（bootstrap API Key 可在「系统配置 → MCP Server」中修改）。

### 认证与授权

- **认证**：`Authorization: Bearer <API Key>`（SHA-256 校验）；缺失 / 无效 / 停用 Key → **HTTP 401**
- **授权**：Tool 白名单 / 知识库越权 / 任务越权 → JSON-RPC error **-32001**（越权与不存在统一消息，不泄露资源存在性）
- **双层开关**：全局 `mcp.enabled`（部署级）+ 用户凭据启用状态（用户级）
- **权限模型**：系统级 API Key（owner 为空）可配置全量权限（bootstrap 管理）；登录用户可在「我的 MCP」页自助生成**绑定自己的凭据**，知识库范围限于自己的知识库

### 提供的 Tool（只读）

| Tool | 说明 |
|------|------|
| `list_knowledge_bases` | 列出当前凭据可访问的知识库 |
| `get_knowledge_base` | 知识库详情（无权限按不存在处理） |
| `retrieve` | 纯检索：召回 chunk 与来源 |
| `ask` | RAG 问答：返回回答与引用来源（不暴露内部推理） |
| `list_documents` | 知识库内文档列表 |
| `get_task` | 入库任务状态（按任务所属知识库校验权限） |

### 客户端接入示例

```json
{
  "mcpServers": {
    "binrag": {
      "url": "http://localhost:8085/mcp",
      "headers": { "Authorization": "Bearer <API Key>" }
    }
  }
}
```

支持 streamable HTTP 的 MCP 客户端（Claude Desktop、Cursor、自研 Agent 等）配置后即可调用；`initialize` → `tools/list` → `tools/call` 标准握手。REST 管理接口：`PUT /api/v1/api-keys/:id/permissions`（系统级，bootstrap）、`/api/v1/mcp/my/*`（用户自助）。

## 📡 API 概览

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
| PUT | `/api/v1/api-keys/:id/permissions` | 更新 API Key 的 MCP 权限（bootstrap） |
| GET | `/api/v1/auth/providers` | 三方登录 Provider 列表（公开） |
| GET | `/api/v1/auth/oidc/:provider/login` | OIDC 授权跳转（公开） |
| GET | `/api/v1/auth/github/login` | GitHub 授权跳转（公开） |
| POST | `/api/v1/auth/exchange` | 登录票据兑换会话 JWT（公开） |
| GET | `/api/v1/auth/me` | 当前登录身份（API Key / 用户） |
| GET | `/api/v1/mcp/my/status` | 「我的 MCP」状态（全局开关 + 我的凭据） |
| POST / DELETE | `/api/v1/mcp/my/key` | 生成（明文仅一次）/ 吊销我的 MCP 凭据 |
| POST | `/api/v1/mcp/my/key/toggle` | 启用 / 停用我的 MCP 凭据 |
| PUT | `/api/v1/mcp/my/key/permissions` | 配置我的 MCP 权限（知识库限自己的） |
| POST | `/mcp` | MCP 端点（streamable HTTP：initialize / tools / call） |
| GET | `/swagger/index.html` | Swagger UI（公开） |
| GET | `/swagger/doc.json` | OpenAPI 3 文档 JSON（公开） |

所有接口统一响应格式：`{"code": 0, "message": "ok", "data": ...}`；全部 `/api/v1/*` 接口（含 API Key 管理）均需 `Authorization: Bearer <api_key>`。Swagger 文档页 `/swagger/` 公开可访问（接口实测仍需 Key）。

## 📊 RAG 评估

`cmd/eval` 提供独立的 RAG 评估 CLI，以数据集驱动量化检索与问答质量，为分块 / Embedding / Prompt 调优提供依据。

```bash
# 仅检索评估（Recall@K，不调 LLM）
go run ./cmd/eval -c configs/config.local.yaml -d dataset.json -m retrieve

# 全量评估（检索 + 问答 + LLM-as-Judge 准确性与忠实度）
go run ./cmd/eval -c configs/config.local.yaml -d dataset.json -m full -o report.json
```

数据集格式（`.json` 或 `.jsonl`）：

```json
{
  "name": "示例评估集",
  "samples": [
    { "question": "产品支持哪些文档格式？", "answer": "TXT/Markdown/PDF 等",
      "expected_ids": ["<chunk_id>"], "kb_id": "<kb_id,可选>" }
  ]
}
```

- `expected_ids`：期望检索命中的 chunk ID（入库时生成的 chunk_id，可从向量库 payload 或文档 `ChunkIDs` 获取），用于 Recall@K 判定
- `answer`：标准答案，`full` 模式下用于 LLM 准确性评分（可选）
- 模式：`retrieve`（仅检索）/ `qa`（检索+问答）/ `full`（全量含 LLM 指标），K 值默认 `1,3,5`（`-k` 覆盖）

## 📁 项目结构

```
cmd/
├── server/              Web 服务入口（-c / --config 指定配置文件，托管前端静态资源）
├── desktop/             桌面应用入口（Wails v3：内嵌后端 + 窗口加载本地服务）
└── eval/                RAG 评估 CLI（数据集 + Recall@K + LLM-as-Judge）
frontend/                Vue 3 + TypeScript 前端（Web 与桌面共用，npm run build 产物进 internal/webui/dist）
internal/
├── app/                 服务装配（PostgreSQL / worker / 引擎 / 路由，Web 与桌面共用）
├── webui/               前端静态资源托管（go:embed dist + SPA 回退）
├── api/                 HTTP 层：路由、中间件（认证/日志/CORS/限流）、Handler、SSE、Swagger 注解
├── auth/                认证：OIDC / GitHub 三方登录、会话 JWT 签发与校验、用户身份
├── mcp/                 MCP Server：streamable HTTP、认证/授权网关层、6 个只读 Tool、异步审计
├── eval/                RAG 评估：数据集加载 / Recall@K / LLM-as-Judge / 报告
├── store/               PostgreSQL 元数据存储：知识库/文档/任务/API Key/对话历史
├── task/                异步入库 worker 池（状态机 + 失败重试 + 重启恢复）
├── loader/              文档加载器（TXT/Markdown/PDF/DOCX/CSV/Excel/HTML）
├── chunker/             分块器（固定大小 / 递归字符 / Markdown 标题）
├── embedding/           Embedding 客户端（OpenAI 兼容）
├── vectorstore/         Qdrant 向量存储
├── retriever/           混合检索（向量 + BM25 + RRF 融合，按知识库过滤）
├── reranker/            Cross-encoder 重排序客户端
├── rag/                 RAG 编排（Query 改写 / 上下文组装 / 生成 / 对话历史）
└── llm/                 LLM 客户端（普通生成 / SSE 流式）
build/                   macOS .app 打包资源（Info.plist）
.github/workflows/       release.yml：tag 触发多平台交叉编译发布
docs/                    各阶段的 spec / plan / task / checklist 设计文档
```

## 📍 当前阶段情况

| 阶段 | 内容 | 状态 |
|------|------|------|
| 一 | 项目初始化与框架选型 | ✅ 完成 |
| 二 | 文档加载器（多格式解析） | ✅ 完成 |
| 三 | 文档分块器（多策略） | ✅ 完成 |
| 四 | Embedding 与向量存储（Qdrant） | ✅ 完成 |
| 五 | 检索器与重排序（混合检索 + RRF） | ✅ 完成 |
| 六 | LLM 集成与 RAG 编排（改写 / 流式 / 历史） | ✅ 完成 |
| 七 | API 层与知识库管理（REST / 异步入库 / 认证） | ✅ 完成 |
| 八 | 前端界面（Web SPA + Wails 桌面，对话/知识库/文档/密钥管理） | ✅ 完成 |
| 九 | RAG 评估与优化（评估 CLI：Recall@K / LLM-as-Judge，为调优提供量化依据） | ✅ 完成 |
| 十 | MCP Server（streamable HTTP 只读 RAG 能力 + 认证/授权/审计 + 用户维度凭据自助管理） | ✅ 完成 |
| 十一 | OIDC / GitHub 三方登录（会话 JWT + 用户体系 + 用户知识库归属） | ✅ 完成 |

每个阶段的完整设计文档（spec → plan → task → checklist）见 `docs/` 目录。

## 📚 文档

- `docs/00-开发路线图.md` — 系统架构总览与分阶段规划
- `docs/0X-*/` — 各阶段四件套设计文档（需求规格 / 技术方案 / 任务拆解 / 验收清单）

## 📄 License

本项目使用 [MIT License](LICENSE) 开源。Copyright (c) 2026 Bin-hy。
