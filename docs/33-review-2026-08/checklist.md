# Review 修复（P0 + 配置接线）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] F1 前端 XSS 消毒生效（验证：构造含 `<script>alert(1)</script>` 的 Markdown 字符串，经 MarkdownRenderer 渲染后输出中无 `<script>` 标签；正常 Markdown 代码块/表格/链接渲染不受影响）
- [ ] F2 ChatStream nil engine 防护（验证：engine 为 nil 时调用流式接口，返回统一错误格式 `{code, message}`，不 panic）
- [ ] F3 eval 忠实度指标修复（验证：`JudgeFaithfulness` 收到非空 sources 参数，忠实度判定基于真实检索内容）
- [ ] F4 eval sessionID 唯一（验证：两个前 20 字相同的问题样本产生不同 sessionID；中文问题截断后无非法 UTF-8）
- [ ] F5 eval panic 捕获（验证：构造 panic 样本，评测进程不 crash，该样本标记为失败，其余样本正常完成）
- [ ] F6 启动失败资源清理（验证：代码审查确认 `webui.Register` 失败路径有 `vs.Close()` 调用）
- [ ] F7 reranker.top_n 生效（验证：配置 `reranker.top_n: 3`，rerank 结果最多 3 条）
- [ ] F8 web_search.qps 生效（验证：配置 `web_search.qps: 1`，连续两次搜索间隔 ≥ 1s）
- [ ] F9 embedder.provider 生效（验证：`provider: openai` 正常工作；`provider: unknown` 返回明确错误「未知 embedding provider」）
- [ ] F10 hyde_skip_simple 生效（验证：`hyde_skip_simple: true` 时简单查询不触发 HyDE；`false` 时正常触发）
- [ ] F11 Swagger 文档同步（验证：swagger UI 或 docs.go 中包含 supported-types、raw、video stream、mcp/my/* 共 8 条路由；`BearerAuth` 有定义）

## 集成

- [ ] `NewEmbedder` 签名变化后所有调用点正确处理 error（验证：`go build ./...` 编译通过）
- [ ] `shouldHyde` 被 engine.go 实际调用（验证：grep 确认 `shouldHyde` 有调用方，不再是死代码）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...` 退出码 0）
- [ ] `go vet ./...` 无告警
- [ ] 所有 Go 单元测试通过（验证：`go test ./internal/...` 全绿）
- [ ] 前端类型检查零错误（验证：`cd frontend && npx vue-tsc --noEmit`）
- [ ] 前端测试全绿（验证：`cd frontend && npx vitest run`）

## 端到端场景

- [ ] 场景 1：上传一个含恶意 Markdown（嵌入 `<script>`）的文档到知识库，提问触发检索到该文档，前端渲染回答时不执行脚本（验证：浏览器 devtools 无 script 执行，页面无 alert 弹出）
- [ ] 场景 2：修改配置 `reranker.top_n: 2`，热重载后提问，观察返回的引用来源数量 ≤ 2（验证：API 响应中 sources 数组长度 ≤ 2）
