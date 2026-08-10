package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// 票据默认 TTL
const (
	// stateTTL 授权码流程 state 有效期（防 CSRF；OIDC 场景同时绑定 nonce）
	stateTTL = 10 * time.Minute
	// ticketTTL 换 JWT 的一次性 ticket 有效期
	ticketTTL = 2 * time.Minute
)

// newToken 生成高熵随机票据（crypto/rand 32 字节 → base64url）
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// stateEntry 授权码流程 state 记录（绑定 provider 与 OIDC nonce；GitHub 的 nonce 恒为空串）
type stateEntry struct {
	Provider  string
	Nonce     string
	ExpiresAt time.Time
}

// stateStore 一次性 state 存储（TTL、原子「读取+删除」、并发安全）
type stateStore struct {
	mu sync.Mutex
	m  map[string]stateEntry
}

func newStateStore() *stateStore {
	return &stateStore{m: make(map[string]stateEntry)}
}

// New 生成并存储一个 state；ttl<=0 时用默认 stateTTL
func (s *stateStore) New(provider, nonce string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = stateTTL
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.m[token] = stateEntry{Provider: provider, Nonce: nonce, ExpiresAt: time.Now().Add(ttl)}
	return token, nil
}

// Consume 原子「读取+删除」：state 不存在、已过期或类型不符 → ok=false；成功仅一次
func (s *stateStore) Consume(state string) (provider, nonce string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.m[state]
	if !exists {
		return "", "", false
	}
	delete(s.m, state)
	if time.Now().After(e.ExpiresAt) {
		return "", "", false
	}
	return e.Provider, e.Nonce, true
}

// cleanupLocked 顺带清理过期项（调用方须持锁）
func (s *stateStore) cleanupLocked(now time.Time) {
	for k, e := range s.m {
		if now.After(e.ExpiresAt) {
			delete(s.m, k)
		}
	}
}

// ticketEntry 换 JWT 的一次性 ticket 记录（绑定已认证用户；不保存 nonce）
type ticketEntry struct {
	UserID    string
	Provider  string
	ExpiresAt time.Time
}

// ticketStore 一次性 ticket 存储（TTL、原子消费、并发安全）
type ticketStore struct {
	mu sync.Mutex
	m  map[string]ticketEntry
}

func newTicketStore() *ticketStore {
	return &ticketStore{m: make(map[string]ticketEntry)}
}

// New 生成并存储一个 ticket；ttl<=0 时用默认 ticketTTL
func (s *ticketStore) New(userID, provider string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = ticketTTL
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.m[token] = ticketEntry{UserID: userID, Provider: provider, ExpiresAt: time.Now().Add(ttl)}
	return token, nil
}

// Consume 原子「读取+删除」：ticket 不存在/已过期 → ok=false；成功后立即删除，重放失败
func (s *ticketStore) Consume(ticket string) (userID, provider string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.m[ticket]
	if !exists {
		return "", "", false
	}
	delete(s.m, ticket)
	if time.Now().After(e.ExpiresAt) {
		return "", "", false
	}
	return e.UserID, e.Provider, true
}

// cleanupLocked 顺带清理过期项（调用方须持锁）
func (s *ticketStore) cleanupLocked(now time.Time) {
	for k, e := range s.m {
		if now.After(e.ExpiresAt) {
			delete(s.m, k)
		}
	}
}
