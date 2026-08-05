package rag

import (
	"sync"
	"testing"

	"github.com/Bin-hy/bin-rag/internal/llm"
)

// AC9: 超容量丢弃最旧消息
func TestHistoryStore_CapacityEvictsOldest(t *testing.T) {
	hs := NewMemoryHistoryStore(3)

	for i := 1; i <= 4; i++ {
		if err := hs.Append("s1", llm.RoleUser, string(rune('0'+i))); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}

	msgs, err := hs.Get("s1", 0)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("容量裁剪失败: got %d want 3", len(msgs))
	}
	// 最旧的 "1" 被丢弃，剩下 "2","3","4"
	if msgs[0].Content != "2" || msgs[2].Content != "4" {
		t.Errorf("最旧消息未正确丢弃: %+v", msgs)
	}
}

// AC9: Get 返回最近 limit 条
func TestHistoryStore_GetLimit(t *testing.T) {
	hs := NewMemoryHistoryStore(50)

	for i := 1; i <= 5; i++ {
		_ = hs.Append("s1", llm.RoleUser, string(rune('0'+i)))
	}

	msgs, err := hs.Get("s1", 3)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("limit 未生效: got %d want 3", len(msgs))
	}
	if msgs[0].Content != "3" || msgs[2].Content != "5" {
		t.Errorf("最近 limit 条错误: %+v", msgs)
	}
}

// AC9: 不同 session 隔离；Clear 清空
func TestHistoryStore_SessionIsolation(t *testing.T) {
	hs := NewMemoryHistoryStore(10)
	_ = hs.Append("s1", llm.RoleUser, "a")
	_ = hs.Append("s2", llm.RoleUser, "b")

	s1, _ := hs.Get("s1", 0)
	s2, _ := hs.Get("s2", 0)
	if len(s1) != 1 || len(s2) != 1 {
		t.Fatalf("session 隔离失败: s1=%d s2=%d", len(s1), len(s2))
	}

	_ = hs.Clear("s1")
	s1, _ = hs.Get("s1", 0)
	if len(s1) != 0 {
		t.Errorf("Clear 失败: %d", len(s1))
	}
}

// N2: 并发读写无竞争
func TestHistoryStore_Concurrent(t *testing.T) {
	hs := NewMemoryHistoryStore(100)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			session := "s" + string(rune('A'+g))
			for i := 0; i < 50; i++ {
				_ = hs.Append(session, llm.RoleUser, "msg")
				_, _ = hs.Get(session, 5)
			}
		}(g)
	}
	wg.Wait()
}
