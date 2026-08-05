// Package task 提供入库任务的异步 worker 池：轮询领取、状态机流转、失败重试、重启恢复。
package task

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/loader"
	"github.com/Bin-hy/bin-rag/internal/pipeline"
	"github.com/Bin-hy/bin-rag/internal/store"
)

const (
	pollInterval   = 500 * time.Millisecond // 无任务时的轮询间隔
	errorBackoff   = 1 * time.Second        // 领取出错后的退避
	claimBatchSize = 1                      // 每 worker 每次领取任务数
)

// WorkerPool 入库任务 worker 池
type WorkerPool interface {
	Start(ctx context.Context) // 启动 worker（先重置悬挂任务）
	Shutdown()                 // 停止 worker 并等待当前任务完成
}

type defaultWorkerPool struct {
	cfg      config.ServerConfig
	store    store.Store
	pipeline pipeline.Pipeline
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewWorkerPool 创建 worker 池
func NewWorkerPool(cfg config.ServerConfig, s store.Store, p pipeline.Pipeline) WorkerPool {
	return &defaultWorkerPool{cfg: cfg, store: s, pipeline: p}
}

// Start 启动 worker 池：先重置 processing 悬挂任务，再启动 WorkerCount 个 worker
func (w *defaultWorkerPool) Start(ctx context.Context) {
	if err := w.store.ResetProcessingTasks(ctx); err != nil {
		slog.Warn("重置悬挂任务失败", "err", err)
	}

	wctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	for i := 0; i < w.cfg.WorkerCount; i++ {
		w.wg.Add(1)
		go w.workerLoop(wctx)
	}
	slog.Info("入库 worker 已启动", "count", w.cfg.WorkerCount)
}

// Shutdown 停止 worker，等待当前任务完成
func (w *defaultWorkerPool) Shutdown() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("入库 worker 已停止")
}

// workerLoop 单个 worker 循环：领取 → 处理
func (w *defaultWorkerPool) workerLoop(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tasks, err := w.store.ClaimPendingTasks(ctx, claimBatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("领取任务失败", "err", err)
			time.Sleep(errorBackoff)
			continue
		}

		if len(tasks) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			continue
		}

		for _, t := range tasks {
			// 用不随 Shutdown 取消的 ctx 处理任务：保证失败时状态能落库（防残留 processing）
			w.process(context.WithoutCancel(ctx), t)
		}
	}
}

// process 处理单个任务：读取文件 → 入库 → 更新状态
func (w *defaultWorkerPool) process(ctx context.Context, t store.Task) {
	doc, err := w.store.GetDocument(ctx, t.DocumentID)
	if err != nil {
		w.fail(ctx, t, err)
		return
	}

	f, err := os.Open(doc.FilePath)
	if err != nil {
		w.fail(ctx, t, err)
		return
	}
	defer f.Close()

	start := time.Now()
	chunkIDs, err := w.pipeline.Ingest(ctx, pipeline.IngestRequest{
		KBID:       t.KBID,
		DocumentID: t.DocumentID,
		Reader:     f,
		Info:       loader.FileInfo{Filename: doc.Filename},
	})
	if err != nil {
		w.fail(ctx, t, err)
		return
	}

	t.Status = store.TaskStatusCompleted
	t.ErrorMessage = ""
	if err := w.store.UpdateTask(ctx, t); err != nil {
		slog.Warn("更新任务状态失败", "task", t.ID, "err", err)
	}
	if err := w.store.UpdateDocumentStatus(ctx, t.DocumentID, store.DocStatusCompleted, chunkIDs); err != nil {
		slog.Warn("更新文档状态失败", "doc", t.DocumentID, "err", err)
	}
	slog.Info("入库任务完成", "task", t.ID, "doc", t.DocumentID, "chunks", len(chunkIDs),
		"耗时ms", time.Since(start).Milliseconds())
}

// fail 任务失败处理：未超上限回 pending 重试，否则 failed
func (w *defaultWorkerPool) fail(ctx context.Context, t store.Task, err error) {
	if t.RetryCount < w.cfg.TaskMaxRetries {
		t.Status = store.TaskStatusPending
		t.RetryCount++
		t.ErrorMessage = err.Error()
		slog.Warn("入库任务失败，准备重试", "task", t.ID, "retry", t.RetryCount, "err", err)
	} else {
		t.Status = store.TaskStatusFailed
		t.ErrorMessage = err.Error()
		slog.Error("入库任务失败（超过重试上限）", "task", t.ID, "err", err)
	}
	if err := w.store.UpdateTask(ctx, t); err != nil {
		slog.Warn("更新任务状态失败", "task", t.ID, "err", err)
	}
}
