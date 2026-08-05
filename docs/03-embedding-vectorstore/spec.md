# Embedding 与向量存储 Spec

## 背景

BinRag 已完成文档加载器和分块器，能将文档转为结构化 Chunk 列表。Embedding 与向量存储是 Pipeline 的第三个环节，负责将 Chunk 文本转为向量并持久化到向量数据库，同时提供文档入库的完整编排流程（Load → Chunk → Embed → Store）。

## 目标

- 定义 Embedding 提供者接口，通过配置选择具体实现（OpenAI / 本地模型）
- 支持批量 Embedding 生成（带限流和重试）
- 定义 VectorStore 接口，对接 Qdrant（Docker Compose 部署）
- 提供同步 Pipeline 编排：Load → Chunk → Embed → Store 一步完成
- 向量维度和距离度量由配置文件指定
- 提供 docker-compose.yml 和项目配置文件模板

## 功能需求

- F1：Embedder 接口 — 定义统一的 Embedding 生成接口，接收文本列表，输出向量列表
- F2：配置化 Provider 选择 — 通过配置文件指定使用哪个 Embedding 提供者（OpenAI / 本地兼容模型），包括 API 地址、API Key、模型名称
- F3：批量 Embedding — 支持批量生成（一次最多 N 条），自动按 batch_size 拆分，限制并发数
- F4：限流与重试 — 对 API 调用进行限流（可配置 QPS），遇到 429 或网络错误自动重试（指数退避，最多 3 次）
- F5：VectorStore 接口 — 定义向量存储抽象，支持 Upsert、Search、Delete 三个核心操作
- F6：向量搜索 + 元数据过滤 — Search 支持传入过滤条件（按文档来源、自定义字段等），与向量相似度搜索同时生效
- F7：批量操作 — Upsert 和 Delete 支持批量（一次操作多条向量）
- F8：Qdrant 实现 — 对接 Qdrant gRPC SDK，实现 VectorStore 接口，Docker Compose 一键启动
- F9：Pipeline 编排 — 提供 Ingest 函数，输入文件 Reader + FileInfo + 配置，同步执行 Load → Chunk → Embed → Store 全流程
- F10：配置驱动 — 向量维度、距离度量、batch_size、并发数、重试次数等均由配置文件指定
- F11：基础设施配置 — 提供 docker-compose.yml（Qdrant 服务）和项目配置文件模板（config.yaml）

## 非功能需求

- N1：并发安全 — Embedder 和 VectorStore 实例可被多个 goroutine 并发调用
- N2：可测试性 — 通过接口抽象，测试时可 mock Embedder 和 VectorStore，Pipeline 测试不依赖外部服务
- N3：超时控制 — 所有外部调用（Embedding API、Qdrant）支持 context 超时和取消
- N4：错误可观测 — 批量操作中部分失败时，返回成功数量和失败详情，不因单条失败阻断整批
- N5：性能基线 — 1000 个 Chunk 的 Embedding 生成 + 入库不超过 30s（受外部 API 延迟影响）

## 不做的事

- 不做 Embedding 模型的本地推理（只通过 HTTP API 调用）
- 不做向量数据库的集群管理/运维
- 不做 Collection 自动迁移（维度变更需手动重建）
- 不做异步队列/后台任务（当前同步执行）
- 不做向量压缩/量化（交给 Qdrant 自身配置）
- 不做多租户隔离（单 Collection 模式）
- 不做内存 VectorStore（测试时 mock 接口即可）

## 验收标准

- AC1：配置 OpenAI provider，传入 10 条文本，返回 10 个向量，维度与配置一致
- AC2：配置本地模型 provider（兼容 OpenAI 接口），同样返回正确维度向量
- AC3：传入 100 条文本 + batch_size=20，实际发出 5 次 API 调用（验证批量拆分）
- AC4：模拟 API 返回 429，验证自动重试并最终成功
- AC5：docker-compose up 启动 Qdrant，Upsert 10 条向量后 Search 返回最相似的 TopK 结果且顺序正确
- AC6：Search 附带过滤条件时，结果仅包含满足条件的向量
- AC7：Delete 指定 ID 后，Search 不再返回该向量
- AC8：Pipeline Ingest 传入一个 Markdown 文件，全流程执行完成后在 VectorStore 中可搜索到相关内容
- AC9：单元测试 mock Embedder + mock VectorStore，不依赖外部服务
- AC10：集成测试依赖 Docker Compose 中的 Qdrant 实例，全流程可跑通
