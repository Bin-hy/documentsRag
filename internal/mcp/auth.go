package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Bin-hy/bin-rag/internal/store"
)

// keyCtxKey request context 中 keyCtx 的键
type keyCtxKey struct{}

// keyCtx 认证后的身份与权限（仅 Key ID 引用，不含 Secret/Authorization Token，spec F7）。
// 不含 bootstrap 标记：MCP 认证层不识别 bootstrap，一律按 Key 权限字段授权（plan D6）。
type keyCtx struct {
	KeyID string
	Tools []string // MCP Tool 白名单（空 = 无任何 MCP Tool 权限，spec F4/F6）
	Scope KBPermission
}

// ctxWithKeyCtx 将 keyCtx 写入 request context
func ctxWithKeyCtx(ctx context.Context, k *keyCtx) context.Context {
	return context.WithValue(ctx, keyCtxKey{}, k)
}

// KeyCtxFrom 读取 request context 中的 keyCtx；未认证返回 nil
func KeyCtxFrom(ctx context.Context) *keyCtx {
	if v, ok := ctx.Value(keyCtxKey{}).(*keyCtx); ok {
		return v
	}
	return nil
}

// keyLookup 认证所需的存储最小接口（真实 store.Store 满足；测试可用最小 fake）
type keyLookup interface {
	GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error)
}

// authenticate 校验 Authorization: Bearer <API Key>（复用现有 SHA-256 查库流程）。
// 缺失 / 无效 / 已停用 → 写 HTTP 401 并返回 false，不进入 JSON-RPC（spec F3）。
// 成功 → 解析 MCP 权限字段 → 返回 keyCtx。
func authenticate(w http.ResponseWriter, r *http.Request, st keyLookup) (*keyCtx, bool) {
	header := r.Header.Get("Authorization")
	var token string
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		token = header[7:]
	}
	if token == "" {
		writeUnauthorized(w)
		return nil, false
	}

	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	key, err := st.GetAPIKeyByHash(r.Context(), hash)
	if err != nil {
		slog.Warn("MCP API Key 校验出错", "err", err)
		writeUnauthorized(w)
		return nil, false
	}
	if key == nil || !key.Enabled {
		// 无效或已停用的 Key：统一 401（不泄露区分）
		writeUnauthorized(w)
		return nil, false
	}

	return &keyCtx{
		KeyID: key.ID,
		Tools: key.MCPTools,
		Scope: ParseScope(key.MCPKBScope, key.MCPKBIDs),
	}, true
}

// writeUnauthorized 认证失败响应：HTTP 401 + WWW-Authenticate（不进入 JSON-RPC 处理）
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"认证失败"}`))
}
