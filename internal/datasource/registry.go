package datasource

import "sync"

// Registry 数据源注册中心：支持运行时动态注册、按名获取、列出全部数据源。
// 仿 internal/loader/registry.go 的注册表模式。并发安全（运行时动态注册 + 多请求读取）。
type Registry interface {
	// Register 注册数据源（重名直接覆盖，便于动态更新）
	Register(s Source)
	// Get 按名称获取数据源
	Get(name string) (Source, bool)
	// List 返回全部已注册数据源
	List() []Source
	// Names 返回全部已注册数据源名称
	Names() []string
}

type defaultRegistry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

// NewRegistry 创建数据源注册中心
func NewRegistry() Registry {
	return &defaultRegistry{
		sources: make(map[string]Source),
	}
}

func (r *defaultRegistry) Register(s Source) {
	if s == nil || s.Name() == "" {
		return
	}
	r.mu.Lock()
	r.sources[s.Name()] = s
	r.mu.Unlock()
}

func (r *defaultRegistry) Get(name string) (Source, bool) {
	r.mu.RLock()
	s, ok := r.sources[name]
	r.mu.RUnlock()
	return s, ok
}

func (r *defaultRegistry) List() []Source {
	r.mu.RLock()
	out := make([]Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	r.mu.RUnlock()
	return out
}

func (r *defaultRegistry) Names() []string {
	r.mu.RLock()
	out := make([]string, 0, len(r.sources))
	for name := range r.sources {
		out = append(out, name)
	}
	r.mu.RUnlock()
	return out
}
