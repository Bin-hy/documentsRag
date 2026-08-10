package api

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Bin-hy/bin-rag/internal/auth"
	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// kbView 知识库对外视图（不暴露 owner_id——归属字段仅在服务端授权判断使用）。
// 字段名与原 store.KnowledgeBase 直接序列化一致（Go 字段名），保证 API 兼容不回退（N1）。
type kbView struct {
	ID          string
	Name        string
	Description string
	Strategy    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// toKBView 转换（剔除 OwnerID）
func toKBView(kb store.KnowledgeBase) kbView {
	return kbView{
		ID:          kb.ID,
		Name:        kb.Name,
		Description: kb.Description,
		Strategy:    kb.Strategy,
		CreatedAt:   kb.CreatedAt,
		UpdatedAt:   kb.UpdatedAt,
	}
}

// toKBViews 批量转换
func toKBViews(kbs []store.KnowledgeBase) []kbView {
	views := make([]kbView, 0, len(kbs))
	for _, kb := range kbs {
		views = append(views, toKBView(kb))
	}
	return views
}

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

// canAccessKB 当前身份能否访问该知识库：
// 系统级 API Key → 全部（含系统级与用户级）；登录用户 → 仅 owner_id == UserID（系统级 NULL 不可见）。
// 不匹配一律返回 false（调用方转 404，不泄露存在性）。
func (h *handler) canAccessKB(c *gin.Context, kb *store.KnowledgeBase) bool {
	id := auth.IdentityOf(c)
	if id.Kind == auth.KindAPIKey {
		return true
	}
	return kb.OwnerID != nil && *kb.OwnerID == id.UserID
}

// ensureKBAccess 按 kb_id 取知识库并校验当前身份访问权；不存在/越权均返回 false
func (h *handler) ensureKBAccess(c *gin.Context, kbID string) bool {
	if kbID == "" {
		return false
	}
	kb, err := h.store.GetKB(c.Request.Context(), kbID)
	if err != nil {
		return false
	}
	return h.canAccessKB(c, kb)
}

// CreateKB 创建知识库（owner 由当前身份决定：登录用户 → 归属本人；系统级 Key → 系统级 NULL）
//
//	@Summary		创建知识库
//	@Description	创建一个新的知识库，返回知识库完整信息；登录用户创建的知识库仅本人与系统级 API Key 可见
//	@Tags			知识库
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createKBRequest	true	"知识库信息"
//	@Success		200		{object}	Response{data=kbView}
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
	// owner 由 API 层按当前身份显式决定：OIDC 用户 → user.ID；系统级 API Key → NULL
	if id := auth.IdentityOf(c); id.Kind == auth.KindUser {
		kb.OwnerID = &id.UserID
	}
	if err := h.store.CreateKB(c.Request.Context(), kb); err != nil {
		Fail(c, CodeInternal, "创建知识库失败")
		return
	}
	OK(c, toKBView(kb))
}

// ListKBs 知识库列表（按身份过滤：登录用户仅本人；系统级 API Key 全量）
//
//	@Summary		知识库列表
//	@Description	按当前身份返回知识库：登录用户仅返回自己创建的；系统级 API Key 返回全部。按创建时间倒序
//	@Tags			知识库
//	@Produce		json
//	@Success		200	{object}	Response{data=[]kbView}
//	@Failure		401	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/knowledge-bases [get]
func (h *handler) ListKBs(c *gin.Context) {
	ctx := c.Request.Context()
	id := auth.IdentityOf(c)
	var kbs []store.KnowledgeBase
	var err error
	if id.Kind == auth.KindUser {
		kbs, err = h.store.ListKBsByOwner(ctx, id.UserID)
	} else {
		kbs, err = h.store.ListAllKBs(ctx)
	}
	if err != nil {
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}
	OK(c, toKBViews(kbs))
}

// GetKB 知识库详情（越权访问一律 404）
//
//	@Summary		知识库详情
//	@Description	按 ID 查询单个知识库；无权访问返回 404
//	@Tags			知识库
//	@Produce		json
//	@Param			id	path		string	true	"知识库 ID"
//	@Success		200	{object}	Response{data=kbView}
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
	if !h.canAccessKB(c, kb) {
		Fail(c, CodeNotFound, "知识库不存在")
		return
	}
	OK(c, toKBView(*kb))
}

// UpdateKB 更新知识库（越权访问一律 404）
//
//	@Summary		更新知识库
//	@Description	更新知识库的名称与描述；无权访问返回 404
//	@Tags			知识库
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"知识库 ID"
//	@Param			body	body		updateKBRequest	true	"更新内容"
//	@Success		200		{object}	Response{data=kbView}
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
	if !h.canAccessKB(c, kb) {
		Fail(c, CodeNotFound, "知识库不存在")
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
	OK(c, toKBView(*kb))
}

// DeleteKB 删除知识库（越权访问一律 404；先清理其全部文档的向量与索引，再删记录）
//
//	@Summary		删除知识库
//	@Description	删除知识库及其全部文档、向量与索引数据；无权访问返回 404
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

	kb, err := h.store.GetKB(ctx, kbID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "知识库不存在")
			return
		}
		Fail(c, CodeInternal, "查询知识库失败")
		return
	}
	if !h.canAccessKB(c, kb) {
		Fail(c, CodeNotFound, "知识库不存在")
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
