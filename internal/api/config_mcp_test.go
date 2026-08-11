package api

import (
	"encoding/json"
	"testing"
)

// bootstrap Key 可修改 server.mcp；非 bootstrap 403；GET /config 返回 mutable.mcp
func TestConfigMCPUpdate(t *testing.T) {
	env, bootstrapKey := newConfigTestEnv(t)

	// GET /config 返回 mutable.mcp（默认值：applyDefaults 后 path=/mcp、limit=2000、enabled=false）
	w := doReq(t, env.router, "GET", "/api/v1/config", nil, bootstrapKey)
	if w.Code != 200 {
		t.Fatalf("GET /config 失败: %d", w.Code)
	}
	var resp struct {
		Data struct {
			Mutable struct {
				MCP struct {
					Enabled         bool   `json:"enabled"`
					Path            string `json:"path"`
					AuditParamLimit int    `json:"audit_param_limit"`
				} `json:"mcp"`
			} `json:"mutable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Data.Mutable.MCP.Enabled {
		t.Errorf("默认 mcp.enabled 应为 false，实际 %+v", resp.Data.Mutable.MCP)
	}

	// bootstrap Key 修改 mcp
	w = doReq(t, env.router, "PUT", "/api/v1/config", map[string]any{
		"mcp": map[string]any{"enabled": true, "path": "/custom-mcp", "audit_param_limit": 500},
	}, bootstrapKey)
	if w.Code != 200 {
		t.Fatalf("bootstrap 修改 mcp 失败: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Data.Mutable.MCP.Enabled || resp.Data.Mutable.MCP.Path != "/custom-mcp" || resp.Data.Mutable.MCP.AuditParamLimit != 500 {
		t.Errorf("修改后 mutable.mcp 未生效: %+v", resp.Data.Mutable.MCP)
	}

	// 非 bootstrap Key（普通 API Key）修改 → 403
	w = doReq(t, env.router, "PUT", "/api/v1/config", map[string]any{
		"mcp": map[string]any{"enabled": false},
	}, testAPIKey)
	if w.Code != 403 {
		t.Errorf("非 bootstrap 修改 mcp 应 403，实际 %d", w.Code)
	}

	// 非法 path（不以 / 开头）→ 400
	w = doReq(t, env.router, "PUT", "/api/v1/config", map[string]any{
		"mcp": map[string]any{"path": "bad-mcp"},
	}, bootstrapKey)
	if w.Code != 400 {
		t.Errorf("非法 mcp.path 应 400，实际 %d", w.Code)
	}
}
