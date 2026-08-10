package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 约定：config.local.yaml 自动合并（local 字段覆盖主配置，未出现字段保留）
func TestLoadConfigLocalOverride(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config.yaml")
	local := filepath.Join(dir, "config.local.yaml")

	if err := os.WriteFile(main, []byte(`
llm:
  model: gpt-4o
  api_key: sk-main
web_search:
  provider: bocha
  api_key: ""
retriever:
  top_k: 10
`), 0o644); err != nil {
		t.Fatalf("写主配置失败: %v", err)
	}
	if err := os.WriteFile(local, []byte(`
llm:
  model: qwen2.5:3b
web_search:
  api_key: sk-local-key
`), 0o644); err != nil {
		t.Fatalf("写 local 配置失败: %v", err)
	}

	cfg, err := LoadConfig(main)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	// local 覆盖
	if cfg.LLM.Model != "qwen2.5:3b" {
		t.Errorf("llm.model 应被 local 覆盖: %q", cfg.LLM.Model)
	}
	if cfg.WebSearch.APIKey != "sk-local-key" {
		t.Errorf("web_search.api_key 应被 local 覆盖: %q", cfg.WebSearch.APIKey)
	}
	// 主配置保留
	if cfg.LLM.APIKey != "sk-main" {
		t.Errorf("local 未出现的 llm.api_key 应保留主配置值: %q", cfg.LLM.APIKey)
	}
	if cfg.Retriever.TopK != 10 {
		t.Errorf("local 未出现的 retriever.top_k 应保留: %d", cfg.Retriever.TopK)
	}
	if cfg.WebSearch.Provider != "bocha" {
		t.Errorf("local 未出现的 provider 应保留: %q", cfg.WebSearch.Provider)
	}
}

// 显式指定 local 文件本身时不重复合并（避免 local.local）
func TestLoadConfigLocalExplicitNoDoubleMerge(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "config.local.yaml")
	if err := os.WriteFile(local, []byte("llm:\n  model: qwen\n"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	cfg, err := LoadConfig(local)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.LLM.Model != "qwen" {
		t.Errorf("显式加载 local 文件应直接生效: %q", cfg.LLM.Model)
	}
}

// 三方登录配置校验：默认值 + 静态校验规则
// MCP Server 默认值：Path=/mcp、AuditParamLimit=2000、Enabled=false（安全默认 D7）
func TestMCPConfigDefaults(t *testing.T) {
	c := &Config{}
	c.applyDefaults()

	if c.Server.MCP.Enabled {
		t.Error("MCP Enabled 默认应为 false（显式开启才挂载 /mcp）")
	}
	if c.Server.MCP.Path != "/mcp" {
		t.Errorf("MCP Path 默认应为 /mcp，实际 %q", c.Server.MCP.Path)
	}
	if c.Server.MCP.AuditParamLimit != 2000 {
		t.Errorf("MCP AuditParamLimit 默认应为 2000，实际 %d", c.Server.MCP.AuditParamLimit)
	}
}

// MCP 显式配置生效：enabled=true / 自定义 path / 自定义截断长度
func TestMCPConfigExplicit(t *testing.T) {
	c := &Config{Server: ServerConfig{MCP: MCPConfig{
		Enabled:         true,
		Path:            "/custom-mcp",
		AuditParamLimit: 500,
	}}}
	c.applyDefaults()

	if !c.Server.MCP.Enabled {
		t.Error("显式 enabled=true 应生效")
	}
	if c.Server.MCP.Path != "/custom-mcp" {
		t.Errorf("自定义 Path 应保留，实际 %q", c.Server.MCP.Path)
	}
	if c.Server.MCP.AuditParamLimit != 500 {
		t.Errorf("自定义 AuditParamLimit 应保留，实际 %d", c.Server.MCP.AuditParamLimit)
	}
}

func TestOIDCValidate(t *testing.T) { // 合法配置：oidc 默认 type/scope + github oauth2
	valid := &Config{OIDC: OIDCConfig{
		Enabled:   true,
		PublicURL: "https://rag.example.com",
		Providers: []ProviderConfig{
			{Name: "github", Type: ProviderTypeOAuth2, ClientID: "cid", ClientSecret: "csec"},
			{Name: "company", Type: ProviderTypeOIDC, ClientID: "cid2", ClientSecret: "csec2", Issuer: "https://sso.example.com"},
		},
	}}
	valid.applyDefaults()
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法配置应通过: %v", err)
	}
	if valid.OIDC.Providers[1].Scope[0] != "openid" {
		t.Errorf("oidc 默认 scope 应含 openid: %v", valid.OIDC.Providers[1].Scope)
	}
	if valid.OIDC.Providers[0].Scope[0] != "read:user" {
		t.Errorf("github 默认 scope 应为 read:user: %v", valid.OIDC.Providers[0].Scope)
	}

	cases := []struct {
		name   string
		mutate func(*Config)
		want   string // 期望错误子串
	}{
		{"enabled 缺 public_url", func(c *Config) { c.OIDC.PublicURL = "" }, "public_url"},
		{"重复 Name", func(c *Config) { c.OIDC.Providers[0].Name = "company" }, "重复"},
		{"Name 非法字符", func(c *Config) { c.OIDC.Providers[0].Name = "a/b" }, "非法字符"},
		{"oidc 缺 issuer", func(c *Config) { c.OIDC.Providers[1].Issuer = "" }, "issuer"},
		{"oauth2 非 github", func(c *Config) { c.OIDC.Providers[0].Type = ProviderTypeOAuth2; c.OIDC.Providers[0].Name = "gitlab" }, "仅支持内置 github"},
		{"type 非法", func(c *Config) { c.OIDC.Providers[0].Type = "saml" }, "type 非法"},
		{"缺 client_id", func(c *Config) { c.OIDC.Providers[1].ClientID = "" }, "client_id"},
		{"缺 client_secret", func(c *Config) { c.OIDC.Providers[1].ClientSecret = "" }, "client_secret"},
		{"redirect_url 非法", func(c *Config) { c.OIDC.Providers[0].RedirectURL = "not-a-url" }, "redirect_url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := *valid
			providers := make([]ProviderConfig, len(valid.OIDC.Providers))
			copy(providers, valid.OIDC.Providers)
			cp.OIDC.Providers = providers
			tc.mutate(&cp)
			cp.applyDefaults()
			err := cp.Validate()
			if err == nil {
				t.Fatalf("应校验失败: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 %q 应包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// oidc 未启用时缺 public_url 不报错（现有配置零改动兼容）
func TestOIDCDisabledNoValidation(t *testing.T) {
	cfg := &Config{OIDC: OIDCConfig{Enabled: false}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("未启用时不应校验: %v", err)
	}
}
