# Embedding 与向量存储 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `docker-compose.yml` | Qdrant 服务定义 |
| 新建 | `configs/config.yaml` | 项目配置模板 |
| 新建 | `internal/config/config.go` | 配置加载 |
| 新建 | `internal/embedding/embedder.go` | Embedder 接口 + OpenAI 兼容实现 |
| 新建 | `internal/embedding/embedder_test.go` | Embedder 单元测试 |
| 新建 | `internal/vectorstore/types.go` | VectorRecord、SearchResult、SearchRequest |
| 新建 | `internal/vectorstore/store.go` | VectorStore 接口 |
| 新建 | `internal/vectorstore/qdrant.go` | Qdrant gRPC 实现 |
| 新建 | `internal/vectorstore/qdrant_test.go` | Qdrant 集成测试 |
| 新建 | `internal/pipeline/pipeline.go` | Pipeline 接口 + 实现 |
| 新建 | `internal/pipeline/pipeline_test.go` | Pipeline 单元测试 |

## T1: 基础设施配置

**文件：** `docker-compose.yml`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. 创建 `docker-compose.yml`：Qdrant 服务（镜像 qdrant/qdrant:latest，端口 6333/6334，volume 持久化）
2. 创建 `configs/` 目录
3. 创建 `configs/config.yaml` 模板，包含 embedder 和 vectorstore 完整配置项（带注释说明）
4. 添加 `.gitignore` 中忽略 `configs/config.local.yaml`

**验证：** `docker compose config` 无报错；config.yaml 结构完整

## T2: 配置加载

**文件：** `internal/config/config.go`
**依赖：** T1
**步骤：**
1. 创建 `internal/config/` 目录
2. 定义 `Config` 结构体（Embedder、VectorStore、Chunker）
3. 定义 `EmbedderConfig` 结构体（Provider、APIKey、BaseURL、Model、Dimension、BatchSize、MaxRetries、QPS）
4. 定义 `VectorStoreConfig` 结构体（Host、CollectionName、Dimension、Distance）
5. 实现 `LoadConfig(path string) (*Config, error)`
6. 实现默认路径逻辑：环境变量 BINRAG_CONFIG 或 ./configs/config.yaml
7. 添加依赖：gopkg.in/yaml.v3

**验证：** `go build ./internal/config/...` 编译通过

## T3: VectorStore 类型与接口

**文件：** `internal/vectorstore/types.go`、`internal/vectorstore/store.go`
**依赖：** T2
**步骤：**
1. 创建 `internal/vectorstore/` 目录
2. 定义 VectorRecord（ID、Vector、Payload）
3. 定义 SearchRequest（Vector、TopK、Filter）
4. 定义 SearchResult（ID、Score、Payload）
5. 定义 VectorStore 接口（Upsert、Search、Delete、EnsureCollection）

**验证：** `go build ./internal/vectorstore/...` 编译通过

## T4: Embedder 实现

**文件：** `internal/embedding/embedder.go`
**依赖：** T2
**步骤：**
1. 创建 `internal/embedding/` 目录
2. 定义 Embedder 接口
3. 定义 openaiEmbedder 结构体（config、httpClient、limiter）
4. 实现 NewEmbedder(config)
5. 实现 Embed：按 BatchSize 拆分 → 限流 → POST /v1/embeddings → 重试 → 拼接结果
6. 添加依赖：golang.org/x/time

**验证：** `go build ./internal/embedding/...` 编译通过

## T5: Embedder 单元测试

**文件：** `internal/embedding/embedder_test.go`
**依赖：** T4
**步骤：**
1. httptest.NewServer 模拟 OpenAI API
2. 测试正常请求：10 条 → 10 个向量
3. 测试批量拆分：100 条 + batch=20 → 5 次请求
4. 测试重试：前 2 次 429 + 第 3 次成功
5. 测试超时：ctx 超时返回错误

**验证：** `go test ./internal/embedding/... -v` 全部通过

## T6: Qdrant VectorStore 实现

**文件：** `internal/vectorstore/qdrant.go`
**依赖：** T3
**步骤：**
1. 添加依赖：github.com/qdrant/go-client
2. 定义 qdrantStore 结构体
3. 实现 NewQdrantStore(config)：建立 gRPC 连接
4. 实现 EnsureCollection：检查存在 → 不存在则创建
5. 实现 Upsert：VectorRecord → PointStruct → Points.Upsert
6. 实现 Search：SearchPoints + Filter 转换
7. 实现 Delete：PointsSelector → Points.Delete
8. 实现 Close()

**验证：** `go build ./internal/vectorstore/...` 编译通过

## T7: Qdrant 集成测试

**文件：** `internal/vectorstore/qdrant_test.go`
**依赖：** T6、T1
**步骤：**
1. 文件顶部 `//go:build integration`
2. 测试 EnsureCollection
3. 测试 Upsert 10 条
4. 测试 Search TopK 排序
5. 测试 Filter 过滤
6. 测试 Delete 后不可搜到
7. 清理测试 Collection

**验证：** `docker compose up -d && go test ./internal/vectorstore/... -v -tags=integration`

## T8: Pipeline 实现

**文件：** `internal/pipeline/pipeline.go`
**依赖：** T4、T6
**步骤：**
1. 创建 `internal/pipeline/` 目录
2. 定义 Pipeline 接口（Ingest）
3. 定义 defaultPipeline 结构体
4. 实现 NewPipeline(loader, chunker, embedder, vectorstore, chunkerConfig)
5. 实现 Ingest：Load → Chunk → Embed → 组装 VectorRecord → Upsert
6. 添加依赖：github.com/google/uuid

**验证：** `go build ./internal/pipeline/...` 编译通过

## T9: Pipeline 单元测试

**文件：** `internal/pipeline/pipeline_test.go`
**依赖：** T8
**步骤：**
1. 定义 mockEmbedder、mockVectorStore
2. 测试 Ingest 完整流程：Markdown 输入 → records 数量正确
3. 验证 Payload 包含 filename、heading_context、chunk_index
4. 测试 Embed 失败返回 error
5. 测试 Upsert 失败返回 error

**验证：** `go test ./internal/pipeline/... -v` 全部通过

## 执行顺序

```
T1 → T2 → T3（与 T4 可并行）
            ↘
             T4 → T5
            ↗
      T3 → T6 → T7

T8（依赖 T4、T6）→ T9
```
