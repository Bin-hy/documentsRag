package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// videoMIMEs 视频扩展名 → MIME 类型（流式响应 Content-Type）
var videoMIMEs = map[string]string{
	".mp4":  "video/mp4",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".mov":  "video/quicktime",
	".webm": "video/webm",
}

// StreamVideo 视频流式访问（支持 HTTP Range，spec F8）。
// 路径仅来自数据库记录 doc.FilePath（video_id → db lookup），防路径穿越。
//
//	@Summary		视频流式播放
//	@Description	按文档 ID 流式返回视频文件，支持 HTTP Range（浏览器 video 标签 seek）
//	@Tags			文档
//	@Produce		video/mp4
//	@Param			id	path		string	true	"视频文档 ID"
//	@Success		206	{string}	string	"Partial Content（Range 请求）"
//	@Failure		400	{object}	Response
//	@Failure		404	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/videos/{id}/stream [get]
func (h *handler) StreamVideo(c *gin.Context) {
	ctx := c.Request.Context()
	docID := c.Param("id")
	if _, err := uuid.Parse(docID); err != nil {
		Fail(c, CodeNotFound, "视频不存在")
		return
	}

	doc, err := h.store.GetDocument(ctx, docID)
	if err != nil {
		Fail(c, CodeNotFound, "视频不存在")
		return
	}
	if !h.ensureKBAccess(c, doc.KBID) {
		Fail(c, CodeNotFound, "视频不存在")
		return
	}

	format := strings.ToLower(doc.Format)
	mime, isVideo := videoMIMEs[format]
	if !isVideo {
		Fail(c, CodeBadRequest, "该文档不是视频")
		return
	}

	f, err := os.Open(doc.FilePath)
	if err != nil {
		Fail(c, CodeNotFound, "视频文件不存在")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		Fail(c, CodeInternal, "读取视频文件失败")
		return
	}

	c.Header("Content-Type", mime)
	c.Header("Accept-Ranges", "bytes")
	// http.ServeContent 原生支持 Range 请求与 206 Partial Content
	http.ServeContent(c.Writer, c.Request, doc.Filename, stat.ModTime(), f)
}
