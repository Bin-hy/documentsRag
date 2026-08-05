# API 层与知识库管理 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `go.mod` | 新增 gin、pgx 依赖 |
| 修改 | `internal/config/config.go` | 新增 PostgresConfig、ServerConfig + 默认值 |
| 修改 | `configs/config.yaml` | 新增 server、postgres 配置段 |
| 修改 | `docker-compose.yml` | 新增 postgres 服务 |
| 新建 | `internal/store/store.go` | Store 接口、pgStore、NewStore |
| 新建 | `internal/store/schema.go` | 五张表 DDL + Migrate |
| 新建 | `internal/store/kb.go` | 知识库 CRUD |
| 新建 | `internal/store/document.go` | 文档 CRUD |
| 新建 | `internal/store/task.go` | 任务 CRUD + ClaimPendingTasks + ResetProcessingTasks |
| 新建 | `internal/store/apikey.go` | API Key CRUD + hash 查询 |
| 新建 | `internal/store/history.go` | 对话历史（实现 rag.HistoryStore） |
| 新建 | `internal/store/store_test.go` | store 关键 SQL 测试（pgxmock） |
| 修改 | `internal/pipeline/pipeline.go` | IngestRequest / kb 维度 / 返回 chunkIDs |
| 修改 | `internal/pipeline/pipeline_test.go` | 适配新签名 |
| 修改 | `internal/retriever/bm25.go` | Add 带 kbID、SearchFiltered |
| 修改 | `internal/retriever/retriever.go` | 混合检索传 kb filter |
| 修改 | `internal/retriever/retriever_test.go` | 适配新签名 |
| 新建 | `internal/task/worker.go` | WorkerPool 实现 |
| 新建 | `internal/task/worker_test.go` | worker 状态机测试（fake store） |
| 新建 | `internal/api/response.go` | 统一响应 |
| 新建 | `internal/api/middleware.go` | API Key 认证 / 日志 / CORS / 限流 |
| 新建 | `internal/api/router.go` | 路由注册 |
| 新建 | `internal/api/handler_kb.go` | 知识库 CRUD |
| 新建 | `internal/api/handler_doc.go` | 上传 / 列表 / 删除 |
| 新建 | `internal/api/handler_task.go` | 任务查询 / 重试 |
| 新建 | `internal/api/handler_chat.go` | 问答（普通 + SSE） |
| 新建 | `internal/api/handler_key.go` | API Key 管理 |
| 新建 | `internal/api/handler_history.go` | 对话历史查询 |
| 新建 | `internal/api/api_test.go` | httptest 端到端（fake store + fake engine） |
| 新建 | `cmd/server/main.go` | 装配入口 |

## T1: 依赖与配置扩展

**文件：** `go.mod`、`internal/config/config.go`、`configs/config.yaml`、`docker-compose.yml`
**依赖：** 无
**步骤：**
1. `go get github.com/gin-gonic/gin` 与 `github.com/jackc/pgx/v5`
2. Config 新增 `Postgres PostgresConfig \`yaml:"postgres"\`` 与 `Server ServerConfig \`yaml:"server"\``
3. 定义 `PostgresConfig{DSN string}`、`ServerConfig{Port/FileStorageDir/UploadMaxSizeMB/WorkerCount/TaskMaxRetries/AuthEnabled/BootstrapAPIKey/RateLimitQPS}`
4. applyDefaults：Port=8080、FileStorageDir=./data/uploads、UploadMaxSizeMB=50、WorkerCount=2、TaskMaxRetries=3、AuthEnabled=true、RateLimitQPS=0
5. config.yaml 追加 `server:` 与 `postgres:` 段（带注释，风格与现有段落一致）
6. docker-compose.yml 追加 postgres:16 服务（含 volume、健康检查），并让现有服务可访问

**验证：** `go build ./internal/config/...` 编译通过；`go mod tidy` 无残留

## T2: store 骨架（接口 + 建表）

**文件：** `internal/store/store.go`、`internal/store/schema.go`
**依赖：** T1
**步骤：**
1. 定义 `KnowledgeBase`、`Document`、`Task`、`APIKey` 结构体（字段与 plan.md 一致）
2. 定义 `Store` 接口（全部方法签名照 plan.md）
3. 实现 `pgStore`（持有 pgxpool.Pool）、`NewStore(ctx, dsn) (Store, error)`（pgxpool.New + Ping）
4. schema.go 写五张表 DDL：knowledge_bases / documents / ingest_tasks / api_keys / chat_history（含索引：documents.kb_id、ingest_tasks.status、chat_history.session_id）
5. 实现 `Migrate(ctx) error`（执行 DDL）

**验证：** `go build ./internal/store/...` 编译通过

## T3: store 知识库与文档

**文件：** `internal/store/kb.go`、`internal/store/document.go`
**依赖：** T2
**步骤：**
1. kb.go：CreateKB / ListKBs / GetKB / UpdateKB / DeleteKB（标准 INSERT/SELECT/UPDATE/DELETE）
2. document.go：CreateDocument / ListDocuments(kbID) / GetDocument / UpdateDocumentStatus(id, status, chunkIDs) / DeleteDocument
3. Document.ChunkIDs 以 `text[]` 或 JSON 存储，UpdateDocumentStatus 一并回填

**验证：** `go build ./internal/store/...` 编译通过

## T4: store 任务与 API Key

**文件：** `internal/store/task.go`、`internal/store/apikey.go`
**依赖：** T2
**步骤：**
1. task.go：CreateTask / GetTask / ListTasks / UpdateTask；`ClaimPendingTasks(limit)` 用 `UPDATE ingest_tasks SET status='processing' WHERE id IN (SELECT id FROM ingest_tasks WHERE status='pending' ORDER BY created_at LIMIT $1) RETURNING ...` 原子领取；`ResetProcessingTasks()` 把 processing 重置为 pending
2. apikey.go：CreateAPIKey / ListAPIKeys / GetAPIKeyByHash / SetAPIKeyEnabled / DeleteAPIKey / TouchAPIKey（UPDATE last_used_at）

**验证：** `go build ./internal/store/...` 编译通过

## T5: store 对话历史

**文件：** `internal/store/history.go`
**依赖：** T2
**步骤：**
1. 实现 `PostgresHistoryStore`（持有 Store 或直接持有 pool），实现 `rag.HistoryStore`：Append 插入 chat_history、Get(sessionID, limit) 取最近 limit 条（按 created_at DESC 再反序）、Clear 删除
2. 保证返回的 Message 顺序为时间正序

**验证：** `go build ./internal/store/...` 编译通过

## T6: pipeline 扩展（kb/document 维度）

**文件：** `internal/pipeline/pipeline.go`、`internal/pipeline/pipeline_test.go`
**依赖：** T1（与 store 无依赖，可与 T3-T5 并行）
**步骤：**
1. 新增 `IngestRequest{KBID, DocumentID string; Reader io.Reader; Info loader.FileInfo}`
2. `Ingest(ctx, req IngestRequest) ([]string, error)` 返回 chunkIDs
3. VectorRecord.Payload 增加 `kb_id`、`document_id`、`chunk_id`（chunk_id = uuid，与 BM25 使用的 id 一致）
4. BM25 更新改为 `bm25Index.Add(chunkID, content, req.KBID)`
5. 更新 pipeline_test 适配新签名与 payload 断言

**验证：** `go build ./...` + `go test ./internal/pipeline/` 通过

## T7: retriever BM25 多知识库过滤

**文件：** `internal/retriever/bm25.go`、`internal/retriever/retriever.go`、`internal/retriever/retriever_test.go`
**依赖：** T1（与 T6 相关联，建议 T6 之后做）
**步骤：**
1. bm25.go：`Add(id, content, kbID string)` 记录 docKB 映射；新增 `SearchFiltered(query string, topK int, kbID string) []BM25Result`（空 kbID 时不过滤）
2. retriever.go：向量检索与 BM25 并发路径——若 `req.Filter["kb_id"]` 存在，BM25 走 SearchFiltered；向量检索保持传 filter
3. defaultRetriever 构造不变（bm25Index 由外部注入，Add 签名变化由 pipeline 调用处适配）
4. 更新 retriever_test 适配新签名，补充 kb 过滤用例

**验证：** `go build ./...` + `go test ./internal/retriever/` 通过

## T8: task worker

**文件：** `internal/task/worker.go`
**依赖：** T2、T6、T7
**步骤：**
1. 定义 `WorkerPool` 接口（Start/Shutdown）与 `defaultWorkerPool`
2. `NewWorkerPool(cfg, store, pipeline) WorkerPool`；Start 前先调 `store.ResetProcessingTasks()`
3. worker 循环：`store.ClaimPendingTasks(batch)` → 逐任务 `pipeline.Ingest(IngestRequest{...})` → 成功 UpdateTask(completed) + UpdateDocumentStatus(completed, chunkIDs)；失败：retry_count < 上限 → retry_count+1 回 pending（日志告警），否则 failed + error_message
4. 任务读取文件：从 `{file_storage_dir}/{kb_id}/{document_id}.{ext}` 读，document_id 与文件名从任务关联的文档记录取（GetDocument）
5. Shutdown：退出循环，等待当前任务完成

**验证：** `go build ./internal/task/...` 编译通过

## T9: store 单元测试

**文件：** `internal/store/store_test.go`
**依赖：** T2-T5
**步骤：**
1. 引入 `github.com/pashagolub/pgxmock/v4`（测试依赖）
2. 用 pgxmock 断言关键 SQL：CreateKB（参数绑定）、ClaimPendingTasks（UPDATE...RETURNING 行为与状态变化）、ResetProcessingTasks、CreateAPIKey/GetAPIKeyByHash、PostgresHistoryStore 的 Get（最近 limit 条）
3. 真实 DSN 集成测试用环境变量 `BINRAG_TEST_DSN` 门控，未设置时 t.Skip

**验证：** `go test ./internal/store/ -v` 通过

## T10: api 基础（响应 / 中间件 / 路由）

**文件：** `internal/api/response.go`、`internal/api/middleware.go`、`internal/api/router.go`
**依赖：** T2
**步骤：**
1. response.go：`OK(c, data)`、`Fail(c, code, message)` 统一 `{code, message, data}`；定义业务错误码常量（400/401/404/500）
2. middleware.go：`Auth(store)`（Bearer → SHA-256 → GetAPIKeyByHash → 校验 Enabled → TouchAPIKey；未启用认证配置时放行）、`Logger`（方法/路径/状态码/耗时）、`CORS`、`RateLimit(qps)`
3. router.go：`NewRouter(cfg, store, engine, worker) *gin.Engine`——注册全部 13 条路由（先写骨架，handler 在 T11/T12 实现），认证中间件挂到受保护组

**验证：** `go build ./internal/api/...` 编译通过（handler 未实现时用占位）

## T11: api handler（知识库 / 文档 / 任务）

**文件：** `internal/api/handler_kb.go`、`handler_doc.go`、`handler_task.go`
**依赖：** T10
**步骤：**
1. handler_kb.go：POST/GET/GET:id/PUT/DELETE knowledge-bases；删除知识库时先删其全部文档（查文档列表 → 逐个走删除文档逻辑）再删记录
2. handler_doc.go：上传（multipart 解析 kb_id+file → 扩展名校验（loader.Registry.Resolve）→ 大小限制 → 存文件 `{dir}/{kb_id}/{doc_id}.{ext}` → CreateDocument + CreateTask(pending) → 返回 task_id）；列表（按 kb_id filter）；删除（GetDocument → ChunkIDs → vectorstore.Delete + bm25Index.Remove×N → DeleteDocument）
3. handler_task.go：GET /tasks/:id 状态；POST /tasks/:id/retry（failed → pending，retry_count 清零）

**验证：** `go build ./internal/api/...` 编译通过

## T12: api handler（问答 / 历史 / Key）+ SSE

**文件：** `internal/api/handler_chat.go`、`handler_history.go`、`handler_key.go`
**依赖：** T10
**步骤：**
1. handler_chat.go：POST /chat——解析 {session_id, question, kb_id(可选)}；普通模式调 engine.Ask 返回 {answer, sources}；流式模式（Accept 含 text/event-stream 或 stream=true）用 engine.StreamAsk，Gin 流式写 `event: sources/chunk/done/error`（复用 rag.StreamEvent 类型）
2. handler_history.go：GET /chat/history?session_id= → store 的 rag.HistoryStore 实现 Get
3. handler_key.go：POST /api-keys（生成明文 Key 返回一次，存 SHA-256 hash + name）、GET 列表（不含 hash）、DELETE、POST /:id/toggle
4. Key 生成用 crypto/rand 生成 32 字节随机串，base64 展示

**验证：** `go build ./internal/api/...` 编译通过

## T13: cmd/server 装配

**文件：** `cmd/server/main.go`
**依赖：** T1、T8、T11、T12
**步骤：**
1. LoadConfig → store.New + Migrate → 若 api_keys 表空且配置了 BootstrapAPIKey，种子创建（打印明文一次）
2. 组装：pipeline（含 bm25 索引、embedder、vectorstore）→ task.NewWorkerPool.Start → rag.NewEngine（HistoryStore 用 store 的 Postgres 实现）→ api.NewRouter
3. HTTP Listen + 优雅关停（signal → worker.Shutdown → server.Shutdown）
4. 启动时打印关键配置（端口、worker 数、存储目录）

**验证：** `go build ./...` + `go vet ./...` 通过

## T14: worker 与 api 测试

**文件：** `internal/task/worker_test.go`、`internal/api/api_test.go`
**依赖：** T8、T11、T12
**步骤：**
1. worker_test.go：fake store（内存实现 Store 接口，记录调用）；场景——任务成功 completed、失败超限 failed 且 error_message 记录、重试未超限回 pending、ResetProcessingTasks 被调用、并发任务无竞争
2. api_test.go：fake store + fake engine（实现 rag.Engine，Ask 返回固定回答、StreamAsk 发固定事件序列）；httptest 端到端覆盖：创建知识库、上传返回 task_id、任务状态、文档列表、删除文档、问答（普通 + SSE 事件断言）、对话历史、API Key 认证（401/通过）、统一响应格式
3. 上传测试用 multipart 构造 + 临时目录存文件

**验证：** `go test ./internal/task/... ./internal/api/... -race` 全部通过

## T15: 全量验证

**文件：** 无新增
**依赖：** T1-T14
**步骤：**
1. `go build ./...` 全项目编译
2. `go test ./...` 全部测试（含既有包无回归）
3. `go test -race ./internal/store/... ./internal/task/... ./internal/api/...` 无数据竞争
4. `go vet ./...` 无告警

**验证：** 上述命令全部通过

## 执行顺序

```
T1
 ├→ T2 → T3 → T4 → T5 → T9
 ├→ T6 → T7
 └→ (T2 后) T10 → T11 → T12
                        ↘
T8（依赖 T6/T7）→ T13（依赖 T8/T11/T12）→ T14 → T15
```

T3/T4/T5 在 T2 后可并行；T6/T7 与 store 线并行；T10 依赖 T2 可早于 T3 开始。
