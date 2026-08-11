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

项目提供 `docker-compose.yml` 与 `docker-compose.local.yml`：

```bash
docker compose up -d          # 启动 Qdrant + PostgreSQL 基础设施
```

应用容器化部署需自行构建镜像（二进制 + 配置），基础设施依赖 Qdrant / PostgreSQL。

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
