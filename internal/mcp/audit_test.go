package mcp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// fakeAuditStore 记录 worker 写入的审计（并发安全）
type fakeAuditStore struct {
	mu   sync.Mutex
	logs []store.AuditLog
}

func (f *fakeAuditStore) AppendAuditLog(ctx context.Context, log store.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, log)
	return nil
}

func (f *fakeAuditStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.logs)
}

// 正常投递：worker 异步写入收到
func TestAuditSinkSubmitAndFlush(t *testing.T) {
	st := &fakeAuditStore{}
	s := NewAuditSink(st, 1024, 2000)

	for i := 0; i < 50; i++ {
		s.Submit(store.AuditLog{
			APIKeyID: "key-1", ToolName: "ask",
			Params: `{"question":"测试"}`, Status: "success",
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Shutdown(ctx)

	if got := st.count(); got != 50 {
		t.Errorf("Shutdown 后应写入全部 50 条，实际 %d", got)
	}
}

// 队列满：Submit 不阻塞、丢弃并 warn，主流程可继续
func TestAuditSinkQueueFullNonBlocking(t *testing.T) {
	st := &fakeAuditStore{}
	s := NewAuditSink(st, 2, 2000) // 极小 buffer

	done := make(chan struct{})
	go func() {
		// 大量 Submit（远超 buffer），若阻塞将 hang
		for i := 0; i < 1000; i++ {
			s.Submit(store.AuditLog{APIKeyID: "key-1", ToolName: "retrieve", Params: "{}"})
		}
		close(done)
	}()

	select {
	case <-done:
		// 主流程未被阻塞
	case <-time.After(3 * time.Second):
		t.Fatal("Submit 在队列满时阻塞了主流程")
	}
	s.Shutdown(context.Background())
}

// 参数截断：params ≤ limit，params_len = 截断前原始长度
func TestAuditSinkTruncation(t *testing.T) {
	st := &fakeAuditStore{}
	s := NewAuditSink(st, 8, 10) // limit=10

	long := strings.Repeat("中", 100) // 100 个 rune
	s.Submit(store.AuditLog{APIKeyID: "key-1", ToolName: "ask", Params: long})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Shutdown(ctx)

	if st.count() != 1 {
		t.Fatalf("应写入 1 条，实际 %d", st.count())
	}
	log := st.logs[0]
	if got := len([]rune(log.Params)); got != 10 {
		t.Errorf("截断后应 10 个字符，实际 %d", got)
	}
	if log.ParamsLen != len(long) {
		t.Errorf("params_len 应为原始字节长度 %d，实际 %d", len(long), log.ParamsLen)
	}
}

// Shutdown 后 Submit 不 panic（防御 send-on-closed）
func TestAuditSinkSubmitAfterShutdown(t *testing.T) {
	st := &fakeAuditStore{}
	s := NewAuditSink(st, 4, 100)
	s.Shutdown(context.Background())
	s.Submit(store.AuditLog{APIKeyID: "key-1", ToolName: "ask", Params: "{}"}) // 不应 panic
}
