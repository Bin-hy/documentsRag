# 检索器与重排序 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 编译与测试

- [ ] `go build ./...` 编译无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./internal/retriever/... -race` 全部通过
- [ ] `go test ./internal/reranker/... -race` 全部通过
- [ ] `go test ./internal/pipeline/... -race` 全部通过（现有测试不回归）

## Tokenizer

- [ ] 英文 "Hello World" 分词得到 ["hello", "world"]
- [ ] 中文 "向量数据库" bigram 分词得到 ["向量", "量数", "数据", "据库"]
- [ ] 中英混合 "使用Qdrant存储" 正确切分中文和英文部分

## BM25 索引

- [ ] Add 3 篇文档后 DocCount() == 3
- [ ] Search("Qdrant") 返回包含 "Qdrant" 的文档，分数 > 0
- [ ] 包含关键词更多次的文档排序更靠前
- [ ] Remove 一篇文档后 DocCount 减 1，且该文档不再被检索命中
- [ ] Rebuild 重建后索引状态与逐条 Add 一致
- [ ] 并发 Add + Search 不 panic（-race 通过）

## RRF 融合

- [ ] 两路都包含的文档，融合分数 = vector_weight/(k+rank1) + bm25_weight/(k+rank2)
- [ ] 仅出现在一路的文档，另一路不贡献分数
- [ ] 结果按融合分数降序排列
- [ ] 修改 vector_weight/bm25_weight 后排序发生变化

## Retriever 编排

- [ ] Search 返回 ≤ TopK 条结果，每条包含 ID、Content、Score、Metadata
- [ ] 向量检索路径正确调用 Embedder.Embed + VectorStore.Search
- [ ] BM25 路径正确调用 BM25Index.Search
- [ ] 两路检索并行执行（通过 mock 延迟验证总耗时 < 两路之和）
- [ ] EnableBM25=false 时跳过 BM25，仅返回向量结果
- [ ] EnableReranker=false 时跳过 Reranker，直接返回融合结果

## Reranker

- [ ] 正常请求：发送 query + documents 到 /v1/rerank，按返回的 relevance_score 重排
- [ ] 429 自动重试后成功
- [ ] context 超时返回错误
- [ ] Reranker API 失败时 Retriever 降级返回融合结果（不报错）

## Pipeline 集成

- [ ] Ingest 成功后 BM25 索引包含新入库文档（DocCount 增加，Search 可命中）
- [ ] bm25Index 为 nil 时 Ingest 正常工作不 panic

## 端到端场景

- [ ] 场景 1：入库一篇包含 "Qdrant 向量数据库配置" 的 Markdown 文档，用 query "Qdrant 配置" 检索，返回结果中包含该文档内容
- [ ] 场景 2：入库 3 篇文档，其中 1 篇同时包含语义相关和关键词匹配内容，混合检索后该文档排名高于仅命中一路的文档
