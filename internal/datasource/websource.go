package datasource

import (
	"context"
	"fmt"

	"github.com/Bin-hy/bin-rag/internal/retriever"
)

// webSearchSource web 搜索数据源（占位）。
// 接口已就绪；具体搜索引擎实现（Tavily/Serper/Bing 等）后续接入后
// 将 Available 改为 true 即可被路由使用。
type webSearchSource struct{}

// NewWebSearchSource 创建 web 搜索占位数据源
func NewWebSearchSource() Source {
	return &webSearchSource{}
}

func (s *webSearchSource) Name() string    { return SourceWebSearch }
func (s *webSearchSource) Available() bool { return false }

func (s *webSearchSource) Search(ctx context.Context, req SearchRequest) ([]retriever.RetrieveResult, error) {
	return nil, fmt.Errorf("web_search 数据源未实现")
}
