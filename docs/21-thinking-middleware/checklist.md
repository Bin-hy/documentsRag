# 思考链路中间件 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] C1（AC1）: 开启 thinking 后，流式请求的 SSE 事件流出现 `thinking` 事件，且全部位于 `sources` 之前（验证：跑 engine 测试断言事件顺序；或起服务用 curl 观察事件序）
- [ ] C2（AC2）: 同一问题分别走流式与非流式，两者思考链路内容一致（验证：engine 测试中 Ask 与 StreamAsk 对同一 fake LLM 输入断言步骤序列相同）
- [ ] C3（AC3）: Routing=auto 且判定成功时，thinking 含复杂度/策略/reasoning（验证：测试断言 StepRouting 的 Data 字段非空）
- [ ] C4（AC4）: Multi-Query 路径 thinking 含全部变体（含原问题）；单查询改写路径含 original/rewritten；改写失败时 fallback=true（验证：测试断言 StepMultiQuery/StepRewrite 载荷）
- [ ] C5（AC5）: Multi-Query 多路检索逐路展示召回数，融合结果数与最终一致（验证：测试断言 StepRetrieval.PerQuery 长度与 queries 数相等、Recalled 与融合结果一致）
- [ ] C6（AC6）: EnableReranker=true 时 thinking 含 rerank 前后对比；关闭时无 StepRerank（验证：retriever 测试断言 Trace.RerankBefore/After；引擎测试断言步骤集合）
- [ ] C7（AC7）: 目标 chunks 展示与最终 `sources` 同 chunk 集合（验证：测试断言 ChunksData 的 chunk id 集合 == sources 的 id 集合）
- [ ] C8（AC8）: Decomposition 路径展示子问题+逐子问题检索；Step-Back 展示回退问题；HyDE 展示假设文档；未走策略不出现（验证：各策略路径的引擎测试断言对应步骤存在/缺失）
- [ ] C9（F8）: 非流式 `/api/v1/chat` 响应 `data.thinking` 存在且结构完整（验证：API 测试断言 JSON 字段）
- [ ] C10（F9/N2）: thinking=off 时：流式无 thinking 事件、非流式无 thinking 字段、行为与旧版一致（验证：关闭态测试 + 现有测试全绿）

## 集成

- [ ] C11（AC9）: 关闭开关后行为与旧版一致：无 thinking 事件、无 thinking 字段、主链路测试全部通过（验证：`go test ./...` 全绿 + 关闭态行为断言）
- [ ] C12（AC10）: 长文本（chunk 内容）前端可展开查看；HyDE 假设文档服务端截断（验证：前端组件测试 + 后端测试断言截断长度 ≤500）
- [ ] C13（N4）: thinking 不写入历史：历史接口返回的消息无 thinking 字段（验证：history 相关测试 + 手动调用历史接口观察）
- [ ] C14（N6）: 现有 SSE 事件（sources/chunk/done/error）语义不变，前端旧逻辑兼容（验证：现有 chat.test.ts 全绿）
- [ ] C15（AC11）: 多路并行检索时 PerQuery 顺序与传入 queries 一致、多次请求结构稳定（验证：retriever 测试断言顺序）

## 编译与测试

- [ ] C16: 项目编译无错误（验证：`go build ./...`）
- [ ] C17: 后端全部单元测试通过（验证：`go test ./...`）
- [ ] C18: 前端类型检查通过（验证：`cd frontend && npx tsc --noEmit`）
- [ ] C19: 前端测试与构建通过（验证：`cd frontend && npx vitest run && npm run build`）
- [ ] C20: gofmt/vet 检查通过（验证：`gofmt -l .` 无输出、`go vet ./...`）

## 端到端场景

- [ ] E1（主流程）: 起服务 → 配置 thinking=on → 发 Multi-Query 流式问答 → 观察：先收到若干 thinking 事件（改写变体 → 多路检索召回 → rerank 对比 → 目标 chunks），再收到 sources 与回答；前端消息气泡上方出现可折叠「思考过程」面板，展开可见各环节，chunk 内容可展开（验证：`go run ./cmd/server` + 前端 `npm run dev` 手动走通）
- [ ] E2（策略路径）: 配置 routing=auto + decomposition 可用，发一个复杂问题 → thinking 面板按实际路径展示路由判定/子问题/逐子问题检索；未走的环节不出现（验证：手动观察面板）
- [ ] E3（关闭降级）: 配置 thinking=off → 对话与旧版完全一致，无思考面板（验证：手动观察 + 响应无 thinking 字段）
