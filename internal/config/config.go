package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 项目配置
type Config struct {
	Embedder    EmbedderConfig    `yaml:"embedder"`
	VectorStore VectorStoreConfig `yaml:"vectorstore"`
	Chunker     ChunkerConfig     `yaml:"chunker"`
	Retriever   RetrieverConfig   `yaml:"retriever"`
	Reranker    RerankerConfig    `yaml:"reranker"`
	LLM         LLMConfig         `yaml:"llm"`
	RAG         RAGConfig         `yaml:"rag"`
	Postgres    PostgresConfig    `yaml:"postgres"`
	Server      ServerConfig      `yaml:"server"`
	Loader      LoaderConfig      `yaml:"loader"`
}

// LoaderConfig 文档加载器配置
// MinReadableChars: 全文最低可读文本量（中文+单词计数），低于该值判定为扫描件/空内容拒绝入库；0 表示禁用
// （阈值语义：默认 20，正常文档动辄数百，扫描件 PDF 图像指令通常 < 15）
type LoaderConfig struct {
	MinReadableChars int `yaml:"min_readable_chars"`
}

// RetrieverConfig 检索器配置
type RetrieverConfig struct {
	TopK                  int     `yaml:"top_k"`
	RRFK                  int     `yaml:"rrf_k"`
	VectorWeight          float32 `yaml:"vector_weight"`
	BM25Weight            float32 `yaml:"bm25_weight"`
	EnableBM25            bool    `yaml:"enable_bm25"`
	EnableReranker        bool    `yaml:"enable_reranker"`
	MultiQueryConcurrency int     `yaml:"multi_query_concurrency"` // 多路检索并发上限（默认 3）
}

// RerankerConfig 重排序配置
type RerankerConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	TopN       int    `yaml:"top_n"`
	MaxRetries int    `yaml:"max_retries"`
	QPS        int    `yaml:"qps"`
}

// EmbedderConfig Embedding 提供者配置
type EmbedderConfig struct {
	Provider   string `yaml:"provider"`
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimension  int    `yaml:"dimension"`
	BatchSize  int    `yaml:"batch_size"`
	MaxRetries int    `yaml:"max_retries"`
	QPS        int    `yaml:"qps"`
}

// VectorStoreConfig 向量存储配置
type VectorStoreConfig struct {
	Host           string `yaml:"host"`
	CollectionName string `yaml:"collection_name"`
	Dimension      int    `yaml:"dimension"`
	Distance       string `yaml:"distance"`
}

// ChunkerConfig 分块器配置
type ChunkerConfig struct {
	Strategy     string `yaml:"strategy"`
	ChunkSize    int    `yaml:"chunk_size"`
	ChunkOverlap int    `yaml:"chunk_overlap"`
	HeadingLevel int    `yaml:"heading_level"`
}

// LLMConfig LLM 提供者配置（OpenAI 兼容接口）
type LLMConfig struct {
	BaseURL     string  `yaml:"base_url"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	Temperature float32 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
	MaxRetries  int     `yaml:"max_retries"`
	QPS         int     `yaml:"qps"`
	Timeout     int     `yaml:"timeout"` // 秒
}

// StrategyConfig 检索策略配置（三级覆盖：全局 config.yaml → 知识库 → 单次请求）
// 字段取值：
//
//	Query: single / multi
//	Fusion: rrf / none
//	Decomposition: off / parallel / sequential
//	StepBack: off / on
//	HyDE: off / on
//	Routing: off / auto
//
// 空字符串表示「未设置，继承低层级」（全局默认由 applyDefaults 兜底）
type StrategyConfig struct {
	Query         string `yaml:"query" json:"query,omitempty"`
	Fusion        string `yaml:"fusion" json:"fusion,omitempty"`
	Decomposition string `yaml:"decomposition" json:"decomposition,omitempty"`
	StepBack      string `yaml:"step_back" json:"step_back,omitempty"`
	HyDE          string `yaml:"hyde" json:"hyde,omitempty"`
	Routing       string `yaml:"routing" json:"routing,omitempty"`
}

// RAGConfig RAG 编排配置
type RAGConfig struct {
	TopK                      int            `yaml:"top_k"`
	MaxContextTokens          int            `yaml:"max_context_tokens"`
	MaxChunks                 int            `yaml:"max_chunks"`
	EnableRewrite             *bool          `yaml:"enable_rewrite"`
	MultiQueryEnabled         *bool          `yaml:"multi_query_enabled"`
	MultiQueryCount           int            `yaml:"multi_query_count"`
	MultiQueryConcurrency     int            `yaml:"multi_query_concurrency"`
	MultiQueryTemplatePath    string         `yaml:"multi_query_template_path"`
	DecompositionEnabled      *bool          `yaml:"decomposition_enabled"`
	DecompositionMode         string         `yaml:"decomposition_mode"`
	DecompositionMaxSub       int            `yaml:"decomposition_max_sub"`
	StepBackEnabled           *bool          `yaml:"step_back_enabled"`
	DecompositionTemplatePath string         `yaml:"decomposition_template_path"`
	StepBackTemplatePath      string         `yaml:"step_back_template_path"`
	RoutingEnabled            *bool          `yaml:"routing_enabled"`
	RoutingFallback           string         `yaml:"routing_fallback"`
	HyDEEnabled               *bool          `yaml:"hyde_enabled"`
	HyDESkipSimple            *bool          `yaml:"hyde_skip_simple"`
	RoutingTemplatePath       string         `yaml:"routing_template_path"`
	HyDETemplatePath          string         `yaml:"hyde_template_path"`
	Strategy                  StrategyConfig `yaml:"strategy"` // 全局默认策略
	HistoryCapacity           int            `yaml:"history_capacity"`
	HistoryLimit              int            `yaml:"history_limit"`
	SystemPromptPath          string         `yaml:"system_prompt_path"`
	ContextTemplatePath       string         `yaml:"context_template_path"`
	RewriteTemplatePath       string         `yaml:"rewrite_template_path"`
}

// RewriteEnabled 是否启用 Query 改写（nil 视为启用，即默认开启）
func (c RAGConfig) RewriteEnabled() bool {
	return c.EnableRewrite == nil || *c.EnableRewrite
}

// MultiQueryOn 是否启用多查询（nil 视为关闭，保守默认保持现状）
func (c RAGConfig) MultiQueryOn() bool {
	return c.MultiQueryEnabled != nil && *c.MultiQueryEnabled
}

// DecompositionOn 是否启用问题分解（nil 视为关闭）
func (c RAGConfig) DecompositionOn() bool {
	return c.DecompositionEnabled != nil && *c.DecompositionEnabled
}

// StepBackOn 是否启用回退查询（nil 视为关闭）
func (c RAGConfig) StepBackOn() bool {
	return c.StepBackEnabled != nil && *c.StepBackEnabled
}

// RoutingOn 是否启用 RAG 路由（nil 视为关闭）
func (c RAGConfig) RoutingOn() bool {
	return c.RoutingEnabled != nil && *c.RoutingEnabled
}

// HyDEOn 是否启用 HyDE 假设文档（nil 视为关闭）
func (c RAGConfig) HyDEOn() bool {
	return c.HyDEEnabled != nil && *c.HyDEEnabled
}

// HyDESkipSimpleOn 简单查询是否跳过 HyDE（nil 视为 true）
func (c RAGConfig) HyDESkipSimpleOn() bool {
	return c.HyDESkipSimple == nil || *c.HyDESkipSimple
}

// PostgresConfig 元数据存储配置
type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Port            int    `yaml:"port"`
	FileStorageDir  string `yaml:"file_storage_dir"`
	UploadMaxSizeMB int    `yaml:"upload_max_size_mb"`
	WorkerCount     int    `yaml:"worker_count"`
	TaskMaxRetries  int    `yaml:"task_max_retries"`
	AuthEnabled     bool   `yaml:"auth_enabled"`
	BootstrapAPIKey string `yaml:"bootstrap_api_key"` // 仅首次启动种子用
	RateLimitQPS    int    `yaml:"rate_limit_qps"`    // 0 表示不限制
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("BINRAG_CONFIG")
	}
	if path == "" {
		path = "./configs/config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Embedder.BatchSize <= 0 {
		c.Embedder.BatchSize = 100
	}
	if c.Embedder.MaxRetries <= 0 {
		c.Embedder.MaxRetries = 3
	}
	if c.Embedder.QPS <= 0 {
		c.Embedder.QPS = 10
	}
	if c.Embedder.Dimension <= 0 {
		c.Embedder.Dimension = 1536
	}
	if c.VectorStore.Distance == "" {
		c.VectorStore.Distance = "cosine"
	}
	if c.VectorStore.Dimension <= 0 {
		c.VectorStore.Dimension = c.Embedder.Dimension
	}
	if c.Chunker.ChunkSize <= 0 {
		c.Chunker.ChunkSize = 512
	}
	if c.Chunker.ChunkOverlap < 0 {
		c.Chunker.ChunkOverlap = 50
	}
	if c.Chunker.HeadingLevel <= 0 {
		c.Chunker.HeadingLevel = 2
	}
	// Retriever 默认值
	if c.Retriever.TopK <= 0 {
		c.Retriever.TopK = 10
	}
	if c.Retriever.RRFK <= 0 {
		c.Retriever.RRFK = 60
	}
	if c.Retriever.VectorWeight <= 0 {
		c.Retriever.VectorWeight = 0.7
	}
	if c.Retriever.BM25Weight <= 0 {
		c.Retriever.BM25Weight = 0.3
	}
	if c.Retriever.MultiQueryConcurrency <= 0 {
		c.Retriever.MultiQueryConcurrency = 3
	}
	// Reranker 默认值
	if c.Reranker.TopN <= 0 {
		c.Reranker.TopN = 5
	}
	if c.Reranker.MaxRetries <= 0 {
		c.Reranker.MaxRetries = 3
	}
	if c.Reranker.QPS <= 0 {
		c.Reranker.QPS = 10
	}
	// LLM 默认值
	if c.LLM.MaxRetries <= 0 {
		c.LLM.MaxRetries = 3
	}
	if c.LLM.QPS <= 0 {
		c.LLM.QPS = 10
	}
	if c.LLM.Timeout <= 0 {
		c.LLM.Timeout = 60
	}
	if c.LLM.Temperature == 0 {
		c.LLM.Temperature = 0.7
	}
	if c.LLM.MaxTokens <= 0 {
		c.LLM.MaxTokens = 2048
	}
	// RAG 默认值
	if c.RAG.TopK <= 0 {
		c.RAG.TopK = 5
	}
	if c.RAG.MaxContextTokens <= 0 {
		c.RAG.MaxContextTokens = 2048
	}
	if c.RAG.MaxChunks <= 0 {
		c.RAG.MaxChunks = 5
	}
	if c.RAG.EnableRewrite == nil {
		enabled := true
		c.RAG.EnableRewrite = &enabled
	}
	if c.RAG.HistoryCapacity <= 0 {
		c.RAG.HistoryCapacity = 50
	}
	if c.RAG.HistoryLimit <= 0 {
		c.RAG.HistoryLimit = 10
	}
	// Multi-Query 默认值
	if c.RAG.MultiQueryCount <= 0 {
		c.RAG.MultiQueryCount = 3
	}
	if c.RAG.MultiQueryConcurrency <= 0 {
		c.RAG.MultiQueryConcurrency = 3
	}
	// Decomposition / Step-Back 默认值
	if c.RAG.DecompositionMode == "" {
		c.RAG.DecompositionMode = "parallel"
	}
	if c.RAG.DecompositionMaxSub <= 0 {
		c.RAG.DecompositionMaxSub = 5
	}
	// Routing / HyDE 默认值
	if c.RAG.RoutingFallback == "" {
		c.RAG.RoutingFallback = "multi_query"
	}
	// Strategy 默认值（与阶段一~三默认一致：multi 开、rrf 融合、其余 off）
	if c.RAG.Strategy.Query == "" {
		c.RAG.Strategy.Query = "multi"
	}
	if c.RAG.Strategy.Fusion == "" {
		c.RAG.Strategy.Fusion = "rrf"
	}
	if c.RAG.Strategy.Decomposition == "" {
		c.RAG.Strategy.Decomposition = "off"
	}
	if c.RAG.Strategy.StepBack == "" {
		c.RAG.Strategy.StepBack = "off"
	}
	if c.RAG.Strategy.HyDE == "" {
		c.RAG.Strategy.HyDE = "off"
	}
	if c.RAG.Strategy.Routing == "" {
		c.RAG.Strategy.Routing = "off"
	}
	// Loader 默认值
	if c.Loader.MinReadableChars == 0 {
		c.Loader.MinReadableChars = 20
	}
	// Server 默认值
	if c.Server.Port <= 0 {
		c.Server.Port = 8080
	}
	if c.Server.FileStorageDir == "" {
		c.Server.FileStorageDir = "./data/uploads"
	}
	if c.Server.UploadMaxSizeMB <= 0 {
		c.Server.UploadMaxSizeMB = 50
	}
	if c.Server.WorkerCount <= 0 {
		c.Server.WorkerCount = 2
	}
	if c.Server.TaskMaxRetries <= 0 {
		c.Server.TaskMaxRetries = 3
	}
}
