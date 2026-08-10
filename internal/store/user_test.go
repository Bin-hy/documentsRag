package store

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// GetOrCreateUser：INSERT ON CONFLICT 绑定参数正确
func TestGetOrCreateUser(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()

	s := &pgStore{pool: mock}
	u := User{ID: "user-1", Provider: "github", Subject: "12345", Name: "octocat", Email: ""}
	ts := now()

	rows := pgxmock.NewRows([]string{"id", "provider", "subject", "name", "email", "created_at"}).
		AddRow("user-1", "github", "12345", "octocat", "", ts)

	mock.ExpectQuery("INSERT INTO users").
		WithArgs(u.ID, u.Provider, u.Subject, u.Name, u.Email, pgxmock.AnyArg()).
		WillReturnRows(rows)

	got, err := s.GetOrCreateUser(context.Background(), u)
	if err != nil {
		t.Fatalf("GetOrCreateUser 失败: %v", err)
	}
	if got.ID != "user-1" || got.Subject != "12345" {
		t.Errorf("返回用户解析错误: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}

// GetOrCreateUser：空 provider/subject 直接报错
func TestGetOrCreateUserEmpty(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	s := &pgStore{pool: mock}
	if _, err := s.GetOrCreateUser(context.Background(), User{}); err == nil {
		t.Fatal("空 provider/subject 应报错")
	}
}

// GetUser：命中返回用户；不存在返回 (nil, nil)
func TestGetUser(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("创建 mock 失败: %v", err)
	}
	defer mock.Close()
	s := &pgStore{pool: mock}
	ts := now()

	rows := pgxmock.NewRows([]string{"id", "provider", "subject", "name", "email", "created_at"}).
		AddRow("user-1", "github", "12345", "octocat", "", ts)
	mock.ExpectQuery("SELECT id, provider, subject").
		WithArgs("user-1").
		WillReturnRows(rows)
	got, err := s.GetUser(context.Background(), "user-1")
	if err != nil || got == nil || got.Name != "octocat" {
		t.Fatalf("GetUser 异常: got=%+v err=%v", got, err)
	}

	mock.ExpectQuery("SELECT id, provider, subject").
		WithArgs("missing").
		WillReturnRows(pgxmock.NewRows([]string{"id", "provider", "subject", "name", "email", "created_at"}))
	got, err = s.GetUser(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetUser 缺失查询报错: %v", err)
	}
	if got != nil {
		t.Fatalf("缺失用户应返回 nil: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("期望未满足: %v", err)
	}
}
