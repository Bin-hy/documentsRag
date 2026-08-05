package api

import (
	"errors"

	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// GetTask 任务状态
func (h *handler) GetTask(c *gin.Context) {
	task, err := h.store.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "任务不存在")
			return
		}
		Fail(c, CodeInternal, "查询任务失败")
		return
	}
	OK(c, task)
}

// RetryTask 手动重试失败任务（failed → pending，重试次数清零）
func (h *handler) RetryTask(c *gin.Context) {
	ctx := c.Request.Context()
	task, err := h.store.GetTask(ctx, c.Param("id"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Fail(c, CodeNotFound, "任务不存在")
			return
		}
		Fail(c, CodeInternal, "查询任务失败")
		return
	}

	if task.Status != store.TaskStatusFailed {
		Fail(c, CodeBadRequest, "仅 failed 状态的任务可重试")
		return
	}

	task.Status = store.TaskStatusPending
	task.RetryCount = 0
	task.ErrorMessage = ""
	if err := h.store.UpdateTask(ctx, *task); err != nil {
		Fail(c, CodeInternal, "更新任务失败")
		return
	}
	OK(c, task)
}
