# Review 修复第二轮（P1 剩余）Plan

## 架构概览

本次修复分为四个模块组，全部在现有代码内定点修补：

- **retriever 包（F1）**：`SearchMulti` 填充 `PerQueryTrace.Method`
- **app 包 + cmd/desktop（F2/F3/F4）**：删死字段、去重 Rebuild 闭包和 parseConfigFlag
- **pipeline + task + store（F5/F6/F7）**：pipeline 重试补偿、worker 退避、删 KB 清理任务
- **multimedia + config（F8/F9）**：dashscope 时间戳声明、损坏音轨区分

## 核心接口与数据结构变化

### F1: PerQueryTrace.Method 接线

`searchFused` 签名不变（已返回 method），`SearchMulti` 内部补捕获并填充。

### F5: pipeline 重试补偿

```go
// VectorStore 接口新增
DeleteByFilter(ctx context.Context, filter map[string]any) error

// BM25Index 接口新增
RemoveByDoc(docID string)
```

pipeline `Ingest` 开头按 document_id 清理旧 chunk。

### F6: worker 重试退避

不加新字段。`fail` 时计算退避时间（`1s << RetryCount`），将 task 的 `updated_at` 设为 `NOW() + backoff`。`ClaimPendingTasks` 的 WHERE 条件加 `AND updated_at <= NOW()`。

### F7: 删 KB 清理任务

`DeleteKB` 内先 `DELETE FROM ingest_tasks WHERE kb_id = $1`，再删 KB。

### F9: 损坏音轨区分

`Extract` 中 ffmpeg 失败时检查 stderr 关键词：含 "no audio" / "does not contain any stream" 等 → 无音轨（返回空路径 + nil）；否则 → 提取失败（返回 error，调用方记 warning）。

## 模块交互

```
F1: SearchMulti → searchFused（已返回 method）→ 填充 PerQueryTrace.Method
F5: worker 重试 → pipeline.Ingest → 先清理旧 chunk → 正常入库
F6: worker.fail → 计算退避 → updated_at 设为未来时间 → ClaimPendingTasks 跳过未到期
F7: api DeleteKB handler → store.DeleteKB（先删 ingest_tasks 再删 KB）
F9: parser_video → audioExtractor.Extract → 区分无音轨/提取失败 → warning 透传
```

## 文件组织

```
docs-rag/
├── internal/
│   ├── retriever/
│   │   ├── retriever.go              — SearchMulti 填充 PerQueryTrace.Method
│   │   └── bm25.go                   — BM25Index 接口加 RemoveByDoc
│   ├── app/
│   │   ├── app.go                    — 删 engine 死字段；Rebuild 闭包调 rebuildComponents
│   │   └── rebuild.go                — 保持不变
│   ├── cmd/desktop/main.go           — 删 parseConfigFlag，调 app.ParseConfigFlag
│   ├── pipeline/
│   │   └── pipeline.go               — Ingest 开头加旧 chunk 清理
│   ├── vectorstore/
│   │   ├── vectorstore.go            — VectorStore 接口加 DeleteByFilter
│   │   └── qdrant.go                 — 实现 DeleteByFilter
│   ├── task/
│   │   └── worker.go                 — fail 加指数退避
│   ├── store/
│   │   ├── kb.go                     — DeleteKB 先删关联 ingest_tasks
│   │   └── task.go                   — ClaimPendingTasks 加 updated_at <= NOW() 条件
│   ├── multimedia/
│   │   └── audio_extractor.go        — Extract 区分无音轨/提取失败
│   └── config/
│       └── config.go                 — dashscope ASR 注释声明时间戳限制
└── docs/30-视频处理增强-spec.md       — 补充 dashscope 时间戳降级声明
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| pipeline 补偿时机 | Ingest 开头清理 | 重试入口统一，不需要调用方感知 |
| BM25 清理方式 | `RemoveByDoc(documentID)` | 内存索引按 doc 删除比逐个 chunk 删更高效 |
| 向量清理方式 | `DeleteByFilter(document_id)` | Qdrant 支持按 payload 过滤删除，不需要知道旧 chunkID |
| worker 退避实现 | 修改 `updated_at` 为未来时间 | 不加新字段，利用现有 ClaimPendingTasks 的 ORDER BY 语义 |
| 退避策略 | `1s << RetryCount`（1s/2s/4s/8s...） | 简单指数退避，上限由 TaskMaxRetries 控制 |
| 删 KB 清理任务 | 手动 DELETE 不加外键 | 避免分库分表时外键失效 |
| 音轨区分方式 | 检查 ffmpeg stderr 关键词 | 不改接口签名，通过 error 文本区分 |
| dashscope 时间戳 | 文档声明降级 | 上游 provider 限制，代码无法解决 |
