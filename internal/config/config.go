package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	WebSearch   WebSearchConfig   `yaml:"web_search"` // 联网搜索（web_search 数据源 / 增强模式 tool-use）
	Postgres    PostgresConfig    `yaml:"postgres"`
	Server      ServerConfig      `yaml:"server"`
	Loader      LoaderConfig      `yaml:"loader"`
	Multimedia  MultimediaConfig  `yaml:"multimedia"` // 多媒体处理（图片/音频/视频）能力配置
	OIDC        OIDCConfig        `yaml:"oidc"`       // 三方登录 Provider（自定义 OIDC + GitHub OAuth2）
}

// WebSearchConfig 联网搜索提供者配置
type WebSearchConfig struct {
	Provider string `yaml:"provider"` // 提供者：bocha（默认）
	BaseURL  string `yaml:"base_url"` // 空 = 提供者官方地址
	APIKey   string `yaml:"api_key"`  // 空 = 未就绪（Available()==false）
	Count    int    `yaml:"count"`    // 默认 5
	Timeout  int    `yaml:"timeout"`  // 秒，默认 30
	QPS      int    `yaml:"qps"`      // 默认 1
}

// LoaderConfig 文档加载器配置
// MinReadableChars: 全文最低可读文本量（中文+单词计数），低于该值判定为扫描件/空内容拒绝入库；0 表示禁用
// （阈值语义：默认 20，正常文档动辄数百，扫描件 PDF 图像指令通常 < 15）
type LoaderConfig struct {
	MinReadableChars int `yaml:"min_readable_chars"`
}

// MultimediaConfig 多媒体处理能力配置（ingestion 阶段，与问答阶段 llm 职责分离）
// Vision: 图片/视频帧视觉理解；Speech: 语音转写。各自独立可配，未配置（APIKey 为空）时
// 对应类型文件上传被明确拒绝（见 ErrMediaCapabilityMissing）。
type MultimediaConfig struct {
	Vision           MultimediaServiceConfig `yaml:"vision"`
	Speech           MultimediaServiceConfig `yaml:"speech"`
	FrameIntervalSec int                     `yaml:"frame_interval_sec"` // 视频抽帧间隔（秒），默认 10（兼容，video.frame_interval_sec 优先）
	Video            VideoConfig             `yaml:"video"`
}

// VideoConfig 视频处理配置（拆流 + 双抽帧策略 + 场景检测，spec F1-F4）
type VideoConfig struct {
	FrameStrategy    string                  `yaml:"frame_strategy"`     // fixed | scene（默认 fixed）
	FrameIntervalSec int                     `yaml:"frame_interval_sec"` // 固定抽帧间隔（秒），优先于顶层
	Scene            SceneConfig             `yaml:"scene"`
	VisionEmbedding  MultimediaServiceConfig `yaml:"vision_embedding"` // 场景检测视觉 embedding（frame_strategy=scene 必填）
}

// SceneConfig 场景检测参数（两阶段：预抽帧 → 视觉 embedding 相似度 → 场景代表帧）
type SceneConfig struct {
	SampleFPS           int     `yaml:"sample_fps"`            // 预抽帧率（fps），默认 2
	SimilarityThreshold float64 `yaml:"similarity_threshold"`  // 相邻帧余弦相似度低于该值判定场景切换，默认 0.85
	MinSceneDurationMs  int     `yaml:"min_scene_duration_ms"` // 最小场景时长（毫秒），默认 3000
}

// MultimediaServiceConfig 单个多媒体服务配置
// Provider: openai_compat（默认，OpenAI 兼容 API；预留本地 VLM / Whisper 等扩展）
// 注意：provider=dashscope 时 qwen ASR 不返回时间戳，该 provider 下音频 chunk 时间戳为 0，
// 视频/音频定位锚点不可用（spec 30 F5 降级声明）。
type MultimediaServiceConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"` // 空 = 提供者官方地址
	APIKey   string `yaml:"api_key"`  // 空 = 未配置（Available()==false）
	Model    string `yaml:"model"`
	Timeout  int    `yaml:"timeout"` // 秒，默认 30
}

// Available 该服务是否已配置（有 APIKey 视为可用）
func (s MultimediaServiceConfig) Available() bool {
	return s.APIKey != ""
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
	// Mode 重排模式：api（默认，调 /v1/rerank 专用重排接口）/ llm（用 chat/completions 让通用大模型打分重排）
	Mode string `yaml:"mode"`
	// LLMPromptTemplate LLM 打分模式的自定义 prompt 模板，留空使用内置默认模板
	// 模板中 {query}、{document} 占位符会被替换为实际查询与候选文档内容
	LLMPromptTemplate string `yaml:"llm_prompt_template"`
	// LLMTemperature LLM 打分模式的采样温度，默认 0（打分场景要求确定性输出）
	LLMTemperature float64 `yaml:"llm_temperature"`
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
//	Thinking: off / on
//	DataSources: 允许的数据源（vector_store / web_search），空 = 默认仅 vector_store（私有性默认）
//
// 空字符串/空数组表示「未设置，继承低层级」（全局默认由 applyDefaults 兜底）
type StrategyConfig struct {
	Query         string `yaml:"query" json:"query,omitempty"`
	Fusion        string `yaml:"fusion" json:"fusion,omitempty"`
	Decomposition string `yaml:"decomposition" json:"decomposition,omitempty"`
	StepBack      string `yaml:"step_back" json:"step_back,omitempty"`
	HyDE          string `yaml:"hyde" json:"hyde,omitempty"`
	Routing       string `yaml:"routing" json:"routing,omitempty"`
	Thinking      string `yaml:"thinking" json:"thinking,omitempty"` // off / on，空=继承
	// DataSources 允许的数据源列表；空=未设置（合并后默认仅 vector_store）
	DataSources []string `yaml:"data_sources" json:"data_sources,omitempty"`
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
	Port            int       `yaml:"port"`
	FileStorageDir  string    `yaml:"file_storage_dir"`
	UploadMaxSizeMB int       `yaml:"upload_max_size_mb"`
	WorkerCount     int       `yaml:"worker_count"`
	TaskMaxRetries  int       `yaml:"task_max_retries"`
	AuthEnabled     bool      `yaml:"auth_enabled"`
	BootstrapAPIKey string    `yaml:"bootstrap_api_key"` // 仅首次启动种子用
	RateLimitQPS    int       `yaml:"rate_limit_qps"`    // 0 表示不限制
	MCP             MCPConfig `yaml:"mcp"`               // MCP Server（只读 RAG 能力）
}

// MCPConfig MCP Server 配置（默认关闭，安全默认 D7）
type MCPConfig struct {
	// Enabled 是否启用 MCP Server（默认 false，显式开启后才挂载 /mcp）
	Enabled bool `yaml:"enabled"`
	// Path MCP 端点路径（默认 "/mcp"）
	Path string `yaml:"path"`
	// AuditParamLimit 审计参数截断长度（字符，默认 2000）
	AuditParamLimit int `yaml:"audit_param_limit"`
}

// 登录 Provider 类型与内置名称常量
const (
	ProviderTypeOIDC   = "oidc"   // 标准 OIDC（授权码 + ID Token）
	ProviderTypeOAuth2 = "oauth2" // OAuth2 适配（当前仅内置 GitHub）
	ProviderNameGithub = "github"
)

// OIDCConfig 三方登录配置（自定义 Provider 用 OIDC，GitHub 用 OAuth2 适配）
type OIDCConfig struct {
	Enabled   bool   `yaml:"enabled"`
	PublicURL string `yaml:"public_url"` // 外部可访问基址，用于拼回调 URL；Enabled 时必填
	JWTSecret string `yaml:"jwt_secret"` // 空 = 启动时自动生成随机密钥（重启后旧会话失效）

	// JWTExpireMinutes 会话 JWT 有效期（分钟），默认 120
	JWTExpireMinutes int              `yaml:"jwt_expire_minutes"`
	Providers        []ProviderConfig `yaml:"providers"`
}

// ProviderConfig 单个登录 Provider
// type=oidc：Issuer 必填，默认 scope openid profile email
// type=oauth2：不要求 Issuer，仅允许 name=github，默认 scope read:user（最小权限，不请求 email）
// PermissiveSub：兼容不规范 OIDC 服务商把 sub 签发为数字的场景（仍保留签名/iss/aud/exp/nbf/nonce 全量校验，
// 仅将数字 sub 确定性转为字符串）；默认 false 保持严格规范。
type ProviderConfig struct {
	Name          string   `yaml:"name"`         // 标识，须可安全用于 URL path（^[a-zA-Z0-9_-]+$）
	Type          string   `yaml:"type"`         // oidc（默认）/ oauth2
	DisplayName   string   `yaml:"display_name"` // 前端按钮文案，空 = Name
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	Issuer        string   `yaml:"issuer"`         // 仅 type=oidc；OIDC discovery 地址
	Scope         []string `yaml:"scope"`          // 空 = 按 type 给默认值
	RedirectURL   string   `yaml:"redirect_url"`   // 可选；默认 <public_url>/api/v1/auth/{oidc/{name}|github}/callback
	PermissiveSub bool     `yaml:"permissive_sub"` // 兼容数字 sub（见上注释）
}

// LoadConfig 加载配置文件。
// 约定：同目录 <主文件名>.local.yaml（如 configs/config.local.yaml）为本地私有覆盖，
// 存在时自动合并——local 中出现的字段覆盖主配置，未出现的字段保留主配置值。
// 这样桌面版/默认启动无需 -c 参数也能应用本地配置（如 web_search.api_key、llm 指向本地模型）。
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

	// 自动合并本地覆盖文件（<主文件名>.local.yaml）
	if lp := localOverridePath(path); lp != "" {
		localData, err := os.ReadFile(lp)
		if err != nil {
			slog.Warn("读取本地配置覆盖文件失败，忽略 local 覆盖", "path", lp, "err", err)
		} else if err := yaml.Unmarshal(localData, &cfg); err != nil {
			// yaml.v3 对已填充结构：local 出现的字段覆盖，未出现的保留
			slog.Warn("解析本地配置覆盖文件失败，忽略 local 覆盖", "path", lp, "err", err)
		} else {
			slog.Info("已合并本地配置覆盖", "local", lp, "main", path)
		}
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// localOverridePath 返回 <主文件名>.local.yaml 路径；不存在或与主文件相同时返回空串
func localOverridePath(path string) string {
	ext := filepath.Ext(path)
	lp := strings.TrimSuffix(path, ext) + ".local" + ext
	if lp == path {
		return ""
	}
	if _, err := os.Stat(lp); err != nil {
		return ""
	}
	return lp
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
	// WebSearch 默认值
	if c.WebSearch.Provider == "" {
		c.WebSearch.Provider = "bocha"
	}
	if c.WebSearch.Count <= 0 {
		c.WebSearch.Count = 5
	}
	if c.WebSearch.Timeout <= 0 {
		c.WebSearch.Timeout = 30
	}
	if c.WebSearch.QPS <= 0 {
		c.WebSearch.QPS = 1
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
	// MCP Server 默认值（Enabled 零值即 false，安全默认：显式开启才挂载）
	if c.Server.MCP.Path == "" {
		c.Server.MCP.Path = "/mcp"
	}
	if c.Server.MCP.AuditParamLimit <= 0 {
		c.Server.MCP.AuditParamLimit = 2000
	}
	// 三方登录默认值
	if c.OIDC.JWTExpireMinutes <= 0 {
		c.OIDC.JWTExpireMinutes = 120
	}
	for i := range c.OIDC.Providers {
		p := &c.OIDC.Providers[i]
		if p.Type == "" {
			p.Type = ProviderTypeOIDC
		}
		if p.DisplayName == "" {
			p.DisplayName = p.Name
		}
		if len(p.Scope) == 0 {
			if p.Type == ProviderTypeOAuth2 && p.Name == ProviderNameGithub {
				p.Scope = []string{"read:user"} // GitHub 最小权限，不请求 email
			} else {
				p.Scope = []string{"openid", "profile", "email"}
			}
		}
	}
	// 多媒体能力默认值（未配置 APIKey 时 Available()==false，对应类型上传被明确拒绝）
	if c.Multimedia.FrameIntervalSec <= 0 {
		c.Multimedia.FrameIntervalSec = 10
	}
	if c.Multimedia.Vision.Provider == "" {
		c.Multimedia.Vision.Provider = "openai_compat"
	}
	if c.Multimedia.Vision.Timeout <= 0 {
		c.Multimedia.Vision.Timeout = 30
	}
	if c.Multimedia.Speech.Provider == "" {
		c.Multimedia.Speech.Provider = "openai_compat"
	}
	if c.Multimedia.Speech.Timeout <= 0 {
		c.Multimedia.Speech.Timeout = 30
	}
	// 视频处理默认值（frame_interval_sec 顶层兼容，video 子节优先）
	if c.Multimedia.Video.FrameStrategy == "" {
		c.Multimedia.Video.FrameStrategy = "fixed"
	}
	if c.Multimedia.Video.FrameIntervalSec <= 0 {
		c.Multimedia.Video.FrameIntervalSec = c.Multimedia.FrameIntervalSec
	}
	if c.Multimedia.Video.Scene.SampleFPS <= 0 {
		c.Multimedia.Video.Scene.SampleFPS = 2
	}
	if c.Multimedia.Video.Scene.SimilarityThreshold <= 0 {
		c.Multimedia.Video.Scene.SimilarityThreshold = 0.85
	}
	if c.Multimedia.Video.Scene.MinSceneDurationMs <= 0 {
		c.Multimedia.Video.Scene.MinSceneDurationMs = 3000
	}
	if c.Multimedia.Video.VisionEmbedding.Provider == "" {
		c.Multimedia.Video.VisionEmbedding.Provider = "openai_compat"
	}
	if c.Multimedia.Video.VisionEmbedding.Timeout <= 0 {
		c.Multimedia.Video.VisionEmbedding.Timeout = 30
	}
}

// providerNamePattern Provider Name 合法字符（须可安全用于 URL path）
const providerNamePattern = `^[a-zA-Z0-9_-]+$`

// Validate 静态校验配置合法性（Load → ApplyDefaults → Validate）。
// 运行时依赖（OIDC discovery/JWKS）由 auth.NewManager 校验，不在此层。
func (c *Config) Validate() error {
	var errs []string
	if c.OIDC.Enabled {
		if c.OIDC.PublicURL == "" {
			errs = append(errs, "oidc.public_url 必填（oidc.enabled=true 时）")
		}
		seen := make(map[string]bool, len(c.OIDC.Providers))
		for i, p := range c.OIDC.Providers {
			where := fmt.Sprintf("oidc.providers[%d] (%s)", i, p.Name)
			if p.Name == "" {
				errs = append(errs, where+": name 不能为空")
			} else {
				if !regexp.MustCompile(providerNamePattern).MatchString(p.Name) {
					errs = append(errs, where+": name 含非法字符，须匹配 "+providerNamePattern)
				}
				if seen[p.Name] {
					errs = append(errs, where+": name 重复")
				}
				seen[p.Name] = true
			}
			if p.ClientID == "" {
				errs = append(errs, where+": client_id 不能为空")
			}
			if p.ClientSecret == "" {
				errs = append(errs, where+": client_secret 不能为空")
			}
			switch p.Type {
			case ProviderTypeOIDC:
				if p.Issuer == "" {
					errs = append(errs, where+": type=oidc 时 issuer 必填")
				}
			case ProviderTypeOAuth2:
				if p.Name != ProviderNameGithub {
					errs = append(errs, where+": type=oauth2 仅支持内置 github provider")
				}
			default:
				errs = append(errs, where+": type 非法（oidc/oauth2）")
			}
			if p.RedirectURL != "" {
				if _, err := url.Parse(p.RedirectURL); err != nil || !strings.Contains(p.RedirectURL, "://") {
					errs = append(errs, where+": redirect_url 不是合法 URL")
				}
			}
		}
	}
	// 多媒体服务 BaseURL 合法性（非空时须为带协议 URL；空 = 使用提供者官方地址）
	for _, mc := range []struct {
		name string
		svc  MultimediaServiceConfig
	}{{"multimedia.vision", c.Multimedia.Vision}, {"multimedia.speech", c.Multimedia.Speech}, {"multimedia.video.vision_embedding", c.Multimedia.Video.VisionEmbedding}} {
		if mc.svc.BaseURL != "" {
			if _, err := url.Parse(mc.svc.BaseURL); err != nil || !strings.Contains(mc.svc.BaseURL, "://") {
				errs = append(errs, mc.name+".base_url 不是合法 URL")
			}
		}
	}
	// 抽帧策略合法性 + 场景检测依赖（spec F4/AC4）
	switch c.Multimedia.Video.FrameStrategy {
	case "fixed", "scene", "": // 空 = 未显式设置，applyDefaults 补 fixed
	default:
		errs = append(errs, "multimedia.video.frame_strategy 仅支持 fixed / scene")
	}
	if c.Multimedia.Video.FrameStrategy == "scene" && !c.Multimedia.Video.VisionEmbedding.Available() {
		errs = append(errs, "multimedia.video.frame_strategy=scene 时须配置 multimedia.video.vision_embedding.api_key")
	}
	if len(errs) > 0 {
		return fmt.Errorf("配置校验失败: %s", strings.Join(errs, "; "))
	}
	return nil
}
