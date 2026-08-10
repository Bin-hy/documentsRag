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
// OwnerID：nullable，NULL = 系统级知识库（由系统级 API Key 创建/可访问）；非 NULL = 归属该登录用户
type KnowledgeBase struct {
	ID          string
	Name        string
	Description string
	Strategy    string // 策略配置 JSON（空 = 用全局默认）
	OwnerID     *string
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
	// —— 以下为 MCP 权限（spec F4/F5/F6）——
	MCPTools   []string // 允许调用的 MCP Tool 白名单；空 = 无任何 MCP 权限
	MCPKBScope string   // ""（无 MCP 知识库权限）| "all"（全部）| "allowlist"（仅 MCPKBIDs）
	MCPKBIDs   []string // MCPKBScope=="allowlist" 时的知识库白名单
}

// APIKeyPermissions Key 的 MCP 权限（管理接口更新请求体）
type APIKeyPermissions struct {
	MCPTools   []string `json:"mcp_tools"`    // 允许的 Tool 白名单；nil = 清空
	MCPKBScope string   `json:"mcp_kb_scope"` // "" | "all" | "allowlist"
	MCPKBIDs   []string `json:"mcp_kb_ids"`   // 知识库白名单；nil = 清空
}

// AuditLog MCP 调用审计记录（spec F7：仅记录 api_key_id 引用与截断参数，绝不存 Secret/Token）
type AuditLog struct {
	ID           int64
	APIKeyID     string
	ToolName     string
	Params       string // 截断后参数 JSON（默认 ≤2000 字符）
	ParamsLen    int    // 截断前原始参数长度
	Status       string // success / error
	ErrorMessage string
	DurationMS   int64
	CreatedAt    time.Time
}

// Store PostgreSQL 元数据访问接口
type Store interface {
	// 知识库
	CreateKB(ctx context.Context, kb KnowledgeBase) error
	// ListAllKBs 全量列出（含系统级 owner_id IS NULL 与用户级 owner_id 非 NULL），系统级 API Key 用
	ListAllKBs(ctx context.Context) ([]KnowledgeBase, error)
	// ListKBsByOwner 仅列出归属该用户的知识库（owner_id = ownerID）
	ListKBsByOwner(ctx context.Context, ownerID string) ([]KnowledgeBase, error)
	// ListKBsByIDs 按 ID 白名单查询（MCP allowlist 授权用，不过滤 owner）
	ListKBsByIDs(ctx context.Context, ids []string) ([]KnowledgeBase, error)
	GetKB(ctx context.Context, id string) (*KnowledgeBase, error)
	UpdateKB(ctx context.Context, kb KnowledgeBase) error
	DeleteKB(ctx context.Context, id string) error
	// 用户（三方登录）
	GetOrCreateUser(ctx context.Context, u User) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
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
	// UpdateAPIKeyPermissions 全量更新 Key 的 MCP 权限（PUT 语义；nil 切片视为清空）
	UpdateAPIKeyPermissions(ctx context.Context, id string, p APIKeyPermissions) error
	// AppendAuditLog 写入 MCP 调用审计记录（异步 worker 后台调用）
	AppendAuditLog(ctx context.Context, log AuditLog) error
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
