# ============================================================================
# BinRag 标准构建镜像（docker build / docker compose build 的默认入口）
#
# 多阶段构建：
#   阶段 1 frontend : Node 22 + pnpm 构建 Vue 3 前端（产物输出 internal/webui/dist）
#   阶段 2 backend  : Go 1.26 编译单二进制（CGO_ENABLED=0，go:embed 嵌入前端）
#   阶段 3 runtime  : Alpine 精简运行时（仅二进制 + CA 证书 + 时区 + 空目录）
#
# 前端依赖使用 pnpm workspace 管理（lock 文件在仓库根目录 pnpm-lock.yaml，
# frontend 无 package-lock.json），因此必须用 pnpm install + --filter 构建，
# 与 .github/workflows/ci.yml 的构建方式保持一致。
#
# 镜像不包含任何配置/数据（fail-fast）：
#   - 配置文件由外部挂载到 /app/configs/config.yaml（缺失则启动即失败）
#   - 上传数据由外部挂载到 /app/data（file_storage_dir=./data/uploads）
# 一键部署见 docker-compose.yml（自动挂载 deploy/configs/config.docker.yaml）。
#
# 构建：
#   docker build -t binrag-server:latest .
# 或一键（含数据库等全部依赖）：
#   docker compose up -d --build
# ============================================================================

# ---------- 阶段 1：构建前端 ----------
FROM node:22-alpine AS frontend

WORKDIR /app

# 先安装 workspace 依赖（pnpm-lock.yaml 锁定版本；单独一层以利用构建缓存）。
# 安装方式与 .github/workflows/ci.yml 一致：全量 pnpm install --frozen-lockfile，
# 后续仅用 --filter 构建 frontend 包（跳过 official 官网站点）。
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY frontend/package.json frontend/package.json
COPY official/package.json official/package.json
# pnpm 版本与根 package.json 的 packageManager 字段保持一致
RUN npm install -g pnpm@10.33.0 \
    && pnpm install --frozen-lockfile

# 拷贝前端源码并构建（vite.config.ts 已配置 outDir=../internal/webui/dist）
COPY frontend/ ./
RUN pnpm --filter binrag-frontend build

# ---------- 阶段 2：编译 Go 二进制 ----------
FROM golang:1.26-alpine AS backend

WORKDIR /app

# 先下载 Go 依赖（单独一层以利用构建缓存）
COPY go.mod go.sum ./
RUN go mod download

# 拷贝源码（.dockerignore 已排除 dist / node_modules / configs 等）
COPY . .

# 覆盖为前端阶段的最新构建产物（go:embed 编译时打包进二进制）
COPY --from=frontend /app/internal/webui/dist ./internal/webui/dist

# 编译：无 CGO 可交叉编译；TARGETARCH 由 buildx 注入（普通 build 默认 amd64）
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-w -s" -o /out/binrag-server ./cmd/server

# ---------- 阶段 3：运行镜像 ----------
FROM alpine:3.20

# CA 证书（访问外部模型/Embedding API 需要）+ 时区数据；固定 uid 1000 便于挂载卷授权
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 1000 binrag \
    && adduser -S -u 1000 -G binrag binrag

WORKDIR /app

# 仅拷贝二进制。生产配置/数据全部来自外部挂载，镜像不内置任何配置文件：
#   - /app/configs/config.yaml   配置文件（docker compose 挂载 deploy/configs/config.docker.yaml）
#   - /app/data                  上传文件持久化（file_storage_dir=./data/uploads）
# 未挂载配置时服务启动即失败（fail-fast），避免误用默认值运行
COPY --from=backend /out/binrag-server ./binrag-server

RUN mkdir -p /app/configs /app/data/uploads && chown -R binrag:binrag /app

USER binrag

EXPOSE 8085

CMD ["./binrag-server", "-c", "/app/configs/config.yaml"]
