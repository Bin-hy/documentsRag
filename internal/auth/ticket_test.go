package auth

import (
	"sync"
	"testing"
	"time"
)

// state：New→Consume 成功；二次 Consume 失败（一次性）；过期失败
func TestStateStore(t *testing.T) {
	s := newStateStore()
	code, err := s.New("github", "", 0)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if code == "" {
		t.Fatal("state 不能为空")
	}

	prov, nonce, ok := s.Consume(code)
	if !ok || prov != "github" || nonce != "" {
		t.Fatalf("Consume 应成功并返回绑定值: prov=%q nonce=%q ok=%v", prov, nonce, ok)
	}
	if _, _, ok := s.Consume(code); ok {
		t.Fatal("二次 Consume 应失败（一次性）")
	}

	// 过期（直接置过期时间，避免 New 的 ttl<=0 默认语义干扰）
	code2, _ := s.New("oidc-prov", "nonce-1", 0)
	e := s.m[code2]
	e.ExpiresAt = time.Now().Add(-time.Second)
	s.m[code2] = e
	if _, _, ok := s.Consume(code2); ok {
		t.Fatal("过期 state 应 Consume 失败")
	}
}

// ticket：New→Consume 一次成功，重放失败；ticket 不保存 nonce
func TestTicketStore(t *testing.T) {
	s := newTicketStore()
	tk, err := s.New("user-1", "github", 0)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	uid, prov, ok := s.Consume(tk)
	if !ok || uid != "user-1" || prov != "github" {
		t.Fatalf("Consume 应成功: uid=%q prov=%q ok=%v", uid, prov, ok)
	}
	if _, _, ok := s.Consume(tk); ok {
		t.Fatal("ticket 重放应失败")
	}

	// 过期
	tk2, _ := s.New("user-2", "github", 0)
	e := s.m[tk2]
	e.ExpiresAt = time.Now().Add(-time.Second)
	s.m[tk2] = e
	if _, _, ok := s.Consume(tk2); ok {
		t.Fatal("过期 ticket 应失败")
	}
}

// 并发消费：同一 state/ticket 仅一次成功
func TestConcurrentConsume(t *testing.T) {
	ss := newStateStore()
	code, _ := ss.New("github", "", 0)
	ts := newTicketStore()
	tk, _ := ts.New("user-1", "github", 0)

	var wg sync.WaitGroup
	var stateOK, ticketOK int
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, ok := ss.Consume(code); ok {
				mu.Lock()
				stateOK++
				mu.Unlock()
			}
			if _, _, ok := ts.Consume(tk); ok {
				mu.Lock()
				ticketOK++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if stateOK != 1 || ticketOK != 1 {
		t.Fatalf("并发消费应各仅一次成功: state=%d ticket=%d", stateOK, ticketOK)
	}
}

// 顺带清理：插入过期项后 New 会将其清除
func TestCleanupOnNew(t *testing.T) {
	ss := newStateStore()
	expired, _ := ss.New("github", "", 0)
	e := ss.m[expired]
	e.ExpiresAt = time.Now().Add(-time.Minute)
	ss.m[expired] = e
	if _, ok := ss.m[expired]; !ok {
		t.Fatal("过期项应先存在")
	}
	if _, err := ss.New("github", "", 0); err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if _, ok := ss.m[expired]; ok {
		t.Fatal("New 时应顺带清理过期项")
	}

	ts := newTicketStore()
	expiredTk, _ := ts.New("u1", "github", 0)
	e2 := ts.m[expiredTk]
	e2.ExpiresAt = time.Now().Add(-time.Minute)
	ts.m[expiredTk] = e2
	if _, err := ts.New("u2", "github", 0); err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if _, ok := ts.m[expiredTk]; ok {
		t.Fatal("ticketStore New 时应顺带清理过期项")
	}
}
