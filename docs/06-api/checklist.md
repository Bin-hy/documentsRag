# API 层与知识库管理 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性

- [ ] RESTful 服务可启动（验证：`go build ./...` + `cmd/server` 装配编译通过；启动打印端口/worker 数/存储目录）
- [ ] 知识库 CRUD 接口已实现且可被调用（验证：api_test 中创建/列表/查询/更新/删除用例通过）
- [ ] 文档上传返回 task_id（验证：api_test 上传用例断言响应含 task_id 与 document_id）
- [ ] 异步入库任务状态机可用（验证：worker_test 中 pending→processing→completed / failed 转换断言通过）
- [ ] 问答接口返回回答与引用来源（验证：api_test 断言 {answer, sources} 结构）
- [ ] SSE 流式问答可用（验证：api_test 断言事件序列 sources→chunk→done）
- [ ] 对话历史可查询（验证：api_test 断言按 session 取回消息、顺序正确）
- [ ] API Key 管理可用（验证：api_test 断言创建 Key 返回明文一次、列表/删除/toggle 生效）

## 集成

- [ ] 上传→入库→列表 全链路正确（验证：api_test 端到端场景 1 通过，任务最终 completed、文档出现在列表）
- [ ] 文档删除正确清理向量与索引（验证：api_test 断言删除时按 chunk_ids 调用删除逻辑；集成环境验证检索不再命中）
- [ ] 知识库隔离生效（验证：api_test 或 retriever_test 断言检索/问答仅命中指定 kb_id 范围内容）
- [ ] 任务失败重试闭环（验证：worker_test 断言失败未超限回 pending、手动重试后 completed）
- [ ] 对话历史持久化到 PostgreSQL（验证：store_test 断言 PostgresHistoryStore Get 最近 limit 条、Append 插入）
- [ ] 所有公开接口至少被一个真实调用方使用（验证：`go test ./...` 全部通过 + `go vet ./...` 无告警）

## 编译与测试

- [ ] 项目编译无错误（验证：`go build ./...`）
- [ ] 所有单元测试通过（验证：`go test ./...`）
- [ ] 无数据竞争（验证：`go test -race ./internal/store/... ./internal/task/... ./internal/api/...`）
- [ ] 静态检查无告警（验证：`go vet ./...`）
- [ ] 依赖干净（验证：`go mod tidy` 后 `go mod verify` 无异常）

## 端到端场景

- [ ] 场景 1（上传→入库→问答）：创建知识库 → 上传文档 → 轮询任务至 completed → 文档出现在列表 → 带 kb 范围问答返回回答与引用来源（验证：api_test 完整链路用例；真实 Qdrant+Postgres 环境可再跑集成场景）
- [ ] 场景 2（SSE 流式）：发起流式问答，事件顺序为 sources → chunk×N → done，内容与普通问答一致（验证：api_test 流式断言）
- [ ] 场景 3（知识库隔离）：两个知识库各上传内容不同的文档，指定库检索/问答不混入另一库内容（验证：retriever_test kb 过滤用例 + api_test 传 kb_id 用例）
- [ ] 场景 4（认证）：无 Key / 错误 Key / 停用 Key 请求返回 401；正确 Key 正常访问（验证：api_test 认证用例）
- [ ] 场景 5（失败与重试）：上传不支持格式文件 → 任务 failed 且 error_message 可查、其他任务不受影响；手动重试后 completed（验证：api_test 或 worker_test 失败/重试用例）
- [ ] 场景 6（删除文档）：删除文档后文档不在列表，且其 chunk 不再出现在该库检索结果（验证：api_test 删除用例；检索部分依赖集成环境）

## 与 spec 验收标准对照

- [ ] AC1（知识库创建后可见）→ 场景 1 + 实现完整性第 2 项
- [ ] AC2（上传→completed→列表→检索命中）→ 场景 1
- [ ] AC3（坏文件 failed 不影响其他）→ 场景 5
- [ ] AC4（删除后不命中）→ 场景 6
- [ ] AC5（知识库隔离）→ 场景 3
- [ ] AC6（问答 + SSE）→ 场景 1 / 场景 2
- [ ] AC7（对话历史）→ 实现完整性第 7 项 + 集成第 5 项
- [ ] AC8（认证）→ 场景 4
- [ ] AC9（统一响应）→ api_test 断言所有响应含 code/message/data 字段
- [ ] AC10（任务重试）→ 场景 5
- [ ] AC11（重启恢复）→ worker_test 断言 ResetProcessingTasks 在 Start 时被调用；任务状态持久化见集成第 5 项
- [ ] AC12（并发无竞争）→ 编译与测试第 3 项
- [ ] AC13（build + test）→ 编译与测试第 1、2 项
