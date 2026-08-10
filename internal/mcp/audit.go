package mcp

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// auditStore 审计写入所需的最小存储接口（真实 store.Store 满足；测试可用最小 fake）
type auditStore interface {
	AppendAuditLog(ctx context.Context, log store.AuditLog) error
}

// AuditSink 异步审计（plan D8）：
//   - Submit 非阻塞投递到 buffered channel（队列满丢弃并 warn，不阻塞 MCP 主请求）
//   - 后台 worker 从 channel 取事件写入数据库（失败仅 warn）
//   - Shutdown 停止接收 → flush 剩余 → 退出 worker（防 goroutine 泄漏，由 App 管理生命周期）
//
// 只接收 store.AuditLog（仅 api_key_id 引用与截断参数，结构无 Secret/Token 字段，spec F7）。
type AuditSink struct {
	st         auditStore
	ch         chan store.AuditLog
	paramLimit int
	done       chan struct{}
	closed     atomic.Bool // Shutdown 后禁止 Submit（防御 send-on-closed）
	closeOnce  sync.Once
}

// NewAuditSink 创建审计 sink 并启动后台 worker
func NewAuditSink(st auditStore, bufSize, paramLimit int) *AuditSink {
	if bufSize <= 0 {
		bufSize = 1024
	}
	if paramLimit <= 0 {
		paramLimit = 2000
	}
	s := &AuditSink{
		st:         st,
		ch:         make(chan store.AuditLog, bufSize),
		paramLimit: paramLimit,
		done:       make(chan struct{}),
	}
	go s.run()
	return s
}

// Submit 非阻塞投递审计事件：截断参数（默认 ≤2000 字符）并记录截断前原始长度（spec N4）。
// 队列满 → 丢弃并 warn，绝不影响 MCP 主请求耗时。
func (s *AuditSink) Submit(log store.AuditLog) {
	if s.closed.Load() {
		return
	}
	// 截断前原始长度（字节）
	log.ParamsLen = len(log.Params)
	if s.paramLimit > 0 {
		if runes := []rune(log.Params); len(runes) > s.paramLimit {
			log.Params = string(runes[:s.paramLimit])
		}
	}
	select {
	case s.ch <- log:
	default:
		slog.Warn("MCP 审计队列已满，丢弃该审计事件",
			"api_key_id", log.APIKeyID, "tool", log.ToolName)
	}
}

// Shutdown 停止接收新事件并 flush 剩余队列：close(ch) → worker 消费完剩余 → 退出。
// ctx 用于等待超时；正常情况应快速完成。
func (s *AuditSink) Shutdown(ctx context.Context) {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.ch)
	})
	select {
	case <-s.done:
	case <-ctx.Done():
		slog.Warn("MCP 审计 Shutdown 超时，可能丢失部分审计")
	}
}

// run 后台 worker：消费 channel 直到关闭并排空
func (s *AuditSink) run() {
	defer close(s.done)
	for log := range s.ch {
		if err := s.st.AppendAuditLog(context.Background(), log); err != nil {
			slog.Warn("MCP 审计写入失败",
				"api_key_id", log.APIKeyID, "tool", log.ToolName, "err", err)
		}
	}
}
