# Review 修复第二轮（P1 剩余）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/retriever/retriever.go` | SearchMulti 填充 PerQueryTrace.Method |
| 修改 | `internal/app/app.go` | 删 engine 死字段；Rebuild 闭包调 rebuildComponents |
| 修改 | `cmd/desktop/main.go` | 删 parseConfigFlag，调 app.ParseConfigFlag |
| 修改 | `internal/vectorstore/store.go` | VectorStore 接口加 DeleteByFilter |
| 修改 | `internal/vectorstore/qdrant.go` | 实现 DeleteByFilter |
| 修改 | `internal/retriever/bm25.go` | BM25Index 接口加 RemoveByDoc + 实现 |
| 修改 | `internal/pipeline/pipeline.go` | Ingest 开头加旧 chunk 清理 |
| 修改 | `internal/task/worker.go` | fail 加指数退避 |
| 修改 | `internal/store/task.go` | ClaimPendingTasks 加 updated_at <= NOW() |
| 修改 | `internal/store/kb.go` | DeleteKB 先删关联 ingest_tasks |
| 修改 | `internal/multimedia/audio_extractor.go` | Extract 区分无音轨/提取失败 |
| 修改 | `internal/config/config.go` | dashscope ASR 注释声明 |
| 修改 | `docs/30-视频处理增强-spec.md` | 补充 dashscope 时间戳降级声明 |

## T1: PerQueryTrace.Method 接线

**文件：** `internal/retriever/retriever.go`
**依赖：** 无
**步骤：**
1. `SearchMulti` 中 `results` 切片旁加 `methods := make([]string, len(queries))`
2. goroutine 内 `out, _, serr` 改为 `out, method, serr`，成功时 `methods[i] = method`
3. 填充 `perQuery[i]` 时加 `Method: methods[i]`

**验证：** `go build ./internal/retriever/... && go test ./internal/retriever/...` 通过

## T2: App.engine 死字段删除 + Rebuild 闭包去重

**文件：** `internal/app/app.go`
**依赖：** 无
**步骤：**
1. 删除 `App` 结构体的 `engine rag.Engine` 字段
2. 删除 `New` 中 `engine` 的赋值
3. `Rebuild` 闭包改为 `Rebuild: a.rebuildComponents`
4. 确认 `rebuildComponents` 签名匹配

**验证：** `go build ./internal/app/...` 编译通过

## T3: cmd/desktop parseConfigFlag 去重

**文件：** `cmd/desktop/main.go`
**依赖：** 无
**步骤：**
1. 删除 `parseConfigFlag` 函数
2. 调用处改为 `app.ParseConfigFlag(os.Args[1:])`
3. 清理不再使用的 import

**验证：** `go build ./cmd/desktop/...` 编译通过

## T4: VectorStore 接口加 DeleteByFilter + Qdrant 实现

**文件：** `internal/vectorstore/store.go`、`internal/vectorstore/qdrant.go`
**依赖：** 无
**步骤：**
1. `VectorStore` 接口加 `DeleteByFilter(ctx context.Context, filter map[string]any) error`
2. `qdrant.go` 实现：用 Qdrant DeletePoints + filter（复用 buildFilter）
3. 检查是否有其他 VectorStore 实现需要同步

**验证：** `go build ./internal/vectorstore/...` 编译通过

## T5: BM25Index 接口加 RemoveByDoc + 实现

**文件：** `internal/retriever/bm25.go`
**依赖：** 无
**步骤：**
1. `BM25Index` 接口加 `RemoveByDoc(docID string)`
2. 实现：遍历索引找到 docID 匹配的所有 chunk ID，逐个 Remove

**验证：** `go build ./internal/retriever/... && go test ./internal/retriever/...` 通过

## T6: pipeline 重试补偿

**文件：** `internal/pipeline/pipeline.go`
**依赖：** T4、T5
**步骤：**
1. `Ingest` 函数开头（Load 之前）加清理逻辑
2. `p.vectorstore.DeleteByFilter(ctx, map[string]any{"document_id": req.DocumentID})`，失败仅 warn
3. `p.bm25Index.RemoveByDoc(req.DocumentID)`
4. 加中文注释说明补偿意图

**验证：** `go build ./internal/pipeline/... && go test ./internal/pipeline/...` 通过

## T7: worker 重试退避

**文件：** `internal/task/worker.go`、`internal/store/task.go`
**依赖：** 无
**步骤：**
1. `fail` 方法中重试分支计算退避 `backoff := time.Second << uint(t.RetryCount)`
2. 更新 task 时设 `t.UpdatedAt = time.Now().Add(backoff)`
3. `ClaimPendingTasks` SQL 的 WHERE 加 `AND updated_at <= NOW()`

**验证：** `go build ./internal/task/... ./internal/store/... && go test ./internal/task/... ./internal/store/...` 通过

## T8: 删 KB 清理关联任务

**文件：** `internal/store/kb.go`
**依赖：** 无
**步骤：**
1. `DeleteKB` 中先执行 `DELETE FROM ingest_tasks WHERE kb_id = $1`
2. 再执行原有的 `DELETE FROM knowledge_bases WHERE id = $1`
3. 更新函数注释

**验证：** `go build ./internal/store/... && go test ./internal/store/...` 通过

## T9: 损坏音轨区分

**文件：** `internal/multimedia/audio_extractor.go`
**依赖：** 无
**步骤：**
1. ffmpeg 失败后检查 stderr 关键词
2. 含 "no audio" / "does not contain any stream" 等 → 返回空路径 + nil
3. 否则 → 返回空路径 + fmt.Errorf("音轨提取失败: %w", err)
4. 确认调用方 parser_video.go 对 error 记 warning

**验证：** `go build ./internal/multimedia/... && go test ./internal/multimedia/...` 通过

## T10: dashscope 时间戳声明

**文件：** `internal/config/config.go`、`docs/30-视频处理增强-spec.md`
**依赖：** 无
**步骤：**
1. config.go 中 dashscope ASR 配置注释加时间戳限制声明
2. spec 30 文档 F5 条目补充同样声明

**验证：** 文本确认

## 执行顺序

```
T1（retriever）─┐
T2（app）      ─┤
T3（desktop）  ─┤
T4（vectorstore）─┤
T5（bm25）     ─┼─ 可并行
T7（worker+store task）─┤
T8（store kb） ─┤
T9（multimedia）─┤
T10（config+docs）─┘
     │
     ▼
T6（pipeline，依赖 T4+T5）
```
