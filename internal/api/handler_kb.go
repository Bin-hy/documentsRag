package api

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// createKBRequest 创建知识库请求
type createKBRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Strategy    *config.StrategyConfig `json:"strategy,omitempty"` // 知识库级策略
}

// updateKBRequest 更新知识库请求
type updateKBRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Strategy    *config.StrategyConfig `json:"strategy,omitempty"` // 知识库级策略（nil = 不修改）
}

// CreateKB 创建知识库
//
//	@Summary		创建知识库
//	@Description	创建一个新的知识库，返回知识库完整信息
//	@Tags			知识库
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createKBRequest	true	"知识库信息"
//	@Success		200		{object}	Response{data=store.KnowledgeBase}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases [post]
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
	if req.Strategy != nil {
		b, err := json.Marshal(req.Strategy)
		if err != nil {
			Fail(c, CodeBadRequest, "策略格式无效")
			return
		}
		kb.Strategy = string(b)
	}
	if err := h.store.CreateKB(c.Request.Context(), kb); err != nil {
		Fail(c, CodeInternal, "创建知识库失败")
		return
	}
	OK(c, kb)
}

// ListKBs 知识库列表
//
//	@Summary		知识库列表
//	@Description	列出全部知识库，按创建时间倒序
//	@Tags			知识库
//	@Produce		json
//	@Success		200	{object}	Response{data=[]store.KnowledgeBase}
//	@Failure		401	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases [get]
func (h *handler) ListKBs(c *gin.Context) {
	kbs, err := h.store.ListKBs(c.Request.Context())
	if err != nil {
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}
	OK(c, kbs)
}

// GetKB 知识库详情
//
//	@Summary		知识库详情
//	@Description	按 ID 查询单个知识库
//	@Tags			知识库
//	@Produce		json
//	@Param			id	path		string	true	"知识库 ID"
//	@Success		200	{object}	Response{data=store.KnowledgeBase}
//	@Failure		401	{object}	Response
//	@Failure		404	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases/{id} [get]
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
//
//	@Summary		更新知识库
//	@Description	更新知识库的名称与描述
//	@Tags			知识库
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"知识库 ID"
//	@Param			body	body		updateKBRequest	true	"更新内容"
//	@Success		200		{object}	Response{data=store.KnowledgeBase}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		404		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases/{id} [put]
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
	if req.Strategy != nil {
		b, err := json.Marshal(req.Strategy)
		if err != nil {
			Fail(c, CodeBadRequest, "策略格式无效")
			return
		}
		kb.Strategy = string(b)
	}
	if err := h.store.UpdateKB(c.Request.Context(), *kb); err != nil {
		Fail(c, CodeInternal, "更新知识库失败")
		return
	}
	OK(c, kb)
}

// DeleteKB 删除知识库（先清理其全部文档的向量与索引，再删记录）
//
//	@Summary		删除知识库
//	@Description	删除知识库及其全部文档、向量与索引数据
//	@Tags			知识库
//	@Produce		json
//	@Param			id	path		string	true	"知识库 ID"
//	@Success		200	{object}	Response{data=object{id=string}}
//	@Failure		401	{object}	Response
//	@Failure		404	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases/{id} [delete]
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
