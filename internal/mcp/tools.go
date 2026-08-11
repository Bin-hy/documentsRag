package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Tool 名称常量（网关层授权 / 审计共用）
const (
	ToolListKBs  = "list_knowledge_bases"
	ToolGetKB    = "get_knowledge_base"
	ToolRetrieve = "retrieve"
	ToolAsk      = "ask"
	ToolListDocs = "list_documents"
	ToolGetTask  = "get_task"
)

// AllTools 全部已注册 Tool 名（tools/list 全量返回；授权按 Key 白名单拦截）
var AllTools = []string{ToolListKBs, ToolGetKB, ToolRetrieve, ToolAsk, ToolListDocs, ToolGetTask}

// tools 6 个只读 Tool 的实现（spec F2）
type tools struct {
	st     store.Store
	engine func() rag.Engine
	rt     func() retriever.Retriever
	cfg    *config.ConfigManager
	audit  *AuditSink
}

// register 注册 6 个 Tool 到 mcp-go server
func (t *tools) register(s *mcpserver.MCPServer) {
	s.AddTool(mcpgo.NewTool(ToolListKBs, mcpgo.WithDescription("列出当前凭据可访问的知识库（全部或白名单）")), t.handleListKBs)
	s.AddTool(mcpgo.NewTool(ToolGetKB,
		mcpgo.WithDescription("按 ID 返回单个知识库详情；无权限按不存在处理"),
		mcpgo.WithString("kb_id", mcpgo.Required(), mcpgo.Description("知识库 ID")),
	), t.handleGetKB)
	s.AddTool(mcpgo.NewTool(ToolRetrieve,
		mcpgo.WithDescription("纯检索：按查询文本召回知识库 chunk 及来源信息"),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("查询文本")),
		mcpgo.WithString("kb_id", mcpgo.Description("知识库 ID，缺省用凭据可访问范围")),
		mcpgo.WithNumber("top_k", mcpgo.Description("召回数量，缺省用全局配置")),
	), t.handleRetrieve)
	s.AddTool(mcpgo.NewTool(ToolAsk,
		mcpgo.WithDescription("RAG 问答：基于知识库回答并返回引用来源（不暴露内部推理）"),
		mcpgo.WithString("question", mcpgo.Required(), mcpgo.Description("问题")),
		mcpgo.WithString("kb_id", mcpgo.Description("知识库 ID，缺省用凭据可访问范围")),
		mcpgo.WithString("session_id", mcpgo.Description("会话 ID（复用现有会话历史），缺省为新会话")),
	), t.handleAsk)
	s.AddTool(mcpgo.NewTool(ToolListDocs,
		mcpgo.WithDescription("列出知识库（或当前凭据可访问范围）内的文档"),
		mcpgo.WithString("kb_id", mcpgo.Description("知识库 ID，缺省列出可访问全部知识库的文档")),
	), t.handleListDocs)
	s.AddTool(mcpgo.NewTool(ToolGetTask,
		mcpgo.WithDescription("按任务 ID 查询入库任务状态与错误信息"),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("入库任务 ID")),
	), t.handleGetTask)
}

// ---------- 工具元数据视图（对外 JSON 结构） ----------

type kbView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Strategy    string    `json:"strategy,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type docView struct {
	ID        string    `json:"id"`
	KBID      string    `json:"kb_id"`
	Filename  string    `json:"filename"`
	Format    string    `json:"format"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type taskView struct {
	ID           string    `json:"id"`
	KBID         string    `json:"kb_id"`
	DocumentID   string    `json:"document_id"`
	Status       string    `json:"status"`
	RetryCount   int       `json:"retry_count"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type chunkView struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Score    float32        `json:"score"`
	Metadata map[string]any `json:"metadata,omitempty"` // 仅 filename/kb_id（白名单）
}

// ---------- 统一执行包装：业务 + 审计投递 ----------

// run 执行 handler 业务并投递审计（异步，不阻塞）。授权检查由网关层完成（-32001）；
// 此处仅防御性返回 isError 结果。参数 JSON 用于审计（Submit 内截断）。
func (t *tools) run(ctx context.Context, toolName string, args map[string]any, fn func() (any, error)) (*mcpgo.CallToolResult, error) {
	start := time.Now()
	argsJSON, _ := json.Marshal(args)

	result, err := fn()
	if kc := KeyCtxFrom(ctx); kc != nil && t.audit != nil {
		status, errMsg := "success", ""
		if err != nil {
			status, errMsg = "error", err.Error() // 审计保留详情（内部可查）
		}
		t.audit.Submit(store.AuditLog{
			APIKeyID:     kc.KeyID,
			ToolName:     toolName,
			Params:       string(argsJSON),
			Status:       status,
			ErrorMessage: errMsg,
			DurationMS:   time.Since(start).Milliseconds(),
		})
	}
	if err != nil {
		// 展示消息：业务/校验错误原样；内部错误（引擎/存储）统一通用消息，防细节泄漏（安全审查 MEDIUM）
		msg := "工具执行失败，请稍后重试"
		switch e := err.(type) {
		case *PermissionError:
			msg = e.Message()
		case *ShowError:
			msg = e.message
		default:
			slog.Error("MCP tool 执行失败", "tool", toolName, "err", err)
		}
		return &mcpgo.CallToolResult{
			Content: []mcpgo.Content{mcpgo.NewTextContent(msg)},
			IsError: true,
		}, nil
	}
	return okResult(result)
}

// okResult 成功结果：StructuredContent + 等价文本
func okResult(v any) (*mcpgo.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return &mcpgo.CallToolResult{
			Content: []mcpgo.Content{mcpgo.NewTextContent("序列化结果失败")},
			IsError: true,
		}, nil
	}
	return &mcpgo.CallToolResult{
		Content:           []mcpgo.Content{mcpgo.NewTextContent(string(b))},
		StructuredContent: v,
	}, nil
}

// isErrorResult 业务失败结果（消息统一，不泄露存在性）
func isErrorResult(err error) (*mcpgo.CallToolResult, error) {
	return &mcpgo.CallToolResult{
		Content: []mcpgo.Content{mcpgo.NewTextContent(err.Error())},
		IsError: true,
	}, nil
}

// ---------- 参数提取 helper ----------

// argMap 将 CallToolRequest 的 Arguments（any）断言为 map；非 map 返回空
func argMap(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return v
}

func argInt(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

// kbFilter 构造检索过滤条件：单值 / 多值 / 不过滤（与 rag 包语义一致）
func kbFilter(kbIDs []string) map[string]any {
	switch len(kbIDs) {
	case 0:
		return nil
	case 1:
		return map[string]any{"kb_id": kbIDs[0]}
	default:
		return map[string]any{"kb_id": kbIDs}
	}
}

// ---------- 6 个 Tool handler ----------

// handleListKBs list_knowledge_bases：按凭据范围返回可访问知识库
func (t *tools) handleListKBs(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolListKBs, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		if kc == nil {
			return nil, errors.New("未认证")
		}
		var kbs []store.KnowledgeBase
		var err error
		if kc.Scope.All {
			kbs, err = t.st.ListAllKBs(ctx)
		} else {
			kbs, err = t.st.ListKBsByIDs(ctx, kc.Scope.IDs)
		}
		if err != nil {
			return nil, err
		}
		views := make([]kbView, 0, len(kbs))
		for _, kb := range kbs {
			views = append(views, kbView{ID: kb.ID, Name: kb.Name, Description: kb.Description,
				Strategy: kb.Strategy, CreatedAt: kb.CreatedAt, UpdatedAt: kb.UpdatedAt})
		}
		return views, nil
	})
}

// handleGetKB get_knowledge_base：详情；不存在/越权统一消息（网关层已拦截越权，此处防御）
func (t *tools) handleGetKB(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolGetKB, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		kbID := argString(argMap(req.Params.Arguments), "kb_id")
		kb, err := t.st.GetKB(ctx, kbID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, NewKBForbidden()
			}
			return nil, err
		}
		if kc == nil || !kc.Scope.CanAccess(kbID) {
			return nil, NewKBForbidden()
		}
		return kbView{ID: kb.ID, Name: kb.Name, Description: kb.Description,
			Strategy: kb.Strategy, CreatedAt: kb.CreatedAt, UpdatedAt: kb.UpdatedAt}, nil
	})
}

// handleRetrieve retrieve：纯检索（复用检索/Reranker 链路）
func (t *tools) handleRetrieve(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolRetrieve, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		args := argMap(req.Params.Arguments)
		query := argString(args, "query")
		kbIDs, err := kc.Scope.Resolve(argString(args, "kb_id"))
		if err != nil {
			return nil, err // 网关层已拦截；防御
		}
		rt := t.rt()
		if rt == nil {
			return nil, errors.New("检索器未初始化")
		}
		results, err := rt.Search(ctx, retriever.RetrieveRequest{
			Query:  query,
			TopK:   argInt(args, "top_k"),
			Filter: kbFilter(kbIDs),
		})
		if err != nil {
			slog.Warn("MCP retrieve 检索失败", "err", err)
			return nil, err
		}
		views := make([]chunkView, 0, len(results))
		for _, r := range results {
			views = append(views, chunkView{
				ID:       r.ID,
				Content:  r.Content,
				Score:    r.Score,
				Metadata: safeMetadata(r.Metadata),
			})
		}
		return views, nil
	})
}

// handleAsk ask：RAG 问答。不提供 thinking，不暴露内部推理（spec F2.4）；
// session_id 透传现有 engine.Ask 的会话语义（缺省新 uuid，不为 MCP 新建 session 系统）。
func (t *tools) handleAsk(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolAsk, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		args := argMap(req.Params.Arguments)
		question := argString(args, "question")
		if question == "" {
			return nil, NewShowError("question 不能为空")
		}
		kbIDs, err := kc.Scope.Resolve(argString(args, "kb_id"))
		if err != nil {
			return nil, err
		}
		eng := t.engine()
		if eng == nil {
			return nil, errors.New("RAG 引擎未初始化")
		}
		sessionID := argString(args, "session_id")
		if sessionID == "" {
			sessionID = uuid.New().String()
		}
		// 统一绑定 KeyID 前缀：所有 MCP ask 会话与调用方 Key 隔离，
		// 防跨 Key 复用同名 session_id 串读会话历史（安全审查 MEDIUM）
		sessionID = kc.KeyID + ":" + sessionID
		var snap *config.Config
		if t.cfg != nil {
			snap = t.cfg.Get()
		}
		askOpts := []rag.AskOption{
			rag.WithConfigSnapshot(snap),
			rag.WithThinking(false), // 不请求思考链路，不暴露内部推理
		}
		if len(kbIDs) == 0 {
			// scope=all 且未指定 kb_id：不过滤（系统级"不限"语义）
		} else if len(kbIDs) == 1 {
			askOpts = append(askOpts, rag.WithKBID(kbIDs[0]))
		} else {
			askOpts = append(askOpts, rag.WithKBIDs(kbIDs))
		}
		result, err := eng.Ask(ctx, sessionID, question, askOpts...)
		if err != nil {
			slog.Warn("MCP ask 问答失败", "err", err)
			return nil, err
		}
		return map[string]any{"answer": result.Answer, "sources": result.Sources}, nil
	})
}

// handleListDocs list_documents：指定 KB 或可访问全部 KB 的文档
func (t *tools) handleListDocs(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolListDocs, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		kbID := argString(argMap(req.Params.Arguments), "kb_id")
		if kbID != "" {
			if !kc.Scope.CanAccess(kbID) {
				return nil, NewKBForbidden()
			}
			return toDocViews(t.st.ListDocuments(ctx, kbID))
		}
		var kbs []store.KnowledgeBase
		var err error
		if kc.Scope.All {
			kbs, err = t.st.ListAllKBs(ctx)
		} else {
			kbs, err = t.st.ListKBsByIDs(ctx, kc.Scope.IDs)
		}
		if err != nil {
			return nil, err
		}
		var docs []docView
		for _, kb := range kbs {
			views, err := toDocViews(t.st.ListDocuments(ctx, kb.ID))
			if err != nil {
				return nil, err
			}
			docs = append(docs, views...)
		}
		return docs, nil
	})
}

// handleGetTask get_task：先查任务，按其关联知识库校验权限（spec F2.6/F8）；
// 不存在/越权统一「任务不存在」（网关层已拦截，此处防御）
func (t *tools) handleGetTask(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return t.run(ctx, ToolGetTask, argMap(req.Params.Arguments), func() (any, error) {
		kc := KeyCtxFrom(ctx)
		task, err := t.st.GetTask(ctx, argString(argMap(req.Params.Arguments), "task_id"))
		if err != nil || task == nil {
			return nil, NewTaskForbidden()
		}
		if !kc.Scope.CanAccess(task.KBID) {
			return nil, NewTaskForbidden()
		}
		return taskView{ID: task.ID, KBID: task.KBID, DocumentID: task.DocumentID,
			Status: task.Status, RetryCount: task.RetryCount, ErrorMessage: task.ErrorMessage,
			CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}, nil
	})
}

// ---------- 转换 helper ----------

func toDocViews(docs []store.Document, err error) ([]docView, error) {
	if err != nil {
		return nil, err
	}
	views := make([]docView, 0, len(docs))
	for _, d := range docs {
		views = append(views, docView{ID: d.ID, KBID: d.KBID, Filename: d.Filename,
			Format: d.Format, Size: d.Size, Status: d.Status, CreatedAt: d.CreatedAt})
	}
	return views, nil
}

// safeMetadata 仅暴露 filename/kb_id 元数据（避免泄露完整 payload）
func safeMetadata(m map[string]any) map[string]any {
	out := map[string]any{}
	if v, ok := m["filename"]; ok {
		out["filename"] = v
	}
	if v, ok := m["kb_id"]; ok {
		out["kb_id"] = v
	}
	return out
}
