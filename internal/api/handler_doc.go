package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	if _, err := h.store.GetKB(ctx, kbID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "知识库不存在")
			return
		}
		Fail(c, CodeInternal, "查询知识库失败")
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
	if _, err := h.registry.Resolve(info); err != nil {
		Fail(c, CodeBadRequest, "不支持的文件格式: "+filepath.Ext(file.Filename))
		return
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

	// 创建文档与任务记录
	doc := store.Document{
		ID:        docID,
		KBID:      kbID,
		Filename:  file.Filename,
		Format:    ext,
		Size:      file.Size,
		Status:    store.DocStatusPending,
		FilePath:  filePath,
		CreatedAt: time.Now(),
	}
	if err := h.store.CreateDocument(ctx, doc); err != nil {
		Fail(c, CodeInternal, "创建文档记录失败")
		return
	}

	taskID := uuid.New().String()
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
func (h *handler) ListDocuments(c *gin.Context) {
	kbID := c.Query("kb_id")
	if kbID == "" {
		Fail(c, CodeBadRequest, "缺少 kb_id")
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
