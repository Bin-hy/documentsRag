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

## 🏗 架构

```mermaid
graph TB
    subgraph API层
        A[HTTP API] --> AUTH[API Key 认证]
        AUTH --> KB[知识库管理]
        AUTH --> DOC[文档上传]
        AUTH --> CHAT[问答 / SSE 流式]
    end

    subgraph 入库链路
        DOC --> Q[任务队列]
        Q --> P[Pipeline: Load → Chunk → Embed → Store]
        P --> V[(Qdrant 向量库)]
        P --> B[BM25 内存索引]
        P --> PG[(PostgreSQL 元数据)]
    end

    subgraph 问答链路
        CHAT --> R[RAG Engine]
        R --> QW[Query 改写]
        QW --> RET[混合检索: 向量 + BM25 + RRF]
        RET --> RR[Cross-encoder 重排序]
        R --> LLM[LLM 生成]
        V --> RET
        B --> RET
        PG --> R
    end
```

## 🧰 技术栈

| 层次 | 技术 |
|------|------|
| 语言 | Go 1.26+ |
| Web 框架 | Gin |
| 向量数据库 | Qdrant（gRPC） |
| 元数据存储 | PostgreSQL（pgx / 连接池） |
| Embedding / LLM | OpenAI 兼容接口（GPT / 豆包 / DeepSeek / 本地 vLLM 等，base_url 切换） |
| 检索 | 向量 + BM25 + RRF 融合 + Cross-encoder 重排序 |
| 认证 | API Key（SHA-256 哈希存储） |
| 容器化 | Docker Compose（Qdrant + PostgreSQL） |

## 🚀 快速开始

### 前置条件

- Go 1.26+
- Docker（提供 Qdrant 与 PostgreSQL）

### 1. 启动基础设施

```bash
docker compose up -d
```

启动 Qdrant（向量库）与 PostgreSQL（元数据存储）。

### 2. 准备配置

```bash
cp configs/config.yaml configs/config.local.yaml
```

编辑 `configs/config.local.yaml`，填入你的模型服务信息（OpenAI 兼容地址均可）：

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

### 3. 启动服务

```bash
go run ./cmd/server -c configs/config.local.yaml
```

或指定其他配置文件 / 环境变量 / 默认路径（`-c` 优先级最高）：

```bash
go run ./cmd/server                       # 默认 ./configs/config.yaml
BINRAG_CONFIG=configs/prod.yaml go run ./cmd/server
```

启动时可用 `bootstrap_api_key` 获取首个访问密钥（使用后建议从配置中移除）。

### 4. 第一个请求

```bash
# 创建知识库
curl -X POST http://localhost:8080/api/v1/knowledge-bases \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"产品文档库","description":"产品手册与 FAQ"}'

# 上传文档（异步入库，返回 task_id）
curl -X POST "http://localhost:8080/api/v1/documents/upload?kb_id=<kb_id>" \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -F "file=@docs/intro.md"

# 查询任务状态
curl http://localhost:8080/api/v1/tasks/<task_id> \
  -H "Authorization: Bearer sk-your-bootstrap-key"

# 普通问答
curl -X POST http://localhost:8080/api/v1/chat \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"demo","question":"产品支持哪些文档格式？","kb_id":"<kb_id>"}'

# 流式问答（SSE）
curl -N -X POST "http://localhost:8080/api/v1/chat?stream=1" \
  -H "Authorization: Bearer sk-your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"demo","question":"如何部署？","kb_id":"<kb_id>"}'
```

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

所有接口统一响应格式：`{"code": 0, "message": "ok", "data": ...}`，除 `/api/v1/api-keys` 外均需 `Authorization: Bearer <api_key>`。

## 📁 项目结构

```
cmd/server/            服务入口（-c / --config 指定配置文件）
internal/
├── api/               HTTP 层：路由、中间件（认证/日志/CORS/限流）、Handler、SSE
├── store/             PostgreSQL 元数据存储：知识库/文档/任务/API Key/对话历史
├── task/              异步入库 worker 池（状态机 + 失败重试 + 重启恢复）
├── loader/            文档加载器（TXT/Markdown/PDF/DOCX/CSV/Excel/HTML）
├── chunker/           分块器（固定大小 / 递归字符 / Markdown 标题）
├── embedding/         Embedding 客户端（OpenAI 兼容）
├── vectorstore/       Qdrant 向量存储
├── retriever/         混合检索（向量 + BM25 + RRF 融合，按知识库过滤）
├── reranker/          Cross-encoder 重排序客户端
├── rag/               RAG 编排（Query 改写 / 上下文组装 / 生成 / 对话历史）
└── llm/               LLM 客户端（普通生成 / SSE 流式）
docs/                  各阶段的 spec / plan / task / checklist 设计文档
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
| 八 | 评估与优化（数据集 / Recall@K / LLM-as-Judge） | ⏳ 规划中 |

每个阶段的完整设计文档（spec → plan → task → checklist）见 `docs/` 目录。

## 📚 文档

- `docs/00-开发路线图.md` — 系统架构总览与分阶段规划
- `docs/0X-*/` — 各阶段四件套设计文档（需求规格 / 技术方案 / 任务拆解 / 验收清单）

## 📄 License

本项目目前未指定开源协议，商用与二次分发请先与作者联系。
