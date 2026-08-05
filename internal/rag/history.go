package rag

import (
	"sync"

	"github.com/Bin-hy/bin-rag/internal/llm"
)

// HistoryStore 对话历史存储接口（数据库实现留待阶段七）
type HistoryStore interface {
	Append(sessionID string, role string, content string) error
	Get(sessionID string, limit int) ([]llm.Message, error) // 返回最近 limit 条
	Clear(sessionID string) error
}

// memoryHistoryStore 内存实现：map + RWMutex，线程安全
type memoryHistoryStore struct {
	mu       sync.RWMutex
	capacity int
	sessions map[string][]llm.Message
}

// NewMemoryHistoryStore 创建内存历史存储
func NewMemoryHistoryStore(capacity int) HistoryStore {
	if capacity <= 0 {
		capacity = 50
	}
	return &memoryHistoryStore{
		capacity: capacity,
		sessions: make(map[string][]llm.Message),
	}
}

// Append 追加消息，超出容量时丢弃最旧消息
func (s *memoryHistoryStore) Append(sessionID string, role string, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.sessions[sessionID]
	msgs = append(msgs, llm.Message{Role: role, Content: content})

	if len(msgs) > s.capacity {
		overflow := len(msgs) - s.capacity
		msgs = msgs[overflow:]
	}

	s.sessions[sessionID] = msgs
	return nil
}

// Get 返回最近 limit 条；limit <= 0 或超过存量时返回全部（副本，防止调用方修改内部数据）
func (s *memoryHistoryStore) Get(sessionID string, limit int) ([]llm.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.sessions[sessionID]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

// Clear 清空指定 session 的历史
func (s *memoryHistoryStore) Clear(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	return nil
}
