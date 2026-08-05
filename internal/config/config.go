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
}

// RetrieverConfig 检索器配置
type RetrieverConfig struct {
	TopK           int     `yaml:"top_k"`
	RRFK           int     `yaml:"rrf_k"`
	VectorWeight   float32 `yaml:"vector_weight"`
	BM25Weight     float32 `yaml:"bm25_weight"`
	EnableBM25     bool    `yaml:"enable_bm25"`
	EnableReranker bool    `yaml:"enable_reranker"`
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
}
