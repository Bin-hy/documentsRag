# Review 修复第二轮（P1 剩余）Spec

## 背景

第一轮修复（`docs/33-review-2026-08/`）已完成 P0 + 配置接线。review 报告中仍有 11 项 P1 问题未处理，分为三类：
- 死代码/重复代码：P1-5（PerQueryTrace.Method 空串）、P1-6（search.Result.Content 不填）、P1-7（App.engine 死字段）、P1-8（rebuildComponents 未使用）、P1-9（parseConfigFlag 重复）
- 数据一致性：P1-10（pipeline 无原子性）、P1-11（worker 重试无退避）、P1-13（ingest_tasks 孤儿任务）
- spec gap：GAP-1（dashscope ASR 时间戳丢失）、GAP-2（损坏音轨无法区分）

## 目标

- 清理死代码和重复代码，消除误导性残留
- 增强 pipeline 和 worker 的数据一致性与容错能力
- 修复 spec 承诺与实现之间的 gap
- 所有修改不破坏现有功能，全部测试保持绿色

## 功能需求

### 死代码/重复代码清理

- F1: `PerQueryTrace.Method` 接线 — `SearchMulti` 填充每路检索的实际方法（vector/hybrid），思考链路可追溯
- F2: `App.engine` 死字段删除 — 移除无读取方的字段，消除误导
- F3: `app.rebuildComponents` 与 Rebuild 闭包去重 — 闭包改为调用 `a.rebuildComponents()`，消除重复逻辑
- F4: `cmd/desktop` 的 `parseConfigFlag` 去重 — 改为调用 `app.ParseConfigFlag`，删除复制代码

### 数据一致性

- F5: pipeline 重试补偿 — 重复入库同一文档时，先按 document_id 清理旧 chunk（向量 + BM25），再执行新入库，避免孤儿向量
- F6: worker 重试加指数退避 — 失败后延迟 1s/2s/4s...再回 pending，避免密集打满外部服务
- F7: 删 KB 时手动清理关联任务 — `DeleteKB` 级联删 documents 后，显式删除关联 ingest_tasks，避免孤儿任务（不加外键，保持分库分表扩展性）

### spec gap 修复

- F8: dashscope ASR 时间戳降级声明 — 在配置注释和 spec 30 文档中声明「dashscope qwen ASR 不返回时间戳，该 provider 下音频 chunk 时间戳为 0，定位锚点不可用」
- F9: 损坏音轨区分 — ffmpeg 拆流失败时检查 stderr，区分「无音轨」（静默跳过）与「提取失败」（记 warning 透传）

## 非功能需求

- N1: 所有修改不破坏现有功能 — `go build`、`go vet`、`go test ./internal/...` 全部保持绿色
- N2: worker 退避不影响正常任务处理速度 — 仅失败重试时退避，正常流程不增加延迟
- N3: pipeline 补偿逻辑幂等 — 多次重试同一文档不产生重复 chunk
- N4: 修复遵循项目现有代码风格和中文注释约定

## 不做的事

- 不做 P2 架构债（胖接口拆分、RAGEngine 瘦身、两套 web 搜索抽象合并）
- 不做 P2 性能优化（O(n²) 分块、串行 embedding、LLM reranker 串行打分）
- 不做前端剩余问题（store catch、视频流式、会话恢复、404 路由）
- 不做 CORS 可配置化、jwt_secret 文档强化
- 不改 `search.Result.Content` 字段（保留，未来 bocha 模式 B 抓取时填充）
- 不改 Shutdown 不中断长任务的行为（有意设计）

## 验收标准

- AC1（对应 F1）：`SearchMulti` 返回的 `PerQueryTrace.Method` 非空，记录实际检索方式
- AC2（对应 F2）：`App` 结构体无 `engine` 字段，编译通过
- AC3（对应 F3）：`Rebuild` 闭包调用 `a.rebuildComponents()`，无内联重复逻辑
- AC4（对应 F4）：`cmd/desktop/main.go` 中无独立 `parseConfigFlag` 函数，调用 `app.ParseConfigFlag`
- AC5（对应 F5）：同一文档重复入库后，向量库中该文档的 chunk 数量不膨胀（旧 chunk 被清理）
- AC6（对应 F6）：worker 连续失败时，重试间隔递增（日志可见退避时间）
- AC7（对应 F7）：删除 KB 后，关联 ingest_tasks 记录不再存在
- AC8（对应 F8）：配置注释/文档中明确声明 dashscope ASR 时间戳限制
- AC9（对应 F9）：损坏音轨的文件入库时 warning 中包含「音轨提取失败」信息；无音轨文件无此 warning
- AC10（对应 N1）：`go build ./... && go vet ./... && go test ./internal/...` 全绿
