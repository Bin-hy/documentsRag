// Package store 提供 PostgreSQL 元数据存储：知识库 / 文档 / 入库任务 / API Key / 对话历史。
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryExecutor pgx 兼容查询接口（pgxpool.Pool 与测试用 pgxmock.Pool 均满足）
type queryExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// 任务状态
const (
	TaskStatusPending    = "pending"
	TaskStatusProcessing = "processing"
	TaskStatusCompleted  = "completed"
	TaskStatusFailed     = "failed"
)

// 文档入库状态
const (
	DocStatusPending    = "pending"
	DocStatusProcessing = "processing"
	DocStatusCompleted  = "completed"
	DocStatusFailed     = "failed"
)

// KnowledgeBase 知识库
type KnowledgeBase struct {
	ID          string
	Name        string
	Description string
	Strategy    string // 策略配置 JSON（空 = 用全局默认）
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Document 文档
type Document struct {
	ID        string
	KBID      string
	Filename  string
	Format    string
	Size      int64
	Status    string // pending / processing / completed / failed
	ChunkIDs  []string
	FilePath  string
	TaskID    string
	CreatedAt time.Time
}

// Task 入库任务
type Task struct {
	ID           string
	KBID         string
	DocumentID   string
	Status       string // pending / processing / completed / failed
	RetryCount   int
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// APIKey 访问密钥（只存 hash）
type APIKey struct {
	ID         string
	Name       string
	KeyHash    string // SHA-256 hex
	Enabled    bool
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// Store PostgreSQL 元数据访问接口
type Store interface {
	// 知识库
	CreateKB(ctx context.Context, kb KnowledgeBase) error
	ListKBs(ctx context.Context) ([]KnowledgeBase, error)
	GetKB(ctx context.Context, id string) (*KnowledgeBase, error)
	UpdateKB(ctx context.Context, kb KnowledgeBase) error
	DeleteKB(ctx context.Context, id string) error
	// 文档
	CreateDocument(ctx context.Context, doc Document) error
	ListDocuments(ctx context.Context, kbID string) ([]Document, error)
	GetDocument(ctx context.Context, id string) (*Document, error)
	UpdateDocumentStatus(ctx context.Context, id string, status string, chunkIDs []string) error
	DeleteDocument(ctx context.Context, id string) error
	// 任务
	CreateTask(ctx context.Context, t Task) error
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasks(ctx context.Context, kbID string) ([]Task, error)
	UpdateTask(ctx context.Context, t Task) error
	ClaimPendingTasks(ctx context.Context, limit int) ([]Task, error)
	ResetProcessingTasks(ctx context.Context) error
	// API Key
	CreateAPIKey(ctx context.Context, k APIKey) error
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)
	SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) error
	DeleteAPIKey(ctx context.Context, id string) error
	TouchAPIKey(ctx context.Context, id string) error
	// 对话历史（由 PostgresHistoryStore 实现 rag.HistoryStore）
	HistoryStore() HistoryStore
	Migrate(ctx context.Context) error
	Close()
}

type pgStore struct {
	pool queryExecutor
}

// NewStore 创建 PostgreSQL 存储（建立连接池并 Ping 校验）
func NewStore(ctx context.Context, dsn string) (Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &pgStore{pool: pool}, nil
}

// Close 关闭连接池
func (s *pgStore) Close() {
	s.pool.Close()
}

// HistoryStore 返回对话历史存储（实现 rag.HistoryStore）
func (s *pgStore) HistoryStore() HistoryStore {
	return &PostgresHistoryStore{pool: s.pool}
}
