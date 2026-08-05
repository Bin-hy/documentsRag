package api

import (
	"errors"
	"time"

	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// createKBRequest 创建知识库请求
type createKBRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// updateKBRequest 更新知识库请求
type updateKBRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// CreateKB 创建知识库
func (h *handler) CreateKB(c *gin.Context) {
	var req createKBRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	kb := store.KnowledgeBase{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.store.CreateKB(c.Request.Context(), kb); err != nil {
		Fail(c, CodeInternal, "创建知识库失败")
		return
	}
	OK(c, kb)
}

// ListKBs 知识库列表
func (h *handler) ListKBs(c *gin.Context) {
	kbs, err := h.store.ListKBs(c.Request.Context())
	if err != nil {
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}
	OK(c, kbs)
}

// GetKB 知识库详情
func (h *handler) GetKB(c *gin.Context) {
	kb, err := h.store.GetKB(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "知识库不存在")
			return
		}
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}
	OK(c, kb)
}

// UpdateKB 更新知识库
func (h *handler) UpdateKB(c *gin.Context) {
	var req updateKBRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, CodeBadRequest, "请求体无效: "+err.Error())
		return
	}

	kb, err := h.store.GetKB(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "知识库不存在")
			return
		}
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}

	kb.Name = req.Name
	kb.Description = req.Description
	if err := h.store.UpdateKB(c.Request.Context(), *kb); err != nil {
		Fail(c, CodeInternal, "更新知识库失败")
		return
	}
	OK(c, kb)
}

// DeleteKB 删除知识库（先清理其全部文档的向量与索引，再删记录）
func (h *handler) DeleteKB(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")

	if _, err := h.store.GetKB(ctx, kbID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "知识库不存在")
			return
		}
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}

	docs, err := h.store.ListDocuments(ctx, kbID)
	if err != nil {
		Fail(c, CodeInternal, "查询文档失败")
		return
	}
	for _, doc := range docs {
		h.deleteDocument(ctx, doc)
	}

	if err := h.store.DeleteKB(ctx, kbID); err != nil {
		Fail(c, CodeInternal, "删除知识库失败")
		return
	}
	OK(c, gin.H{"id": kbID})
}
