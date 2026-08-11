package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/rag"
	"github.com/Bin-hy/bin-rag/internal/retriever"
	"github.com/Bin-hy/bin-rag/internal/store"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Dependencies MCP 层依赖（同进程嵌入，复用现有 Store/Engine/Retriever/Config，spec N1）
type Dependencies struct {
	Config config.ServerConfig
	Store  store.Store
	Engine func() rag.Engine          // 复用 app 的 EngineProvider（热重载）
	RT     func() retriever.Retriever // 复用 app 的检索器 provider（热重载）
	CfgMgr *config.ConfigManager      // 配置快照（热重载一致性）
	Audit  *AuditSink                 // 异步审计（app 装配并管理生命周期）
}

// NewHandler 创建 MCP HTTP handler。
// 请求链路：认证（HTTP 401）→ 授权网关层（tools/call 越权 -32001）→ mcp-go streamable HTTP。
func NewHandler(deps Dependencies) http.Handler {
	t := &tools{
		st:     deps.Store,
		engine: deps.Engine,
		rt:     deps.RT,
		cfg:    deps.CfgMgr,
		audit:  deps.Audit,
	}
	ms := mcpserver.NewMCPServer("BinRag MCP", "1.0.0")
	t.register(ms)
	return &gateway{
		st:   deps.Store,
		next: mcpserver.NewStreamableHTTPServer(ms),
	}
}

// gateway 认证 + 授权拦截层。
//
// 探针结论（task T1）：mcp-go v0.57.0 的 tools/call handler 返回 error 固定映射 -32603，
// 无法经 handler/middleware 返回自定义错误码。因此授权失败（Tool/KB/Task 越权）在此层
// 解析 JSON-RPC body 后直接构造 -32001 响应（spec F4/F5/F8）。
type gateway struct {
	st   store.Store
	next http.Handler
}

// ServeHTTP 认证 → 授权 → 转发（注入 keyCtx）
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kc, ok := authenticate(w, r, g.st)
	if !ok {
		return // HTTP 401 已写
	}

	// 用户 MCP 凭据：按 owner 解析实际知识库范围（spec F9）——
	// scope=all → 仅自己的知识库；allowlist → 白名单 ∩ 自己的知识库；无 → 空
	if kc.OwnerID != "" {
		if err := resolveOwnerScope(r.Context(), g.st, kc); err != nil {
			slog.Warn("MCP owner 知识库范围解析失败", "key", kc.KeyID, "err", err)
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body)) // 恢复 body 供 mcp-go 读取

	if errResp := g.authorize(r, kc, body); errResp != nil {
		writeJSONRPCError(w, *errResp)
		return
	}

	g.next.ServeHTTP(w, r.WithContext(ctxWithKeyCtx(r.Context(), kc)))
}

// resolveOwnerScope 把用户凭据的配置范围解析为实际可访问范围（与用户归属知识库取交集）。
// 系统级 Key（OwnerID 空）不进入此逻辑（行为不变，D7）。
func resolveOwnerScope(ctx context.Context, st store.Store, kc *keyCtx) error {
	ownerKBs, err := st.ListKBsByOwner(ctx, kc.OwnerID)
	if err != nil {
		return err
	}
	ownerIDs := make([]string, 0, len(ownerKBs))
	for _, kb := range ownerKBs {
		ownerIDs = append(ownerIDs, kb.ID)
	}
	switch {
	case kc.Scope.All:
		kc.Scope = KBPermission{IDs: ownerIDs} // 用户 all = 自己的知识库
	case len(kc.Scope.IDs) > 0:
		kc.Scope = KBPermission{IDs: intersect(kc.Scope.IDs, ownerIDs)} // 白名单 ∩ 自己的
	default:
		kc.Scope = KBPermission{} // 无范围
	}
	return nil
}

// intersect 返回 a 与 b 的交集（保序，按 a）
func intersect(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, v := range b {
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, v := range a {
		if _, ok := set[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

// authorize 对 tools/call 做授权检查（Tool 白名单 + 知识库/任务资源权限）。
// 非 tools/call 或无法解析的请求放行（交由 mcp-go 处理）；越权返回 -32001 响应。
func (g *gateway) authorize(r *http.Request, kc *keyCtx, body []byte) *mcpgo.JSONRPCError {
	if r.Method != http.MethodPost {
		return nil
	}
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil // 不可解析：交给 mcp-go 返回解析错误
	}
	if msg.Method != string(mcpgo.MethodToolsCall) {
		return nil // initialize/tools/list 等不需要调用级授权
	}

	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil || params.Name == "" {
		return nil
	}
	idAny := rawID(msg.ID)

	// Tool 白名单（spec F4）：空白名单（含历史 Key）无任何 MCP Tool 权限
	if !ToolAllowed(kc.Tools, params.Name) {
		return permissionErr(idAny, NewToolForbidden())
	}

	// 知识库 / 任务资源权限（spec F5/F8）；越权与不存在统一消息（不泄露存在性，spec N2）
	switch params.Name {
	case ToolGetKB:
		if !kc.Scope.CanAccess(argString(params.Arguments, "kb_id")) {
			return permissionErr(idAny, NewKBForbidden())
		}
	case ToolRetrieve, ToolAsk, ToolListDocs:
		if _, err := kc.Scope.Resolve(argString(params.Arguments, "kb_id")); err != nil {
			return permissionErr(idAny, err)
		}
	case ToolGetTask:
		task, err := g.st.GetTask(r.Context(), argString(params.Arguments, "task_id"))
		if err != nil || task == nil || !kc.Scope.CanAccess(task.KBID) {
			return permissionErr(idAny, NewTaskForbidden())
		}
	}
	return nil
}

// rawID 将 JSON-RPC id（RawMessage）解析为原生 any；解析失败返回 nil
func rawID(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// permissionErr 构造 -32001 错误响应（*PermissionError 或带 Message 的错误）
func permissionErr(id any, err error) *mcpgo.JSONRPCError {
	if pe, ok := err.(*PermissionError); ok {
		e := pe.ToJSONRPCError(id)
		return &e
	}
	pe := &PermissionError{message: err.Error()}
	e := pe.ToJSONRPCError(id)
	return &e
}

// writeJSONRPCError 写授权失败响应：HTTP 200 + JSON-RPC error -32001（plan D4：
// 认证失败 HTTP 401，授权失败在 JSON-RPC error 层表达，MCP 协议层无 403 概念）
func writeJSONRPCError(w http.ResponseWriter, e mcpgo.JSONRPCError) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e)
}
