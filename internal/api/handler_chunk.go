package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetChunk 按 chunk_id 查询 chunk 原文内容（引用来源点击查看用）
//
//	@Summary		Chunk 原文
//	@Description	按 chunk_id 从向量库取回该 chunk 的原文内容与来源文档信息，供引用来源点击查看
//	@Tags			问答
//	@Produce		json
//	@Param			id	path		string	true	"chunk ID"
//	@Success		200		{object}	Response{data=object{chunk_id=string,content=string,document_id=string,filename=string}}
//	@Failure		401		{object}	Response
//	@Failure		404		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/chunks/{id} [get]
func (h *handler) GetChunk(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		Fail(c, CodeBadRequest, "缺少 chunk_id")
		return
	}
	// chunk id 为 UUID；非法格式直接 404（避免向量库解析错误）
	if _, err := uuid.Parse(id); err != nil {
		Fail(c, CodeNotFound, "chunk 不存在")
		return
	}

	payload, ok, err := h.vs.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, CodeInternal, "查询 chunk 失败")
		return
	}
	if !ok {
		Fail(c, CodeNotFound, "chunk 不存在")
		return
	}

	content, _ := payload["content"].(string)
	documentID, _ := payload["document_id"].(string)
	filename, _ := payload["filename"].(string)
	if content == "" {
		Fail(c, CodeNotFound, "chunk 无可用内容")
		return
	}

	// 访问权校验：chunk 必须能关联到所属文档且文档可访问，否则一律 404
	// （文档不存在/残留向量/越权均不返回原文，防跨租户读取）
	if documentID == "" {
		Fail(c, CodeNotFound, "chunk 不存在")
		return
	}
	doc, err := h.store.GetDocument(c.Request.Context(), documentID)
	if err != nil {
		Fail(c, CodeNotFound, "chunk 不存在")
		return
	}
	if !h.ensureKBAccess(c, doc.KBID) {
		Fail(c, CodeNotFound, "chunk 不存在")
		return
	}

	OK(c, gin.H{
		"chunk_id":    id,
		"content":     content,
		"document_id": documentID,
		"filename":    filename,
	})
}
