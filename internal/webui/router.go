package webui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Register 挂载前端静态资源路由到已有引擎：
//   - GET /                → index.html
//   - GET /assets/*        → 静态资源（Vite 构建产物带 hash，可长缓存）
//   - GET 其他路径（SPA 深链）→ 回退 index.html
//   - /api/* 未匹配路径     → JSON 404（保持 API 语义）
func Register(r *gin.Engine) error {
	dist, err := FS()
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.FS(dist))

	r.GET("/", serveIndex(dist))
	r.GET("/assets/*filepath", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=86400")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.Method != http.MethodGet {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "接口不存在"})
			return
		}
		serveIndex(dist)(c)
	})
	return nil
}

// serveIndex 返回 index.html 内容（embed 文件系统不可用 c.File，需读内容后写回）
func serveIndex(dist fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		content, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html 不存在")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	}
}
