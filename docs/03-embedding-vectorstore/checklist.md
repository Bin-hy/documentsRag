# Embedding 与向量存储 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 基础设施

- [ ] `docker compose config` 无报错（验证：运行命令无输出错误）
- [ ] `docker compose up -d` 启动 Qdrant 成功（验证：`docker compose ps` 显示 qdrant 状态为 running）
- [ ] configs/config.yaml 包含完整配置模板（验证：文件存在且含 embedder/vectorstore 两个顶级 key）

## 编译与测试

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./internal/embedding/... -v` 全部通过
- [ ] `go test ./internal/pipeline/... -v` 全部通过
- [ ] `go test ./internal/vectorstore/... -v -tags=integration` 全部通过（需 Qdrant 运行）

## Embedder 功能（AC1-AC4）

- [ ] 配置 OpenAI provider 传入 10 条文本返回 10 个向量（验证：mock server 测试）
- [ ] 配置本地模型 provider（改 BaseURL）同样返回正确结果（验证：mock server 换地址测试）
- [ ] 100 条文本 + batch_size=20 实际发出 5 次 API 调用（验证：mock server 记录请求次数 == 5）（AC3）
- [ ] API 返回 429 时自动重试并最终成功（验证：mock server 前 N 次返回 429）（AC4）
- [ ] context 超时时返回错误不阻塞（验证：设置短超时 + mock server sleep）

## VectorStore 功能（AC5-AC7）

- [ ] Qdrant Upsert 10 条后 Search 返回 TopK 且按相似度排序（验证：集成测试）（AC5）
- [ ] Search 带 Filter 只返回匹配项（验证：不同 source 的向量，filter 指定一个）（AC6）
- [ ] Delete 后 Search 不再返回该向量（验证：先 Upsert → Search 确认存在 → Delete → Search 确认消失）（AC7）
- [ ] EnsureCollection 幂等（验证：调用两次不报错，Collection 存在时跳过创建）

## Pipeline 编排（AC8-AC9）

- [ ] Ingest 传入 Markdown 文件后 mockVectorStore 收到正确数量的 records（验证：单元测试）（AC8）
- [ ] record 的 Payload 包含 filename、heading_context、chunk_index、content 字段（验证：检查 mock 记录）
- [ ] Embed 失败时 Ingest 返回 error（验证：mock embedder 返回 error）
- [ ] Upsert 失败时 Ingest 返回 error（验证：mock vectorstore 返回 error）
- [ ] 单元测试不依赖外部服务（验证：mock 全部外部依赖）（AC9）

## 配置驱动（F10-F11）

- [ ] LoadConfig 正确解析 config.yaml（验证：解析后字段值与文件内容一致）
- [ ] 环境变量 BINRAG_CONFIG 可覆盖默认路径（验证：设置环境变量后从指定路径加载）
- [ ] 缺失配置项使用默认值（验证：空 yaml 加载后 BatchSize/MaxRetries 为默认值）

## 端到端场景

- [ ] 场景 1：完整 Pipeline — docker compose up → LoadConfig → NewEmbedder + NewQdrantStore → NewPipeline → Ingest 一个 Markdown 文件 → Search 该文件内容可命中
- [ ] 场景 2：Provider 切换 — 修改 config.yaml 的 base_url 指向另一个 mock server → Ingest 仍然成功
- [ ] 场景 3：错误恢复 — Embedding API 暂时不可用（前 2 次 429）→ 重试后最终 Ingest 成功
