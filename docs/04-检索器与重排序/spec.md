# 检索器与重排序 Spec

## 背景

BinRag 已完成文档入库 Pipeline（Load → Chunk → Embed → Store），文档以向量形式存储在 Qdrant 中，payload 包含 content、filename、heading_context 等字段。现需实现检索层，接收用户查询后从向量库中召回高质量的文本片段，供后续 LLM 生成回答使用。

纯向量检索在精确关键词匹配场景下表现弱（如搜索特定函数名、错误码），需结合关键词检索（BM25）进行混合召回，再通过 Cross-encoder 重排序提升最终结果质量。

## 目标

- 提供统一的检索入口，输入用户 query，输出排序后的文本片段列表
- 支持混合检索：向量语义检索 + BM25 关键词检索，通过 RRF 融合
- 支持 Cross-encoder 重排序（调用外部 API，配置方式与 Embedder 一致）
- BM25 使用内存倒排索引，启动时从 Qdrant 自动重建
- 所有参数（RRF 权重、TopK、Reranker 配置等）通过配置文件驱动

## 功能需求

- F1: 提供统一的检索入口，接收 query 字符串和可选参数（TopK、过滤条件），返回排序后的结果列表（包含内容、分数、元数据）
- F2: 向量检索——将 query 通过 Embedder 转为向量，调用 VectorStore.Search 召回 TopK 候选
- F3: BM25 关键词检索——对 query 分词后在内存倒排索引中检索，返回 BM25 打分的 TopK 候选
- F4: RRF 融合——将两路结果按加权 RRF 公式合并去重，输出融合排序后的候选列表。支持配置 k 常数（默认 60）和两路权重（默认 vector_weight=0.7, bm25_weight=0.3）
- F5: Cross-encoder 重排序——将融合后的候选列表连同 query 发送到外部 Reranker API（OpenAI 兼容接口），按返回分数重新排序，截取最终 TopN 结果
- F6: BM25 索引管理——文档入库时自动更新索引；提供重建接口，启动时从 Qdrant 全量拉取 content 字段重建倒排索引
- F7: 中文分词支持——BM25 分词器对中文按字/词粒度切分（默认使用简单的 unigram 或 bigram，与 Chunker 的 Tokenizer 复用思路一致）
- F8: 配置化驱动——Retriever 和 Reranker 的所有参数通过 config.yaml 统一管理，包括启用/禁用 BM25、启用/禁用 Reranker、各项阈值和权重

## 非功能需求

- N1: 检索延迟——单次检索（含向量检索 + BM25 + RRF 融合 + Reranker）在千级文档规模下 P99 < 2s（Reranker API 延迟占主要部分）
- N2: 内存占用——BM25 倒排索引在万级 Chunk 规模下内存增量 < 100MB
- N3: 并发安全——BM25 索引支持并发读、互斥写（读写锁），Retriever 整体支持并发调用
- N4: 可降级——当 Reranker API 不可用时，跳过重排序直接返回 RRF 融合结果；当 BM25 索引为空时退化为纯向量检索
- N5: 可观测——关键步骤（向量检索耗时、BM25 耗时、融合数量、Reranker 耗时）通过日志输出，便于后续接入指标监控

## 不做的事

- Query 改写 / 扩展（留给阶段六 LLM 集成时实现）
- 语义缓存（相似 query 复用检索结果）
- BM25 索引持久化到磁盘（当前仅内存 + Qdrant 重建）
- 分布式检索 / 多 Collection 路由
- MMR 多样性去重（当前仅用 Cross-encoder 重排序）
- 自定义分词器插件化（当前使用内置简单分词，后续可扩展）

## 验收标准

- AC1: 调用 Retriever.Search(query, topK) 返回包含 content、score、metadata 的结果列表，数量 ≤ topK
- AC2: 向量检索路径——query 经 Embedder 转为向量后调用 VectorStore.Search，返回语义相关结果
- AC3: BM25 检索路径——query "Qdrant 配置" 能命中包含该关键词的 Chunk，按 BM25 分数排序
- AC4: RRF 融合——同一 query 的两路结果正确去重合并，融合分数按 `weight / (k + rank)` 计算
- AC5: 权重可配置——修改 config 中 vector_weight / bm25_weight 后，融合排序发生变化
- AC6: Reranker 调用——融合后的候选列表发送到 Reranker API，返回重排序后的最终结果，分数来自 API 响应
- AC7: BM25 索引自动更新——通过 Pipeline 入库新文档后，BM25 索引包含新文档内容，检索可命中
- AC8: BM25 索引重建——调用 Rebuild 接口后，索引与 Qdrant 中存储的全量 content 一致
- AC9: 降级——禁用 Reranker 配置后，检索直接返回 RRF 融合结果；BM25 索引为空时退化为纯向量检索结果
- AC10: 并发安全——多 goroutine 同时检索和入库不 panic、不数据竞争（go test -race 通过）
- AC11: 编译通过 `go build ./...`，所有单元测试通过 `go test ./...`
