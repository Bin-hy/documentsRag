# BinRag Dokploy 部署指南（Dockerfile 模式 · 配置与数据全外部挂载）

> 本文档对应的镜像规范：`Dockerfile.deploy`（**镜像内不包含任何配置文件**，
> 配置与数据全部由外部 Volume 挂载，符合「镜像不含生产配置」的生产要求）。
> 部署形态：单镜像单服务（前端已通过 go:embed 嵌入二进制，不拆分前后端），
> 外部依赖 PostgreSQL + Qdrant + LLM/Embedding API。

---

## 1. 部署架构

```
GitHub push ──► Dokploy（webhook 触发）──► docker build（Dockerfile.deploy）──► 运行容器
                                                                                    │
                 Dokploy 项目网络（同一 Docker Network，服务名互相解析）              │
                 ├── postgres   postgres:16                    ◄── binrag-server 连接
                 ├── qdrant     qdrant/qdrant（gRPC 6334）     ◄── binrag-server 连接
                 └── binrag-server（本镜像，端口 8085）
                        ├── Volume  /opt/dokploy/binrag/config.yaml → /app/configs/config.yaml
                        ├── Volume  /opt/dokploy/binrag/data       → /app/data
                        └── 外部 LLM / Embedding API（配置文件中指定）
```

---

## 2. Dokploy 创建 Application 步骤

1. Dokploy 控制台 → **Services** → **New Service** → 选 **Dockerfile** 部署模式。
2. **Repository**：填写 BinRag 的 Git 仓库地址与分支（`main`）。
3. **Build** 配置（见下节）。
4. **Volumes** 挂载配置与数据（见 §5）。
5. **Network** 加入项目网络，与 PostgreSQL / Qdrant 同网（见 §7）。
6. **Ports**：映射 `8085`（见 §4）。
7. **Advanced / Environment**：按需设置环境变量（见 §6）。
8. **Save & Deploy**。

---

## 3. Dockerfile 配置

| 配置项 | 值 | 说明 |
|---|---|---|
| Build Type | `Dockerfile` | Dokploy 按 Dockerfile 构建 |
| Dockerfile | `Dockerfile.deploy` | 仓库根目录下的多阶段构建文件 |
| Build Context | `.` | 构建上下文 = 仓库根目录（`Dockerfile.deploy` 内 COPY 路径均相对它） |
| Target Stage | 留空 | 默认构建最后一个阶段（runtime，即最终运行镜像） |

`Dockerfile.deploy` 三阶段：

```
frontend（node:22-alpine）  → npm ci + npm run build，产物 internal/webui/dist
backend （golang:1.26-alpine）→ CGO_ENABLED=0 编译单二进制，go:embed 嵌入前端
runtime（alpine:3.20）      → 仅二进制 + CA 证书 + 时区 + 空目录（无任何配置）
```

---

## 4. 端口

- 镜像内 `EXPOSE 8085`，服务监听 `server.port: 8085`（配置文件控制）。
- Dokploy **Ports**：容器端口 `8085` → 对外映射（或交由 Dokploy 域名/Traefik 代理）。
- 健康检查（可选）：HTTP 探针 `/swagger/index.html`（公开、恒定 200）。

---

## 5. Volume 配置（必需）

| 宿主机路径（示例） | 容器内路径 | 说明 |
|---|---|---|
| `/opt/dokploy/binrag/config.yaml` | `/app/configs/config.yaml` | 生产配置（**只读**，`:ro`），缺失则容器启动即失败（fail-fast） |
| `/opt/dokploy/binrag/data` | `/app/data` | 上传文件持久化（`file_storage_dir: ./data/uploads` → `/app/data/uploads`） |

> 权限：容器内以 uid **1000**（用户 `binrag`）运行。若挂载后报权限错误：
> `sudo chown -R 1000:1000 /opt/dokploy/binrag`。
>
> 两种挂载方式任选其一即可：
> - **File Mount**（Dokploy 内直接编辑/上传单文件）→ 适合配置。
> - **Volume（宿主机目录绑定）** → 适合 `data` 目录；配置也可以绑定宿主机文件。

---

## 6. Environment 配置（可选用，配合方案 B）

镜像不读取 Dokploy Environment 作为配置来源；Environment 仅在**启用了
配置环境变量替换（方案 B，见 §8.2）**时生效，用于注入敏感值：

```env
POSTGRES_DSN=postgres://binrag:xxxx@postgres:5432/binrag
EMBEDDING_API_KEY=sk-xxx
LLM_API_KEY=sk-xxx
RERANK_API_KEY=sk-xxx
BOOTSTRAP_API_KEY=sk-xxx
```

> 未启用方案 B 时，这些值直接写在挂载的 `config.yaml` 中即可（方案 A），
> Environment 留空。

---

## 7. PostgreSQL 与 Qdrant 加入同一 Docker Network

Dokploy 中**同一 Project 下的服务默认加入同一个自定义 Docker 网络**，
服务之间用**服务名/容器名**互相解析：

1. 在 Dokploy 新建 `postgres` 服务（镜像 `postgres:16`）：
   - 环境变量 `POSTGRES_USER=binrag` / `POSTGRES_PASSWORD=...` / `POSTGRES_DB=binrag`
   - 挂载卷到 `/var/lib/postgresql/data`
   - **不要**暴露公网端口（仅内网被 binrag-server 访问）
2. 在 Dokploy 新建 `qdrant` 服务（镜像 `qdrant/qdrant:latest`）：
   - 挂载卷到 `/qdrant/storage`
   - gRPC 端口默认 6334，无需额外配置
3. 确认三个服务在**同一个 Project**（Dokploy 会自动放入同一网络）；
   若网络不同，在服务的 Network 设置中把 binrag-server 加入 postgres/qdrant 所在网络。

配置文件中据此填写（服务名即容器名）：

```yaml
postgres:
  dsn: "postgres://binrag:<PASSWORD>@postgres:5432/binrag"
vectorstore:
  host: "qdrant:6334"
```

> 验证：部署后进入 binrag-server 容器执行
> `getent hosts postgres` / `getent hosts qdrant`，能解析即网络连通。

---

## 8. 配置管理：方案 A vs 方案 B

### 结论（推荐）

> **方案 A（YAML 完全通过 Volume 管理）是 BinRag 的默认推荐方案。**
> 方案 B（`${ENV_VAR}` 替换）作为可选增强，仅在「希望 API Key / 数据库密码
> 等敏感值走 Dokploy Environment、不落盘到宿主机文件」时启用。

### 8.1 方案 A：YAML 完全通过 Volume 管理（默认，零代码改动）

- 在宿主机维护一份**完整** `config.yaml`（可基于 `configs/config.yaml` 修改），
  挂载到 `/app/configs/config.yaml`。
- 优点：
  1. **零 Go 代码改动**，行为与本地完全一致；
  2. BinRag 配置是**深层嵌套结构**（`rag.strategy.query`、`reranker.mode` 等），
     完整 YAML 一目了然、可整体 diff/审计/回滚；
  3. 一个文件集中管理，不依赖「env 名 ↔ yaml 字段」的映射约定（不易出错）。
- 缺点：敏感值以明文存在宿主机文件中（与绝大多数自部署服务相同；可通过文件权限
  600 + 宿主机加密盘缓解）。

### 8.2 方案 B：YAML 支持 `${ENV_VAR}` 替换（可选增强，需最小代码改动）

在 YAML 中写占位符，启动时由环境变量替换：

```yaml
postgres:
  dsn: "postgres://binrag:${POSTGRES_PASSWORD}@postgres:5432/binrag"
embedder:
  api_key: "${EMBEDDING_API_KEY}"
```

最小代码改动（`internal/config/config.go` 的 `LoadConfig`，约 6 行）：

```go
// 在 yaml.Unmarshal 之前插入：
// 环境变量替换：支持 ${VAR} 与 ${VAR:-默认值} 两种写法；
// 变量未定义且无默认值时替换为空字符串
data = []byte(os.Expand(data, func(k string) string {
    name, def, hasDef := strings.Cut(k, ":-")
    if v, ok := os.LookupEnv(name); ok {
        return v
    }
    if hasDef {
        return def
    }
    return ""
}))
```

注意：
- 替换发生在 YAML 解析**之前**，替换值若为数字/布尔会被 yaml 正确解析为对应类型；
- 若环境变量值含 `:`、`#` 等 YAML 特殊字符，**占位符两侧需加引号**
  （如 `dsn: "${PG_DSN}"`）；
- `os.Expand` 实际也会展开**不带花括号**的 `$VAR` 写法（非预期行为，但无副作用）；
- 展开只作用于主配置文件，`config.local.yaml` 覆盖文件中的 `${VAR}` **不会**被展开
  （生产环境不使用 local 覆盖，影响很小）；
- 方案 B 与方案 A 可混用：不写 `${}` 的字段仍走文件值，只有写了占位符的字段才查环境变量；
- 缺失的必填项（如 `postgres.dsn`）在启动连接数据库时会以启动失败暴露；
  模型 `api_key` 缺失则要等实际调用模型 API 时才报错——建议把 key 也写进配置或 env。

### 8.3 配置示例（方案 A 完整文件）

挂载到 `/app/configs/config.yaml`：

```yaml
embedder:
  base_url: "https://<embedding服务>/v1"
  api_key: "sk-xxx"
  model: "text-embedding-v4"
  dimension: 1024

vectorstore:
  host: "qdrant:6334"
  collection_name: "binrag"
  dimension: 1024
  distance: "cosine"

llm:
  base_url: "https://<llm服务>/v1"
  api_key: "sk-xxx"
  model: "gpt-4o"

reranker:
  base_url: "https://<rerank服务>/v1"
  api_key: "sk-xxx"
  model: "bge-reranker-v2-m3"

postgres:
  dsn: "postgres://binrag:<PASSWORD>@postgres:5432/binrag"

server:
  port: 8085
  file_storage_dir: "./data/uploads"
  upload_max_size_mb: 1024
  worker_count: 5
  auth_enabled: true
  bootstrap_api_key: "sk-首次启动种子"   # 建完第一个 API Key 后删除并重启
  rate_limit_qps: 0

oidc:                       # 可选，启用三方登录时填写
  enabled: false
  public_url: "https://rag.yourdomain.com"
```

> 字段省略时使用代码内置默认值（`config.go` applyDefaults），因此生产文件
> 只需写与默认值不同的项 + 必填项（模型 key、DSN 等）。

---

## 9. 数据持久化说明

| 数据 | 位置 | 持久化方式 |
|---|---|---|
| 上传的源文件 | `/app/data/uploads`（容器内） | Volume 挂载 `/opt/dokploy/binrag/data` |
| 文档元数据 / 任务 / API Key / 对话历史 | PostgreSQL | postgres 服务的卷（`/var/lib/postgresql/data`） |
| 向量数据 | Qdrant | qdrant 服务的卷（`/qdrant/storage`） |
| 模型配置 / 密钥 | `/app/configs/config.yaml` | Volume 挂载（宿主机维护） |

- 容器重建/镜像更新**不丢失**任何数据（全部在卷上）。
- 备份策略：定期备份 PostgreSQL（`pg_dump`）与 Qdrant 目录、`/opt/dokploy/binrag`。

---

## 10. 更新流程

```
git push（代码变更）
   │
   ▼
Dokploy Webhook（Repository 配置中设置，监听 push）
   │
   ▼
Dokploy 触发 rebuild：docker build -f Dockerfile.deploy（多阶段缓存加速）
   │
   ▼
滚动更新：旧容器替换为新容器（配置/数据卷不受影响）
   │
   ▼
健康检查通过后完成（路径 /swagger/index.html）
```

- 依赖层（`npm ci` / `go mod download`）未变时只重编二进制与前端，构建很快。
- 只改配置：**无需重新构建**，直接改宿主机 `config.yaml` 后重启容器即可。
- 回滚：Dokploy 可选择历史镜像/构建记录重新部署；数据在卷上不受影响。

---

## 附：镜像安全检查清单

- [ ] 镜像内无 `config.yaml`（`docker run --rm <img> ls /app/configs` 应为空）
- [ ] 镜像内无 `data/`、无 `.env`、无 `*.local.*`
- [ ] 以非 root 运行（`USER binrag`，uid 1000）
- [ ] 生产配置只存在于宿主机挂载点，权限建议 `chmod 600`
