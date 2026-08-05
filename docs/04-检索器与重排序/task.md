# 检索器与重排序 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/retriever/types.go` | RetrieveResult, RetrieveRequest, BM25Result, BM25Doc |
| 新建 | `internal/retriever/tokenizer.go` | Tokenizer 接口 + SimpleTokenizer 实现 |
| 新建 | `internal/retriever/bm25.go` | BM25Index 接口 + defaultBM25Index 实现 |
| 新建 | `internal/retriever/rrf.go` | FuseRRF 融合函数 |
| 新建 | `internal/retriever/retriever.go` | Retriever 接口 + defaultRetriever 编排实现 |
| 新建 | `internal/reranker/reranker.go` | Reranker 接口 + apiReranker 实现 |
| 修改 | `internal/config/config.go` | 新增 RetrieverConfig, RerankerConfig |
| 修改 | `configs/config.yaml` | 新增 retriever 和 reranker 配置段 |
| 修改 | `internal/pipeline/pipeline.go` | Ingest 中新增 BM25 索引更新 |
| 新建 | `internal/retriever/retriever_test.go` | Retriever + BM25 + RRF 单元测试 |
| 新建 | `internal/reranker/reranker_test.go` | Reranker 单元测试 |

## T1: 类型定义

**文件：** `internal/retriever/types.go`
**依赖：** 无
**步骤：**
1. 创建 `internal/retriever/` 目录
2. 定义 `RetrieveResult` 结构体（ID, Content, Score, Metadata）
3. 定义 `RetrieveRequest` 结构体（Query, TopK, Filter）
4. 定义 `BM25Result` 结构体（ID, Score）
5. 定义 `BM25Doc` 结构体（ID, Content）

**验证：** `go build ./internal/retriever/...` 编译通过

## T2: Tokenizer

**文件：** `internal/retriever/tokenizer.go`
**依赖：** T1
**步骤：**
1. 定义 `Tokenizer` 接口，方法 `Tokenize(text string) []string`
2. 实现 `SimpleTokenizer` 结构体
3. Tokenize 逻辑：遍历 rune，连续 ASCII 字母/数字聚合为英文 token（小写化），连续中文字符做 bigram 切分
4. 构造函数 `NewSimpleTokenizer() Tokenizer`

**验证：** `go build ./internal/retriever/...` 编译通过

## T3: BM25 倒排索引

**文件：** `internal/retriever/bm25.go`
**依赖：** T2
**步骤：**
1. 定义 `BM25Index` 接口（Add, Remove, Search, Rebuild, DocCount）
2. 定义内部结构：`posting{docID string, tf int}`, `defaultBM25Index` 包含 inverted map、docLen map、totalLen、avgLen、sync.RWMutex
3. 实现 `NewBM25Index(tokenizer Tokenizer) BM25Index`
4. 实现 `Add`：分词 → 统计词频 → 更新倒排表和文档长度，写锁保护
5. 实现 `Remove`：从倒排表移除该文档的所有 posting，更新统计，写锁保护
6. 实现 `Search`：对 query 分词 → 遍历每个 term 的 posting → 按 BM25 公式（k1=1.2, b=0.75）累加得分 → 按分数排序取 topK，读锁保护
7. 实现 `Rebuild`：清空索引，批量 Add 所有文档
8. 实现 `DocCount`：返回文档数

**验证：** `go build ./internal/retriever/...` 编译通过

## T4: RRF 融合

**文件：** `internal/retriever/rrf.go`
**依赖：** T1
**步骤：**
1. 定义 `RRFConfig` 结构体（K int, VectorWeight float32, BM25Weight float32）
2. 实现 `FuseRRF(vectorResults []RetrieveResult, bm25Results []BM25Result, allDocs map[string]RetrieveResult, cfg RRFConfig) []RetrieveResult`
3. 逻辑：为每个文档按其在各路的 rank 计算 `weight/(k+rank)` 得分并累加，按融合分数降序排列
4. bm25Results 中的文档需要从 allDocs map 获取 Content 和 Metadata 填充到 RetrieveResult

**验证：** `go build ./internal/retriever/...` 编译通过

## T5: 配置扩展

**文件：** `internal/config/config.go`, `configs/config.yaml`
**依赖：** 无
**步骤：**
1. 在 config.go 中新增 `RetrieverConfig` 结构体：TopK(int), RRF_K(int), VectorWeight(float32), BM25Weight(float32), EnableBM25(bool), EnableReranker(bool)
2. 新增 `RerankerConfig` 结构体：BaseURL(string), APIKey(string), Model(string), TopN(int), MaxRetries(int), QPS(int)
3. 在 `Config` 结构体中新增 `Retriever RetrieverConfig` 和 `Reranker RerankerConfig` 字段
4. 在 configs/config.yaml 中添加 retriever 和 reranker 配置段，填写默认值

**验证：** `go build ./internal/config/...` 编译通过，配置文件格式正确

## T6: Reranker 实现

**文件：** `internal/reranker/reranker.go`
**依赖：** T5, T1
**步骤：**
1. 创建 `internal/reranker/` 目录
2. 定义 `Reranker` 接口：`Rerank(ctx, query string, candidates []retriever.RetrieveResult, topN int) ([]retriever.RetrieveResult, error)`
3. 实现 `apiReranker` 结构体，包含 config、http.Client、rate.Limiter
4. 实现 `NewReranker(cfg config.RerankerConfig) Reranker`
5. 实现 `Rerank`：构造 JSON 请求体（model, query, documents, top_n）→ POST 到 baseURL+"/v1/rerank" → 解析响应 → 按 relevance_score 重排候选列表
6. 支持重试逻辑（429/5xx 可重试，复用 RetryableError 模式）

**验证：** `go build ./internal/reranker/...` 编译通过

## T7: Retriever 编排实现

**文件：** `internal/retriever/retriever.go`
**依赖：** T3, T4, T5, T6
**步骤：**
1. 定义 `Retriever` 接口：`Search(ctx, req RetrieveRequest) ([]RetrieveResult, error)`
2. 实现 `defaultRetriever` 结构体，包含 embedder、vectorstore、bm25Index、reranker、config
3. 实现 `NewRetriever(cfg config.RetrieverConfig, emb embedding.Embedder, vs vectorstore.VectorStore, idx BM25Index, rk reranker.Reranker) Retriever`
4. 实现 `Search`：
   - 并发启动向量检索（embedder.Embed → vectorstore.Search）和 BM25 检索
   - 等待两路结果，如果 BM25 索引为空或禁用，仅用向量结果
   - 调用 FuseRRF 融合
   - 若 Reranker 启用，调用 reranker.Rerank；失败时降级返回融合结果
   - 返回最终结果

**验证：** `go build ./internal/retriever/...` 编译通过

## T8: Pipeline 集成 BM25 索引更新

**文件：** `internal/pipeline/pipeline.go`
**依赖：** T3, T7
**步骤：**
1. 在 `defaultPipeline` 中新增可选字段 `bm25Index retriever.BM25Index`
2. 修改 `NewPipeline` 接受可选的 BM25Index 参数（可用 functional options 或直接加参数）
3. 在 `Ingest` 方法的 Store 步骤成功后，如果 bm25Index != nil，对每个 record 调用 `bm25Index.Add(id, content)`

**验证：** `go build ./internal/pipeline/...` 编译通过，现有 pipeline_test.go 仍通过

## T9: Retriever 单元测试

**文件：** `internal/retriever/retriever_test.go`
**依赖：** T7
**步骤：**
1. TestSimpleTokenizer：验证中文 bigram + 英文分词正确
2. TestBM25Index：添加 3 篇文档，搜索关键词验证排序正确
3. TestBM25Remove：添加后移除，验证搜索不再命中
4. TestBM25Rebuild：rebuild 后索引状态正确
5. TestFuseRRF：构造两路结果，验证融合分数计算和去重
6. TestRetrieverSearch：mock embedder/vectorstore/bm25/reranker，验证完整流程
7. TestRetrieverDegradeNoReranker：禁用 reranker，验证直接返回融合结果
8. TestRetrieverDegradeEmptyBM25：BM25 索引为空时退化为纯向量结果

**验证：** `go test ./internal/retriever/... -race` 全部通过

## T10: Reranker 单元测试

**文件：** `internal/reranker/reranker_test.go`
**依赖：** T6
**步骤：**
1. TestRerankNormal：mock HTTP server 返回正常排序结果，验证重排正确
2. TestRerankRetry：第一次 429，第二次成功，验证重试逻辑
3. TestRerankTimeout：context 超时返回错误

**验证：** `go test ./internal/reranker/... -race` 全部通过

## 执行顺序

```
T1 → T2 → T3
T1 → T4
T5（独立）
T1 + T5 → T6
T3 + T4 + T5 + T6 → T7
T3 + T7 → T8
T7 → T9
T6 → T10
```
