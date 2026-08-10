package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// User 登录用户（三方登录自动注册；provider+subject 唯一）
type User struct {
	ID        string
	Provider  string // github / 自定义 OIDC 标识
	Subject   string // GitHub=数字用户 ID；OIDC=sub
	Name      string
	Email     string
	CreatedAt time.Time
}

// GetOrCreateUser 按 (provider, subject) 查询或创建用户。
// 已存在时刷新 name/email（每次登录同步展示信息），返回同一用户记录（复用 ID）。
func (s *pgStore) GetOrCreateUser(ctx context.Context, u User) (*User, error) {
	if u.Provider == "" || u.Subject == "" {
		return nil, fmt.Errorf("用户 provider 与 subject 不能为空")
	}
	var out User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, provider, subject, name, email, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (provider, subject)
		 DO UPDATE SET name = EXCLUDED.name, email = EXCLUDED.email
		 RETURNING id, provider, subject, name, email, created_at`,
		u.ID, u.Provider, u.Subject, u.Name, u.Email, time.Now(),
	).Scan(&out.ID, &out.Provider, &out.Subject, &out.Name, &out.Email, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUser 按 ID 查询用户（/auth/me 取展示信息用）
func (s *pgStore) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, provider, subject, name, email, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Provider, &u.Subject, &u.Name, &u.Email, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}
