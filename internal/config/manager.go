package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// ConfigManager 运行时配置管理器：原子快照 + 持久化 + 回滚。
// 核心：请求级快照——每请求 Get() 拿一份不可变快照贯穿整个 pipeline；
// Update() 原子替换，旧快照对新请求不可见，已在执行的请求持旧快照不受影响。
type ConfigManager struct {
	path string
	mu   sync.Mutex // 保护 Update 串行化（校验→试构建→写文件→替换）
	cur  atomic.Pointer[Config]
	prev *Config // 上一份有效快照（回滚/诊断用）
}

// NewConfigManager 创建配置管理器，持有初始配置快照
func NewConfigManager(path string, cfg *Config) *ConfigManager {
	m := &ConfigManager{path: path}
	m.cur.Store(cfg)
	return m
}

// Get 返回当前配置快照（请求级使用，只读；更新仅影响后续 Get）
func (m *ConfigManager) Get() *Config {
	return m.cur.Load()
}

// Current 返回当前配置快照（只读查询，别名 Get）
func (m *ConfigManager) Current() *Config {
	return m.cur.Load()
}

// Update 原子更新配置：
// 1. 校验 newCfg；2. rebuild 试构建新运行时组件（失败不替换）；3. 写配置文件（原子写）；4. 原子替换。
// rebuild 返回 nil 表示新组件构建成功；失败返回 error（调用方保持旧配置）。
func (m *ConfigManager) Update(newCfg *Config, rebuild func(*Config) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if newCfg == nil {
		return fmt.Errorf("新配置为空")
	}
	// 校验
	if err := ValidateConfig(newCfg); err != nil {
		return err
	}
	// 试构建新组件（不替换旧组件）
	if rebuild != nil {
		if err := rebuild(newCfg); err != nil {
			return fmt.Errorf("新配置组件构建失败，已回滚: %w", err)
		}
	}
	// 持久化写文件（原子写：临时文件 + rename）
	if m.path != "" {
		if err := atomicWriteYAML(m.path, newCfg); err != nil {
			return fmt.Errorf("写入配置文件失败: %w", err)
		}
	}
	// 原子替换
	old := m.cur.Load()
	m.prev = old
	m.cur.Store(newCfg)
	return nil
}

// Path 配置文件路径（测试/诊断用）
func (m *ConfigManager) Path() string { return m.path }

// atomicWriteYAML 原子写：临时文件 + rename，避免写一半
func atomicWriteYAML(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后无副作用

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ValidateConfig 校验配置整体合法性（可修改字段的枚举/数值范围/策略组合）
func ValidateConfig(cfg *Config) error {
	// LLM 温度范围
	if cfg.LLM.Temperature < 0 || cfg.LLM.Temperature > 2 {
		return fmt.Errorf("LLM temperature 超出范围 [0,2]: %v", cfg.LLM.Temperature)
	}
	// Retriever top_k 范围
	if cfg.Retriever.TopK < 1 || cfg.Retriever.TopK > 50 {
		return fmt.Errorf("Retriever top_k 超出范围 [1,50]: %d", cfg.Retriever.TopK)
	}
	// 融合权重非负
	if cfg.Retriever.VectorWeight < 0 || cfg.Retriever.BM25Weight < 0 {
		return fmt.Errorf("融合权重不能为负: vector=%v bm25=%v", cfg.Retriever.VectorWeight, cfg.Retriever.BM25Weight)
	}
	// 算法要求：向量权重 + BM25 权重之和必须为 1（RRF 融合比例）
	const weightTolerance = 0.001
	if math.Abs(float64(cfg.Retriever.VectorWeight)+float64(cfg.Retriever.BM25Weight)-1.0) > weightTolerance {
		return fmt.Errorf("算法要求向量权重 + BM25 权重之和必须为 1，当前 vector=%v + bm25=%v = %v",
			cfg.Retriever.VectorWeight, cfg.Retriever.BM25Weight, cfg.Retriever.VectorWeight+cfg.Retriever.BM25Weight)
	}
	// 策略组合校验（复用 ValidateStrategy 语义——在 rag 包；这里做基础枚举校验）
	if err := validateStrategyEnums(cfg.RAG.Strategy); err != nil {
		return err
	}
	// Loader 阈值
	if cfg.Loader.MinReadableChars < 0 {
		return fmt.Errorf("Loader min_readable_chars 不能为负: %d", cfg.Loader.MinReadableChars)
	}
	// MCP path 必须以 / 开头（端点路径）；空表示使用默认 /mcp
	if cfg.Server.MCP.Path != "" && !strings.HasPrefix(cfg.Server.MCP.Path, "/") {
		return fmt.Errorf("server.mcp.path 必须以 / 开头: %q", cfg.Server.MCP.Path)
	}
	return nil
}

// validateStrategyEnums 校验 strategy 枚举值（与 rag.ValidateStrategy 一致的枚举；组合校验在 rag 层）
func validateStrategyEnums(s StrategyConfig) error {
	for name, v := range map[string]string{
		"query": s.Query, "fusion": s.Fusion, "decomposition": s.Decomposition,
		"step_back": s.StepBack, "hyde": s.HyDE, "routing": s.Routing,
	} {
		if v == "" {
			continue
		}
		switch name {
		case "query":
			if v != "single" && v != "multi" {
				return fmt.Errorf("非法 strategy.query=%q（应为 single/multi）", v)
			}
		case "fusion":
			if v != "rrf" && v != "none" {
				return fmt.Errorf("非法 strategy.fusion=%q（应为 rrf/none）", v)
			}
		case "decomposition":
			if v != "off" && v != "parallel" && v != "sequential" {
				return fmt.Errorf("非法 strategy.decomposition=%q", v)
			}
		case "step_back":
			if v != "off" && v != "on" {
				return fmt.Errorf("非法 strategy.step_back=%q", v)
			}
		case "hyde":
			if v != "off" && v != "on" {
				return fmt.Errorf("非法 strategy.hyde=%q", v)
			}
		case "routing":
			if v != "off" && v != "auto" {
				return fmt.Errorf("非法 strategy.routing=%q", v)
			}
		}
	}
	return nil
}
