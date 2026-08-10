package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UploadDocument 上传文档（multipart：kb_id + file），返回 task_id，异步入库
//
//	@Summary		上传文档
//	@Description	上传文档到指定知识库，保存文件并创建入库任务，立即返回 task_id；入库异步执行（Load→Chunk→Embed→Store）
//	@Tags			文档
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			kb_id	query		string	true	"知识库 ID"
//	@Param			file	formData	file	true	"文档文件（支持 md/txt/pdf/docx 等）"
//	@Success		200		{object}	Response{data=object{task_id=string,document_id=string}}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		404		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/documents/upload [post]
func (h *handler) UploadDocument(c *gin.Context) {
	ctx := c.Request.Context()

	// 大小限制必须先于任何 body 读取（FormFile 会解析整个 multipart body）
	maxBytes := int64(h.cfg.UploadMaxSizeMB) * 1024 * 1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)

	// kb_id 走 query，不参与 body 解析（超限时能区分错误原因）
	kbID := c.Query("kb_id")
	if kbID == "" {
		Fail(c, CodeBadRequest, "缺少 kb_id")
		return
	}
	// kb_id 必须为服务端生成的 UUID，防止路径穿越
	if _, err := uuid.Parse(kbID); err != nil {
		Fail(c, CodeBadRequest, "无效的 kb_id")
		return
	}
	// 访问权校验：知识库不存在或越权一律 404
	if !h.ensureKBAccess(c, kbID) {
		Fail(c, CodeNotFound, "知识库不存在")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			Fail(c, CodeBadRequest, fmt.Sprintf("文件超过大小限制（%dMB）", h.cfg.UploadMaxSizeMB))
			return
		}
		Fail(c, CodeBadRequest, "缺少 file 字段: "+err.Error())
		return
	}

	// 扩展名校验（能否解析）
	info := loader.FileInfo{Filename: file.Filename, Size: file.Size}
	parser, err := h.registry.Resolve(info)
	if err != nil {
		Fail(c, CodeBadRequest, "不支持的文件格式: "+filepath.Ext(file.Filename))
		return
	}

	// 上传预检：解析一次统计可读文本，扫描件/空内容直接拒绝，避免无效内容入库
	if h.loaderCfg.MinReadableChars > 0 {
		if err := precheckReadable(file, parser, info, h.loaderCfg.MinReadableChars); err != nil {
			Fail(c, CodeBadRequest, err.Error())
			return
		}
	}

	docID := uuid.New().String()
	ext := filepath.Ext(file.Filename)

	// 保存文件
	dir := filepath.Join(h.cfg.FileStorageDir, kbID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		Fail(c, CodeInternal, "创建存储目录失败")
		return
	}
	filePath := filepath.Join(dir, docID+ext)
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		Fail(c, CodeInternal, "保存文件失败")
		return
	}

	// 创建文档与任务记录（taskID 先于文档生成并回填，保证文档可关联到任务）
	taskID := uuid.New().String()
	doc := store.Document{
		ID:        docID,
		KBID:      kbID,
		Filename:  file.Filename,
		Format:    ext,
		Size:      file.Size,
		Status:    store.DocStatusPending,
		FilePath:  filePath,
		TaskID:    taskID,
		CreatedAt: time.Now(),
	}
	if err := h.store.CreateDocument(ctx, doc); err != nil {
		Fail(c, CodeInternal, "创建文档记录失败")
		return
	}

	task := store.Task{
		ID:         taskID,
		KBID:       kbID,
		DocumentID: docID,
		Status:     store.TaskStatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := h.store.CreateTask(ctx, task); err != nil {
		Fail(c, CodeInternal, "创建任务记录失败")
		return
	}

	OK(c, gin.H{"task_id": taskID, "document_id": docID})
}

// ListDocuments 按知识库列出文档
//
//	@Summary		文档列表
//	@Description	按知识库查询文档列表，含文件名、大小、格式、入库状态、创建时间
//	@Tags			文档
//	@Produce		json
//	@Param			kb_id	query		string	true	"知识库 ID"
//	@Success		200		{object}	Response{data=[]store.Document}
//	@Failure		400		{object}	Response
//	@Failure		401		{object}	Response
//	@Failure		500		{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/documents [get]
func (h *handler) ListDocuments(c *gin.Context) {
	kbID := c.Query("kb_id")
	if kbID == "" {
		Fail(c, CodeBadRequest, "缺少 kb_id")
		return
	}
	// 访问权校验：越权一律 404
	if !h.ensureKBAccess(c, kbID) {
		Fail(c, CodeNotFound, "知识库不存在")
		return
	}
	docs, err := h.store.ListDocuments(c.Request.Context(), kbID)
	if err != nil {
		Fail(c, CodeInternal, "查询文档失败")
		return
	}
	OK(c, docs)
}

// DeleteDocument 删除文档：向量库 + BM25 索引 + 记录
//
//	@Summary		删除文档
//	@Description	删除文档：从向量库删除该文档全部 chunk、从 BM25 索引移除、更新文档记录与任务状态
//	@Tags			文档
//	@Produce		json
//	@Param			id	path		string	true	"文档 ID"
//	@Success		200	{object}	Response{data=object{id=string}}
//	@Failure		401	{object}	Response
//	@Failure		404	{object}	Response
//	@Failure		500	{object}	Response
//	@Security		ApiKeyAuth
//	@Router			/api/v1/documents/{id} [delete]
func (h *handler) DeleteDocument(c *gin.Context) {
	ctx := c.Request.Context()
	docID := c.Param("id")

	doc, err := h.store.GetDocument(ctx, docID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "文档不存在")
			return
		}
		Fail(c, CodeInternal, "查询文档失败")
		return
	}
	// 经文档所属知识库校验访问权（越权一律 404）
	if !h.ensureKBAccess(c, doc.KBID) {
		Fail(c, CodeNotFound, "文档不存在")
		return
	}

	h.deleteDocument(ctx, *doc)
	OK(c, gin.H{"id": docID})
}

// deleteDocument 清理文档的向量与索引并删除记录
func (h *handler) deleteDocument(ctx context.Context, doc store.Document) {
	if len(doc.ChunkIDs) > 0 {
		if err := h.vs.Delete(ctx, doc.ChunkIDs); err != nil {
			// 向量删除失败不阻断记录删除，日志告警
			slog.Warn("删除向量失败", "doc", doc.ID, "err", err)
		}
		if h.bm25 != nil {
			for _, chunkID := range doc.ChunkIDs {
				h.bm25.Remove(chunkID)
			}
		}
	}
	// 清理磁盘文件
	_ = os.Remove(doc.FilePath)
	_ = h.store.DeleteDocument(ctx, doc.ID)
}

// precheckReadable 上传预检：解析文件并校验可读文本量。
// 返回 ErrNoReadableContent 等错误时调用方拒绝上传。
func precheckReadable(file *multipart.FileHeader, parser loader.Parser, info loader.FileInfo, minChars int) error {
	f, err := file.Open()
	if err != nil {
		return fmt.Errorf("读取上传文件失败: %w", err)
	}
	defer f.Close()

	result, err := parser.Parse(context.Background(), f, loader.LoadOptions{Mode: loader.ModeStrict})
	if err != nil {
		// 解析失败视为无法处理，明确告知用户（tolerant 模式下 parser 也可能返回带 Warnings 的结果）
		return fmt.Errorf("文件解析失败，内容可能无法识别: %v", err)
	}
	return loader.ValidateReadable(result.Document, minChars)
}
