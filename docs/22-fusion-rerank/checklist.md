# 融合后整体重排 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] C1（AC1/F5）: 多查询请求（`query: multi`）最终 `sources` 顺序与 rerank 分数一致（降序），不按 RRF 融合分数排序（验证：测试断言 mock reranker 重排后顺序反映在返回结果）
- [ ] C2（AC2/N1）: 多查询请求 rerank API 调用次数为 1（验证：mockReranker 调用计数断言）
- [ ] C3（AC3/F2）: HyDE 双路融合后整体 rerank 一次（验证：HyDE 测试断言调用计数 + 顺序）
- [ ] C4（AC4/F3/F4）: 分解/回退路径汇总后整体 rerank，进入上下文顺序与 rerank 分数一致（验证：策略路径测试断言）
- [ ] C5（AC5/F6）: `query: single` 请求 rerank 调用次数与顺序与改动前一致（验证：现有多路/单路测试全绿 + 计数断言）
- [ ] C6（AC6/F8）: 整体 rerank 失败（mock 报错）时多路返回融合结果不报错（验证：降级测试）
- [ ] C7（AC7/F7）: 多路 thinking 含单个 `StepRerank`（Before=融合结果、After=整体重排结果），逐路检索无 rerank 步骤（验证：thinking 断言）

## 集成

- [ ] C8: `Rerank` 接口被 rag 层三个调用点（hydeSearch/tryDecompose/tryStepBack）真实使用（验证：编译 + 测试覆盖）
- [ ] C9: `SkipRerank` 在 hydeSearch 原查询路、searchSubQuery 单路处生效（验证：SkipRerank 测试断言 mock 调用计数为 0）
- [ ] C10: 现有 SSE/思考链路事件语义不变（验证：现有 thinking/SSE 测试全绿）

## 编译与测试

- [ ] C11: `go build ./...` 无错误
- [ ] C12: `go test ./...` 全绿
- [ ] C13: race 检测通过（验证：`go test -race ./internal/retriever/... ./internal/rag/...`）

## 端到端场景

- [ ] E1（多路重排）: 起服务（`enable_reranker: true` + `query: multi`）→ 发多角度问题 → 观察思考面板：多查询改写 → 逐路检索（无 rerank）→ 融合 → rerank（Before=融合结果、After=整体重排）→ 目标片段；回答引用顺序与 rerank 分数一致（验证：日志/面板观察）
- [ ] E2（降级）: 配置 reranker 指向不可用地址 → 发多路请求 → 仍返回融合结果与回答，主链路不阻塞（验证：观察回答正常 + 日志告警）
