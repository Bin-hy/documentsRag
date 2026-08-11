---
title: 部署
description: BinRag 部署指南 — Web 版交叉编译、桌面应用、Docker 容器化与 GitHub Actions 自动发布。
---

# 部署

## Web 版（多平台交叉编译）

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

发布包建议包含：二进制 + `configs/config.yaml`（示例配置）+ 启动脚本。

## 桌面版（macOS）

桌面版依赖 Wails v3（CGO），需在 macOS 本机构建，产物包含完整后端：

```bash
# 一键构建并组装 .app（内部：前端构建 → go build ./cmd/desktop → 组装 + adhoc 签名）
wails3 task package          # 产物：bin/BinRag.app
wails3 task package:dmg      # 可选：再生成 bin/BinRag.dmg 安装映像
```

> 需要先安装 `wails3` CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.4`

桌面应用启动后自动在 `127.0.0.1` 随机端口启动内嵌后端，窗口直接加载界面；数据库 / 向量库 / 模型仍连接外部服务。

## Docker 容器化

仓库提供三个 Compose 文件：

- `docker-compose.yml` — 一键部署：现场构建镜像并启动 PostgreSQL + Qdrant + binrag-server
- `docker-compose.prod.yml` — 免编译部署：直接拉取 ghcr.io 已发布镜像
- `docker-compose.dev.yml` — 开发用：仅启动 Qdrant + PostgreSQL 基础设施依赖

```bash
docker compose up -d --build                          # 一键部署（先编辑 deploy/configs/config.docker.yaml）
docker compose -f docker-compose.prod.yml up -d       # 拉取 ghcr.io/bin-hy/documentsrag 镜像部署
docker compose -f docker-compose.dev.yml up -d        # 仅启动基础设施（本地开发用）
```

镜像由 GitHub Actions（`.github/workflows/docker-publish.yml`）在 main 分支与 `v*` 标签自动构建并推送到 GitHub Container Registry（`ghcr.io/bin-hy/documentsrag`，多架构 linux/amd64 + arm64）。完整教程见 [快速开始](/guide/getting-started)。

## 自动发布（GitHub Actions）

推送 `v*` 标签自动触发 `.github/workflows/release.yml`：

- **Web 版交叉编译**：Linux（amd64/arm64）、macOS（amd64/arm64）、Windows（amd64），每包含二进制 + 示例配置 + 启动脚本
- **桌面版**：macOS（.app + .dmg）、Windows（.exe，需 MinGW）
- 前端缓存与 Go 构建缓存加速重复构建

## 反向代理

OIDC 登录需外部可访问的 `public_url`（域名 / 反向代理）。典型 Nginx 配置将 `/` 与 `/api/v1/auth/*` 代理到 `server.port`，并转发 SSE（关闭缓冲）：

```nginx
location /api/v1/chat {
    proxy_pass http://127.0.0.1:8085;
    proxy_buffering off;               # SSE 流式必须
    proxy_http_version 1.1;
}
```
