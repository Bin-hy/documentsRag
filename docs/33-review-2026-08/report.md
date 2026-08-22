# BinRag 代码 Review 报告（2026-08-15）

> 来源：全量代码 review（4 个并行模块检查 + 编译/vet/测试验证）。
> 验证基线：`go build ./...` 通过、`go vet ./...` 无告警、`go test ./internal/...` 全绿、前端 `tsc --noEmit` 零错误、24 个 vitest 用例全绿。

## 总体结论

项目完成度高、工程质量中上。34 条 API 路由全部有真实实现，无空壳 handler；认证鉴权挂载正确，无裸奔接口。主要问题集中在：少数实质 bug、配置项未接线、若干架构债。

---

## P0 — 需要立即修的实质 bug

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| P0-1 | 前端 XSS 风险 | `frontend/src/components/MarkdownRenderer.vue:38` | `marked.parse` 输出直接 `v-html`，无 DOMPurify 消毒。恶意文档可注入脚本，存储型 XSS |
| P0-2 | ChatStream nil engine panic | `internal/api/handler_chat.go:156` | 热重载失败窗口期 `h.engine()` 可能为 nil，`Chat` 有判断但 `ChatStream` 没有 |
| P0-3 | eval 忠实度指标无效 | `internal/eval/evaluator.go:191` | `JudgeFaithfulness` sources 传 nil，忠实度永远在评判空资料 |
| P0-4 | eval sessionID 冲突 | `internal/eval/evaluator.go:164` | 问题前 20 字节做 sessionID，前缀相同样本历史互污；中文截断可能出非法 UTF-8 |
| P0-5 | eval 并发无 recover | `internal/eval/evaluator.go:201` | 注释声称捕获 panic 但代码没有，单样本 panic crash 整个评测 |
| P0-6 | 启动失败路径资源泄漏 | `internal/app/app.go:174` | `webui.Register` 失败时漏 `vs.Close()`，Qdrant 连接泄漏 |

## P1 — 配置/代码「声明了但没接线」

| # | 项 | 位置 | 说明 |
|---|----|------|------|
| P1-1 | `reranker.top_n` 未生效 | `internal/config/config.go:105` | 配置/热更新齐全，reranker 实现从不读 |
| P1-2 | `web_search.qps` 未生效 | `internal/config/config.go:39` | bocha provider 无限流器 |
| P1-3 | `embedder.provider` 未生效 | `internal/config/config.go:119` | `NewEmbedder` 无视 provider 字段 |
| P1-4 | `hyde_skip_simple` / `shouldHyde` 死代码 | `internal/rag/routing.go:49` | 判断函数无调用方，配置不生效 |
| P1-5 | `PerQueryTrace.Method` 永远空串 | `internal/retriever/types.go:25` | SearchMulti 填充时不填 |
| P1-6 | `search.Result.Content` 永不填充 | `internal/search/bocha.go:138` | 消费路径存在但 provider 不填 |
| P1-7 | `App.engine` 死字段 | `internal/app/app.go:44` | 热重载后持有旧引擎，无读取方 |
| P1-8 | `app.rebuildComponents` 未被使用 | `internal/app/rebuild.go:57` | Rebuild 闭包内联相同逻辑，重复代码会漂移 |
| P1-9 | `cmd/desktop` 与 `cmd/server` 的 `parseConfigFlag` 重复 | `cmd/desktop/main.go:93` | desktop 复制了 `app.ParseConfigFlag` 逻辑 |

## P1 — spec 承诺 vs 实现的 gap

| # | spec 条目 | 位置 | 说明 |
|---|-----------|------|------|
| GAP-1 | spec 30 F5：ASR 时间戳保留 | `internal/multimedia/dashscope_speech.go:154` | dashscope qwen ASR 与无 verbose_json 的 whisper 端点返回整段文本，时间戳全 0，视频定位锚点失效，文档未声明降级 |
| GAP-2 | spec 30 N5：损坏音轨记 warning | `internal/multimedia/audio_extractor.go:45` | 无法区分「无音轨」与「ffmpeg 失败」，损坏音轨静默跳过 |

## P1 — 数据一致性 / 并发

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| P1-10 | pipeline 向量入库与 BM25 更新无原子性 | `internal/pipeline/pipeline.go:130` | 重试/崩溃产生孤儿 chunk |
| P1-11 | task worker 重试无退避 | `internal/task/worker.go:148` | 持续错误密集打满外部服务 |
| P1-12 | Shutdown 不中断长任务 | `internal/task/worker.go:100` | `WithoutCancel` 导致视频 VLM 等长任务无法被关停 |
| P1-13 | `ingest_tasks` 表无外键 | `internal/store/schema.go:29` | 删 KB 后任务成孤儿 |
| P1-14 | Swagger 文档过时 | `internal/api/docs/docs.go` | 8 条路由缺失（supported-types/raw/video stream/mcp/my/*），需重跑 `swag init`；`BearerAuth` 未定义 |

## P2 — 架构债

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| A-1 | `store.Store` 25+ 方法胖接口 | `internal/store/store.go:113` | mock 成本高，应按实体拆分 |
| A-2 | `RAGEngine` 1114 行职责膨胀 | `internal/rag/engine.go` | Ask/StreamAsk 策略分流段近乎逐行重复 |
| A-3 | 两套 web 搜索抽象并存 | `internal/search` vs `internal/datasource` | search.Provider 真实实现 + datasource.webSearchSource 占位，概念重叠 |
| A-4 | CORS 全放开 `*` 无配置项 | `internal/api/middleware.go:102` | 企业级系统应可配置允许源 |
| A-5 | `jwt_secret` 不显式配置时重启会话全失效 | `internal/auth/jwt.go:30` | 生产部署隐患，文档提示不够醒目 |

## P2 — 性能隐患

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| PERF-1 | fixed/recursive 分块 token 计数 O(n²) | `internal/chunker/strategy_fixed.go:68` | 每次重新构造字符串计数 |
| PERF-2 | scene 检测逐帧串行 embedding | `internal/multimedia/scene_sampler.go:44` | 长视频 1200+ 次串行 HTTP 调用 |
| PERF-3 | LLM reranker 逐候选串行打分 | `internal/reranker/reranker_llm.go:92` | TopK=20 时 20 次串行调用 |
| PERF-4 | embedding 响应不校验向量数量 | `internal/embedding/embedder.go:152` | 缺失时静默返回 nil 向量 |

## P2 — 前端问题

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| FE-1 | kb/doc store `load()` 无 catch | `frontend/src/stores/kb.ts:13`、`doc.ts:26` | 加载失败静默无提示；轮询内失败产生 unhandled rejection |
| FE-2 | 视频查看器整文件下载 | `frontend/src/components/viewers/VideoViewer.vue:23` | 未用后端 `/videos/:id/stream` 流式端点 |
| FE-3 | 会话索引仅存 localStorage | `frontend/src/stores/chat.ts:7` | 跨设备/清缓存后历史会话无法找回 |
| FE-4 | 无 404 catch-all 路由 | `frontend/src/router/index.ts` | 访问不存在路径渲染空布局 |
| FE-5 | 多文件串行上传中途失败全中断 | `frontend/src/stores/doc.ts:40` | 一个失败后续全部中断 |

## P2 — 其他

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| MISC-1 | `handler_config.go` 手写 `intToString` | `internal/api/handler_config.go:330` | 应用 `strconv.Itoa` |
| MISC-2 | `retryableError` 无 `Unwrap()` | `internal/reranker/reranker.go:201` | 与 llm/embedding 的 `RetryableError` 不一致 |
| MISC-3 | QPS=0 时限流器永久阻塞 | `internal/llm/llm.go:128`、`internal/embedding/embedder.go:32` | 仅 reranker 做了 qps≤0 防御 |
| MISC-4 | `parseHostPort` 忽略解析错误 | `internal/vectorstore/qdrant.go:253` | 非法配置静默回退 6334 无告警 |
| MISC-5 | `buildFilter` 未知类型静默丢弃 | `internal/vectorstore/qdrant.go:194` | 过滤条件悄悄失效无日志 |

---

## 修复优先级建议

1. **第一批（P0 全部）**：6 个实质 bug，多为一行级修复
2. **第二批（P1 配置接线）**：P1-1 ~ P1-4 接线或删除，P1-14 重跑 swag init
3. **第三批（P1 一致性 + gap）**：P1-10 ~ P1-13、GAP-1/2
4. **第四批（P2 架构债 + 性能 + 前端）**：排期重构
