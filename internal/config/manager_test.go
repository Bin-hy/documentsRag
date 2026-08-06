package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testConfig() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func TestConfigManagerGet(t *testing.T) {
	cfg := testConfig()
	m := NewConfigManager("", cfg)
	if m.Get() != cfg {
		t.Error("Get 应返回初始配置")
	}
}

func TestConfigManagerUpdateSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := testConfig()
	m := NewConfigManager(path, cfg)

	newCfg := testConfig()
	newCfg.LLM.Temperature = 0.2

	rebuilt := false
	err := m.Update(newCfg, func(c *Config) error {
		rebuilt = true
		return nil
	})
	if err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if !rebuilt {
		t.Error("rebuild 应被调用")
	}
	if m.Get() != newCfg {
		t.Error("Update 后 Get 应返回新配置")
	}
	// 文件已写入
	if _, err := os.Stat(path); err != nil {
		t.Errorf("配置文件应已写入: %v", err)
	}
}

func TestConfigManagerRebuildFail(t *testing.T) {
	cfg := testConfig()
	m := NewConfigManager("", cfg)

	newCfg := testConfig()
	newCfg.LLM.Temperature = 0.5

	err := m.Update(newCfg, func(c *Config) error {
		return errors.New("构建失败")
	})
	if err == nil {
		t.Fatal("rebuild 失败应返回 error")
	}
	if m.Get() != cfg {
		t.Error("rebuild 失败不应替换配置")
	}
}

func TestConfigManagerInvalidConfig(t *testing.T) {
	cfg := testConfig()
	m := NewConfigManager("", cfg)

	bad := testConfig()
	bad.LLM.Temperature = 5 // 超范围
	if err := m.Update(bad, nil); err == nil {
		t.Error("非法 temperature 应被拒绝")
	}
	if m.Get() != cfg {
		t.Error("非法配置不应替换")
	}
}

func TestConfigManagerConcurrentGet(t *testing.T) {
	cfg := testConfig()
	m := NewConfigManager("", cfg)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := m.Get()
			if c == nil {
				t.Error("并发 Get 不应返回 nil")
			}
			// 快照完整性：任一时刻读取的配置各字段齐全（applyDefaults 后非零）
			if c.RAG.TopK <= 0 {
				t.Error("并发 Get 快照不完整")
			}
		}()
	}
	wg.Wait()
}

func TestAtomicWriteYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml") // 子目录不存在
	cfg := testConfig()
	// 目录不存在时应报错（不 panic）
	if err := atomicWriteYAML(path, cfg); err == nil {
		t.Error("父目录不存在时应报错")
	}
	// 正常路径
	path2 := filepath.Join(t.TempDir(), "config.yaml")
	if err := atomicWriteYAML(path2, cfg); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, err := os.ReadFile(path2)
	if err != nil || len(data) == 0 {
		t.Errorf("文件应有内容: %v", err)
	}
}
