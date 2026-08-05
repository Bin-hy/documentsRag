# LLM 集成与 RAG 编排 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] LLM 客户端已实现且可被调用（验证：`go build ./...` 编译通过；`internal/llm.NewLLM(cfg)` 可构造）
- [ ] 普通生成输出符合预期（验证：`go test ./internal/llm/` 中 mock 场景通过，返回完整文本）
- [ ] 流式生成增量按序返回，拼合后与普通生成一致（验证：`go test ./internal/llm/` 流式测试通过）
- [ ] RAG 编排器已实现且可被调用（验证：`internal/rag.NewEngine(cfg, llm, retriever, history)` 可构造，编译通过）
- [ ] 对话历史接口与内存实现可用（验证：`go test ./internal/rag/` history 测试通过）
- [ ] Prompt 模板管理可用，内置默认模板可渲染（验证：`go test ./internal/rag/` prompt 测试通过）

## 集成

- [ ] RAG 编排器正确调用检索器与 LLM（验证：engine_test 中 mock retriever / mock llm 被按序调用，断言通过）
- [ ] 改写查询用于检索、原始问题用于生成（验证：engine_test 断言 mock llm 收到的 user 消息包含原问题；mock retriever 收到的 query 为改写结果）
- [ ] 检索结果为空时走降级路径，不调用 LLM 生成（验证：engine_test 空结果场景断言「未找到相关资料」回答）
- [ ] 所有公开接口至少被一个真实调用方使用（验证：`go test ./...` 全部通过 + `go vet ./...` 无告警）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...`）
- [ ] 所有单元测试通过（验证：`go test ./...`）
- [ ] 无数据竞争（验证：`go test -race ./internal/llm/... ./internal/rag/...`）
- [ ] 静态检查无告警（验证：`go vet ./...`）

## 端到端场景

- [ ] 场景 1（核心链路）：对同一 session 连续提问两次，第二次问题含指代（如「它支持哪些格式？」），回答基于检索资料且带引用来源（验证：运行 `go test ./internal/rag/` 多轮场景；或小型示例程序驱动 mock 观察日志中的改写查询、上下文 token 数、引用数量）
- [ ] 场景 2（流式）：发起 StreamAsk，事件顺序为 引用来源 → 文本增量 → 结束，引用与普通 Ask 一致（验证：engine_test 流式事件序列断言）
- [ ] 场景 3（降级）：检索返回空时得到「未找到相关资料」类回答，无 panic、无 LLM 调用（验证：engine_test 空结果场景）
- [ ] 场景 4（历史容量）：session 消息数超过容量上限后，最旧消息被丢弃，`Get` 返回最近 limit 条（验证：history_test 容量测试）
- [ ] 场景 5（错误路径）：mock LLM 先 500 后 200，Ask 成功（重试生效）；mock 一直 400，Ask 返回明确错误（验证：llm_test 重试与错误场景）

## 与 spec 验收标准对照

- [ ] AC1-AC4（生成/流式/配置/重试）→ 见「实现完整性」与「端到端场景 5」
- [ ] AC5-AC11（改写/编排/流式 RAG/截断/历史/降级/模板）→ 见「集成」与「端到端场景 1-4」
- [ ] AC12（可观测：各阶段耗时日志）→ 运行 engine_test 带日志输出，观察改写/检索/组装/生成耗时行
- [ ] AC13（race）→ 见「编译与测试」第 3 项
- [ ] AC14（build + test）→ 见「编译与测试」第 1、2 项
