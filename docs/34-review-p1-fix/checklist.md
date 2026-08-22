# Review 修复第二轮（P1 剩余）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] F1 PerQueryTrace.Method 接线（验证：`SearchMulti` 返回的 trace 中 Method 非空，记录 vector/hybrid）
- [ ] F2 App.engine 死字段删除（验证：`App` 结构体无 `engine` 字段，`go build` 通过）
- [ ] F3 Rebuild 闭包去重（验证：`Rebuild` 赋值为 `a.rebuildComponents`，无内联重复逻辑）
- [ ] F4 parseConfigFlag 去重（验证：`cmd/desktop/main.go` 无独立 `parseConfigFlag`，调用 `app.ParseConfigFlag`）
- [ ] F5 pipeline 重试补偿（验证：同一文档重复入库后，向量库中该文档 chunk 数量不膨胀）
- [ ] F6 worker 重试退避（验证：连续失败时日志中重试间隔递增）
- [ ] F7 删 KB 清理任务（验证：删除 KB 后查询 ingest_tasks 表无关联记录）
- [ ] F8 dashscope 时间戳声明（验证：config.go 注释和 spec 30 文档包含降级声明）
- [ ] F9 损坏音轨区分（验证：损坏音轨文件入库 warning 含「音轨提取失败」；无音轨文件无此 warning）

## 集成

- [ ] `VectorStore.DeleteByFilter` 接口有真实实现且被 pipeline 调用（验证：编译 + pipeline 测试通过）
- [ ] `BM25Index.RemoveByDoc` 接口有真实实现且被 pipeline 调用（验证：编译 + retriever 测试通过）
- [ ] `ClaimPendingTasks` 的退避条件不影响正常任务认领（验证：store 测试通过）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...` 退出码 0）
- [ ] `go vet ./...` 无告警
- [ ] 所有 Go 单元测试通过（验证：`go test ./internal/...` 全绿）

## 端到端场景

- [ ] 场景 1：上传同一文档两次（模拟重试），第二次入库后检索结果不重复（验证：检索返回的 chunk 数量与首次入库一致）
- [ ] 场景 2：删除一个含已完成任务的 KB，查询数据库确认关联 ingest_tasks 已清理（验证：SQL 查询无残留）
