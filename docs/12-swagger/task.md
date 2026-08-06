# API Swagger 文档 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `go.mod` | 新增 swaggo/gin-swagger/files 依赖 |
| 修改 | `internal/api/handler_kb.go` | 知识库 5 个 handler 加注解 |
| 修改 | `internal/api/handler_doc.go` | 文档 3 个 handler 加注解 |
| 修改 | `internal/api/handler_task.go` | 任务 2 个 handler 加注解 |
| 修改 | `internal/api/handler_chat.go` | 问答 2 个 handler 加注解 |
| 修改 | `internal/api/handler_history.go` | 历史 1 个 handler 加注解 |
| 修改 | `internal/api/handler_key.go` | API Key 4 个 handler 加注解 |
| 修改 | `internal/api/router.go` | 挂载 /swagger/*，import docs 包 |
| 生成 | `internal/api/docs/` | swag 生成产物（docs.go/swagger.json/swagger.yaml） |

## T1: 依赖与生成命令验证

**文件：** `go.mod`
**依赖：** 无
**步骤：**
1. `go get github.com/swaggo/gin-swagger github.com/swaggo/files`（运行时依赖）
2. 安装 swag CLI：`go install github.com/swaggo/swag/cmd/swag@latest`（仅生成期工具）
3. 确认 swag CLI 可用：`swag --version`

**验证：** `swag --version` 输出版本号；`go mod tidy` 无残留

## T2: handler 注解——知识库与文档

**文件：** `internal/api/handler_kb.go`、`internal/api/handler_doc.go`
**依赖：** T1（注解本身不需依赖，可先写）
**步骤：**
1. `handler_kb.go`：为 CreateKB / ListKBs / GetKB / UpdateKB / DeleteKB 各加注解块
   - Tags=知识库；CreateKB 请求体 `createKBRequest`、成功 `Response{data=store.KnowledgeBase}`；ListKBs 成功 `Response{data=[]store.KnowledgeBase}`；GetKB/UpdateKB 路径参数 `id`；DeleteKB 成功 `Response{data=gin.H{id}}`
2. `handler_doc.go`：为 UploadDocument / ListDocuments / DeleteDocument 各加注解块
   - Tags=文档；UploadDocument `multipart/form-data`（file + query kb_id）、成功 `Response{data=gin.H{task_id,document_id}}`；ListDocuments query kb_id、成功 `Response{data=[]store.Document}`；DeleteDocument 路径参数 id、成功 `Response{data=gin.H{id}}`
3. 所有注解带 `@Security ApiKeyAuth`、`@Failure 400/401/404/500 {object} Response`

**验证：** `go build ./internal/api/...` 编译通过（注解是注释，不影响编译）

## T3: handler 注解——任务、问答、历史、Key

**文件：** `internal/api/handler_task.go`、`internal/api/handler_chat.go`、`internal/api/handler_history.go`、`internal/api/handler_key.go`
**依赖：** 无（与 T2 并行）
**步骤：**
1. `handler_task.go`：GetTask / RetryTask——Tags=任务；路径参数 id；成功 `Response{data=store.Task}`
2. `handler_chat.go`：为 Chat 与 ChatStream 分别注解（ChatDispatch 是分流器不注解）
   - Chat：Tags=问答；请求体 `chatRequest`；成功 `Response{data=rag.RAGResult}`
   - ChatStream：Tags=问答；`@Produce text/event-stream`；成功 `Response{data=rag.RAGResult}`（SSE 事件序列在 Description 说明：sources→chunk×N→done/error）
3. `handler_history.go`：GetHistory——Tags=问答；query session_id；成功 `Response{data=[]llm.Message}`
4. `handler_key.go`：CreateAPIKey / ListAPIKeys / DeleteAPIKey / ToggleAPIKey——Tags=API Key；CreateAPIKey 请求体 `createAPIKeyRequest`、成功 `Response{data=gin.H{id,name,key}}`；ListAPIKeys 成功 `Response{data=[]keyView}`；Delete/Toggle 路径参数 id
5. 全部带 `@Security ApiKeyAuth` 与失败响应注解

**验证：** `go build ./internal/api/...` 编译通过

## T4: 挂载 swagger 路由

**文件：** `internal/api/router.go`
**依赖：** T2、T3（注解完成才能生成有效文档）
**步骤：**
1. import：`swaggerFiles "github.com/swaggo/files"`、`ginSwagger "github.com/swaggo/gin-swagger"`、`_ "github.com/Bin-hy/bin-rag/internal/api/docs"`
2. `NewRouter` 中认证组之外挂载：`r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))`
3. 确认 /swagger 挂载在 v1 组之前或之外（文档无需认证即可查看，接口实测仍需 Key）

**验证：** `go build ./...` 编译通过（docs 包尚未生成时先建占位或注释 import，见 T5 顺序调整）

## T5: 生成 docs 包并验证

**文件：** `internal/api/docs/`（生成）
**依赖：** T4（router.go 有 swagger 注解标记或可被扫描）
**步骤：**
1. 运行 `swag init -g ./internal/api/router.go -o ./internal/api/docs`
2. 检查产物：`internal/api/docs/docs.go`、`swagger.json`、`swagger.yaml` 存在
3. `go build ./...` 通过（docs 包被 router import）
4. 启动服务（临时端口）验证：`curl http://127.0.0.1:<port>/swagger/doc.json` 返回 JSON 且 paths 含 16 条接口；`GET /swagger/index.html` 返回 200

**验证：** `go build ./...` + curl `/swagger/doc.json` 含全部路由、`/swagger/index.html` 200

## T6: 全量验证

**文件：** 无新增
**依赖：** T1-T5
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿
2. `swag fmt`（如可用）检查注解格式
3. 比对 swagger.json 的 paths 与 router.go 的 16 条路由一致（人工抽查）

**验证：** 上述命令全部通过；paths 与路由一致

## 执行顺序

```
T1 → T4（依赖 T2/T3 的注解可先写骨架）
T2 ─┐
    ├→ T4 → T5 → T6
T3 ─┘
```

T2/T3 并行（不同文件）；T1 先装依赖与 CLI；T4 挂载（可先于注解，编译需 docs 包存在，故 T4 实际在 T5 生成后编译通过）；T5 生成并验证；T6 收尾。
