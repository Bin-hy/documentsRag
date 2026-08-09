package config

import (
	"os"
	"path/filepath"
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
