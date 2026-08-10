# BinRag 服务器部署指南（dokploy · Dockerfile 模式 · 单服务不分离）

> 目标：把 BinRag 以**单一服务**部署到自有服务器（dokploy 的 Dockerfile 模式），
> 前端与后端打包进同一个镜像（`go:embed` 单二进制），对外域名接入 Cloudflare
> 获得 CDN / HTTPS / 防护。
>
> 为什么不做前后端分离：项目原生设计为单二进制托管前端，同源无跨域、
> SSE 流式与登录回调链路最简单、零代码改动。Cloudflare 的能力（CDN 缓存静态
> 资源、自动 HTTPS、DDoS 防护）通过 DNS 代理 / Tunnel 即可获得，无需拆服务。

---

## 1. 架构总览

```
用户浏览器
   │  https://rag.yourdomain.com
   ▼
Cloudflare（DNS 橙云代理 或 Tunnel：CDN 缓存 + HTTPS + 防护）
   │  回源到服务器
   ▼
dokploy 项目网络（同一 compose 网络，服务名互相解析）
   ├── binrag-server   ← 本镜像（Dockerfile.deploy）：API + 前端静态资源 + SSE
   ├── postgres        ← 元数据存储（postgres:16）
   └── qdrant          ← 向量库（qdrant/qdrant，gRPC 6334）
```

- 前端静态资源由 Go 服务同源托管；Cloudflare 可对 `/assets/*`（带 hash）加缓存规则。
- 依赖关系：BinRag 服务 → PostgreSQL + Qdrant（二者先于 BinRag 启动）。

---

## 2. 前置条件

- 一台有公网 IP 的服务器，已安装 dokploy（建议 2C4G 以上）。
- 一个域名（如 `rag.yourdomain.com`），DNS 托管在 Cloudflare。
- 准备好模型服务信息（Embedding / LLM / 可选 Reranker 的 base_url、api_key、model）。

---

## 3. 在 dokploy 中创建服务

### 3.1 PostgreSQL（元数据存储）

- 新建服务，镜像 `postgres:16`，容器名建议 `binrag-postgres`。
- 环境变量：
  - `POSTGRES_USER=binrag`
  - `POSTGRES_PASSWORD=<强密码>`
  - `POSTGRES_DB=binrag`
- 挂载持久化卷到 `/var/lib/postgresql/data`。
- 不需要对外暴露端口（项目网络内访问即可；如需本机 psql 调试可映射 5432）。

### 3.2 Qdrant（向量库）

- 新建服务，镜像 `qdrant/qdrant:latest`（如需可固定版本号，如 `qdrant/qdrant:v1.12`）。
- 挂载持久化卷到 `/qdrant/storage`。
- 不需要对外暴露端口（gRPC 端口默认 6334，无需额外配置）。

### 3.3 BinRag 主服务

- 新建服务，**部署方式选择 Dockerfile**，填写 Git 仓库地址与分支。
- Dockerfile 文件名填 `Dockerfile.deploy`（仓库根目录）。
- 构建方式：dokploy 会在服务器上执行多阶段构建（Node 构建前端 → Go 编译 → 精简镜像）。

#### 必须的挂载

| 类型 | 挂载目标 | 说明 |
|---|---|---|
| File Mount | `/app/configs/config.local.yaml` | 生产配置覆盖（见 §4），自动与镜像内默认配置合并 |
| Volume | `/app/data` | 上传文件持久化（`file_storage_dir=./data/uploads`，容器重启不丢） |

> 挂载卷属主：镜像内服务以 uid 1000（用户 `binrag`）运行，dokploy 新建的卷
> 首次挂载若报权限错误，在服务器执行 `chown -R 1000:1000 <卷挂载点>` 即可。

#### 端口 / 域名 / 健康检查

- 服务端口 `8085`（镜像内 `EXPOSE 8085`，配置 `server.port=8085`）。
- 对外域名填 `rag.yourdomain.com`，dokploy 自动申请/配置 HTTPS（Traefik）。
- **Cloudflare Tunnel 方式（§5 方式 B）需要额外把容器 8085 映射到宿主机端口**，
  否则服务器上的 cloudflared 无法回源。
- 健康检查：HTTP 探针路径可用 `/swagger/index.html`（公开访问、恒定 200）；
  若 dokploy 无此选项可省略，服务自身有优雅关停。

---

## 4. 生产配置注入（File Mount）

参考模板：`deploy/configs/config.local.yaml.example`（仓库内）。

```bash
cp deploy/configs/config.local.yaml.example /tmp/config.local.yaml
# 编辑：替换 <postgres服务名> / <qdrant服务名> / 模型 key / bootstrap_key 等
```

要点：

- 服务名解析：dokploy 同一项目内的服务通过**容器名**互相解析。
  `postgres.dsn` 的 host 填 PostgreSQL 服务的容器名（如 `binrag-postgres`）；
  `vectorstore.host` 填 Qdrant 服务容器名 + `:6334`。
  若解析失败，可在 dokploy 服务的 Network 设置中确认自定义网络/别名。
- **静默降级提醒**：配置文件读取/解析失败时服务只会打印警告并继续用镜像内默认配置，
  不会报错退出。因此 File Mount 挂载后务必看启动日志确认出现
  `已合并本地配置覆盖` 一行；若没有，说明挂载路径或属主不对（见 FAQ）。
- `oidc.providers` 是**列表**：local 覆盖按字段级合并，启用三方登录时需把
  `providers` 整段完整填写（含所有 provider），不能只补单个字段。
- 敏感信息（模型 key、bootstrap key、数据库密码）**只放这里**，不写进镜像。
- `bootstrap_api_key` 仅用于首次建 Key，创建完第一个 API Key 后删除该行并重启。

---

## 5. Cloudflare 接入（两种方式任选）

### 方式 A：DNS 橙云代理（推荐，最简单）

1. Cloudflare 控制台 → 域名 → DNS → 添加记录：
   - 类型 `A`（或 CNAME），名称 `rag`，内容 = 服务器公网 IP，**代理状态开启（橙云）**。
2. dokploy 侧：服务域名仍指向 `rag.yourdomain.com`（HTTPS 回源由 dokploy/Traefik 处理）。
3. 静态资源加速（可选）：Cloudflare → 缓存 → 缓存规则：
   - 匹配 `rag.yourdomain.com/assets/*`，TTL 设为 1 天以上（Vite 产物带 hash，可长缓存）。

### 方式 B：Cloudflare Tunnel（无公网 IP / 不暴露服务器 80/443）

1. 在 dokploy 服务 **Ports 中把容器 8085 映射到宿主机端口**（如 `18085:8085`），
   否则服务器上的 cloudflared 无法回源。
2. 服务器安装 cloudflared（或 dokploy 的 Tunnel 插件）。
3. 创建 Tunnel，添加公网路由：
   - 服务：`http://127.0.0.1:18085`（回源宿主机映射出的端口）。
4. dokploy 服务本身不再需要对外域名/Traefik 端口，由 Tunnel 统一入口。

> 两种方式下 OIDC 登录回调统一填对外域名 `https://rag.yourdomain.com`，
> 回调路径为 `<public_url>/api/v1/auth/github/callback`（或 oidc provider）。

---

## 6. 首次启动流程

1. 依次启动（或依赖编排）：PostgreSQL → Qdrant → BinRag。
2. 查看 BinRag 服务日志，确认 `HTTP 服务已启动`、无 PostgreSQL/Qdrant 连接错误。
3. 用 bootstrap Key 调接口创建第一个 API Key：

   ```bash
   curl -X POST https://rag.yourdomain.com/api/v1/api-keys \
     -H "Authorization: Bearer sk-你的bootstrap密钥" \
     -H "Content-Type: application/json" \
     -d '{"name":"admin"}'
   ```

4. 打开 `https://rag.yourdomain.com`，用刚创建的 Key 登录使用。
5. **删除 `config.local.yaml` 中的 `bootstrap_api_key` 行，重启服务**（日志中也有提醒）。

---

## 7. 更新与回滚

- 代码推送后，在 dokploy 服务点「Redeploy」重新构建即可（多阶段缓存会加速：依赖层未变时只重编二进制/前端）。
- 回滚：dokploy 支持选择之前的构建记录/镜像重新部署；数据库与向量库数据在持久化卷中不受影响。

---

## 8. 常见问题（FAQ）

| 现象 | 原因 / 解决 |
|---|---|
| 启动报 `连接 PostgreSQL 失败` | DSN 中 host 写错或 PG 未就绪；确认服务容器名与密码 |
| 启动报 `初始化向量存储失败` | Qdrant 未启动或 `vectorstore.host` 端口错（gRPC 是 6334 不是 6333） |
| 上传文件后重启丢失 | 未挂载 `/app/data` 持久化卷 |
| 登录（GitHub/OIDC）回调 404 | 回调地址未按 `https://rag.yourdomain.com/api/v1/auth/...` 在 Provider 后台登记 |
| 配置「没生效」但服务正常启动 | 配置文件读取失败只会警告不会退出。看启动日志是否有 `已合并本地配置覆盖`；没有则检查 File Mount 路径（应为 `/app/configs/config.local.yaml`）与文件属主 |
| 前端打开白屏 / 静态资源 404 | 构建时前端产物未生成（看构建日志 npm 阶段）；确认未手动改 `internal/webui/dist` |
| 页面 API 请求 502 | Cloudflare 回源失败：确认 dokploy 域名 HTTPS 正常、Tunnel 运行中 |
| SSE 问答中途断开 | 服务器无代理缓冲超时；如使用自定义反代，需关闭对 `/api/v1/chat` 的响应缓冲 |
| 卷权限报错 | `chown -R 1000:1000 <卷挂载点>`（镜像内运行用户 uid=1000） |
