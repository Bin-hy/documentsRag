// Package webui 托管前端构建产物（go:embed dist）：SPA 静态资源与路由回退。
package webui

import (
	"embed"
	"io/fs"
)

// distFS 前端构建产物（frontend 目录执行 npm run build 后输出到 internal/webui/dist）
//
//go:embed all:dist
var distFS embed.FS

// FS 返回 dist 子文件系统，供静态资源路由使用。
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
