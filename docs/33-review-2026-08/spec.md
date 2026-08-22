# Review 修复（P0 + 配置接线）Spec

## 背景

2026-08-15 的全量代码 review（报告见 `docs/33-review-2026-08/report.md`）发现 6 个实质 bug 和 4 个配置项「声明了但没接线」。这些问题影响系统安全（XSS）、稳定性（nil panic、资源泄漏）、评测正确性（eval 三处 bug）、以及配置与行为一致性（4 个配置项存在但不生效）。

## 目标

- 修复 6 个 P0 级实质 bug，消除安全和稳定性隐患
- 接线 4 个配置项，使系统行为与配置一致
- 重新生成 Swagger 文档，使 API 文档与实际路由同步
- 所有修复不破坏现有功能，全部测试保持绿色

## 功能需求

### P0 修复

- F1: 前端 Markdown 渲染消毒 — 对 LLM 输出的 Markdown HTML 进行 XSS 消毒，阻止 `<script>`、事件属性等注入
- F2: ChatStream nil engine 防护 — 流式接口在 engine 为 nil 时返回统一错误响应而非 panic
- F3: eval 忠实度指标修复 — 评测时忠实度判定使用真实的检索来源，而非空资料
- F4: eval sessionID 唯一性 — 每个评测样本使用唯一 sessionID，避免历史互污；截断按字符而非字节
- F5: eval 并发 panic 捕获 — 单样本 panic 被捕获并标记为失败，不 crash 整个评测进程
- F6: 启动失败资源清理 — `webui.Register` 失败路径补充 Qdrant 连接关闭

### 配置接线

- F7: `reranker.top_n` 生效 — reranker 读取配置值作为默认 topN，调用方未显式指定时使用
- F8: `web_search.qps` 生效 — bocha provider 增加限流器，QPS 受配置控制
- F9: `embedder.provider` 生效 — Embedder 工厂按 provider 字段分发（当前仅 openai 兼容实现，其他值返回明确错误）
- F10: `hyde_skip_simple` 生效 — HyDE 判断逻辑接入 `shouldHyde`，简单查询可跳过 HyDE

### 文档同步

- F11: Swagger 文档重新生成 — 8 条缺失路由（supported-types/raw/video stream/mcp/my/*）补入，定义 `BearerAuth`

## 非功能需求

- N1: 所有修复不破坏现有功能 — `go build`、`go vet`、`go test ./internal/...`、前端 `tsc --noEmit` + vitest 全部保持绿色
- N2: 配置接线向后兼容 — 4 个配置项的默认值行为与修复前一致（不改默认行为，只让显式配置生效）
- N3: XSS 消毒不影响正常 Markdown 渲染 — 代码块、表格、链接等正常渲染，仅剥离危险内容
- N4: 修复遵循项目现有代码风格和注释约定（中文注释）

## 不做的事

- 不做 P1 数据一致性问题（pipeline 原子性、worker 退避、外键）— 留待下一周期
- 不做 P2 架构债（胖接口拆分、RAGEngine 瘦身、重复代码清理）— 留待后续重构
- 不做 P2 性能优化（O(n²) 分块、串行 embedding 等）
- 不做 spec gap（dashscope 时间戳、损坏音轨 warning）
- 不做前端其他问题（store catch、视频流式、会话恢复等）
- 不新增配置项，只接线已有项

## 验收标准

- AC1（对应 F1）：构造含 `<script>alert(1)</script>` 的 Markdown 输入，渲染输出中无 script 标签；正常 Markdown（代码块/表格/链接）渲染不受影响
- AC2（对应 F2）：engine 为 nil 时调用流式接口，返回统一错误格式 `{code, message}`，不 panic
- AC3（对应 F3）：eval 运行时 `JudgeFaithfulness` 收到非空 sources，忠实度评分基于真实检索内容
- AC4（对应 F4）：两个前 20 字相同的问题样本，sessionID 不同；中文问题截断不产生非法 UTF-8
- AC5（对应 F5）：构造一个会 panic 的评测样本，评测进程不 crash，该样本标记为失败
- AC6（对应 F6）：`webui.Register` 失败路径有 `vs.Close()` 调用（代码审查确认）
- AC7（对应 F7）：配置 `reranker.top_n: 3`，rerank 结果最多返回 3 条
- AC8（对应 F8）：配置 `web_search.qps: 1`，连续两次搜索间隔 ≥ 1s
- AC9（对应 F9）：配置 `embedder.provider: openai` 正常工作；配置未知 provider 返回明确错误
- AC10（对应 F10）：`hyde_skip_simple: true` 时，简单查询不触发 HyDE；`false` 时正常触发
- AC11（对应 F11）：重新生成后 swagger 包含 8 条缺失路由，`BearerAuth` 有定义
- AC12（对应 N1）：`go build ./... && go vet ./... && go test ./internal/...` 全绿；前端 `tsc --noEmit` 零错误、vitest 全绿
