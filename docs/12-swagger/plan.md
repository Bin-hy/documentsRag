# API Swagger 文档 Plan

## 架构概览

使用 **swaggo**（`swag` CLI 生成 + `gin-swagger` 运行时托管）为 16 条接口生成 OpenAPI 3 文档：

1. **注解层**：在 6 个 handler 文件的每个 handler 上方添加 swag 注解（`@Summary`/`@Description`/`@Tags`/`@Accept`/`@Produce`/`@Param`/`@Success`/`@Failure`/`@Router`），覆盖全部 16 条接口。
2. **文档生成**：swag 扫描注解生成 `docs/` 包（`docs/docs.go` + `swagger.json`/`swagger.yaml`），纳入 `internal/api` 或项目根的 `docs` 目录（与 docs/ 需求文档区分，生成到 `internal/api/docs/` 避免与项目 docs/ 冲突）。
3. **运行时托管**：router 挂载 `/swagger/*`（`swaggerFiles.Handler`），使用 gin-swagger 中间件；swagger.json 作为 embed 资源随二进制发布。
4. **认证说明**：swagger 全局 securityDefinitions（`Authorization: Bearer`），注解中给接口标记安全要求；文档页可输入 API Key 实测。

## 核心数据结构

### 文档注解（swag 注释，非 Go 类型）
每个 handler 上方添加标准 swag 注解块，例如：

```go
// CreateKB 创建知识库
// @Summary 创建知识库
// @Tags 知识库
// @Accept json
// @Produce json
// @Param body body createKBRequest true "知识库信息"
// @Success 200 {object} Response{data=store.KnowledgeBase}
// @Failure 400 {object} Response
// @Failure 500 {object} Response
// @Security ApiKeyAuth
// @Router /api/v1/knowledge-bases [post]
```

### 关键注解映射

| 接口组 | Tags | 主要响应 data 类型 |
|--------|------|-------------------|
| 知识库 5 条 | 知识库 | `store.KnowledgeBase` / `gin.H{id}` |
| 文档 3 条 | 文档 | `store.Document` / `gin.H{task_id,document_id}` / `gin.H{id}` |
| 任务 2 条 | 任务 | `store.Task` |
| 问答 2 条 | 问答 | `rag.RAGResult` / `llm.Message[]`（历史） |
| API Key 4 条 | API Key | `gin.H{id,name,key}` / `keyView` / `gin.H{id}` |

## 模块设计

### swag 注解（internal/api/handler_*.go × 6）
**职责：** 为 16 个 handler 添加标准注解。
**改动：** `handler_kb.go`（5）、`handler_doc.go`（3）、`handler_task.go`（2）、`handler_chat.go`（2，ChatDispatch 分流 + 两个实际 handler）、`handler_history.go`（1）、`handler_key.go`（4）。
**依赖：** 无。

### 文档生成包（internal/api/docs/）
**职责：** swag 生成的 OpenAPI 3 描述（docs.go / swagger.json / swagger.yaml），embed 进二进制。
**生成命令：** `swag init -g ./internal/api/router.go -o ./internal/api/docs`（生成产物提交 git，保证构建不依赖 CLI）。

### 路由挂载（internal/api/router.go）
**职责：** 注册 `/swagger/*` 与 `/swagger/doc.json`。
**改动：** `NewRouter` 中在认证组之外挂载 `r.GET("/swagger/*any", swagger.WrapHandler)`；docs 包 import `_ "github.com/Bin-hy/bin-rag/internal/api/docs"`（注册 init 数据）。

### 依赖（go.mod）
- `github.com/swaggo/swag`（CLI，仅生成期）
- `github.com/swaggo/gin-swagger`（运行时托管）
- `github.com/swaggo/files`（静态资源 embed）

## 模块交互

```
handler_*.go 注解
     │ swag init 扫描
     ▼
internal/api/docs/（docs.go + swagger.json/yaml）
     │ go:embed（gin-swagger 内部）
     ▼
GET /swagger/index.html → Swagger UI（读取 docs.go 的 JSON）
GET /swagger/doc.json  → OpenAPI 3 JSON（供外部工具消费）
```

## 文件组织

```
internal/api/
├── handler_kb.go        — 知识库 5 个 handler 加注解
├── handler_doc.go       — 文档 3 个 handler 加注解
├── handler_task.go      — 任务 2 个 handler 加注解
├── handler_chat.go      — 问答 2 个 handler 加注解
├── handler_history.go   — 历史 1 个 handler 加注解
├── handler_key.go       — API Key 4 个 handler 加注解
├── router.go            — 挂载 /swagger/*，import docs 包
└── docs/                — swag 生成产物（docs.go/swagger.json/swagger.yaml）
go.mod                   — swaggo/gin-swagger/files 依赖
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 文档方案 | swaggo（swag CLI + gin-swagger） | 路线图与决策记录既定方向；gin 生态标准方案；注解与代码同文件，维护成本低 |
| 生成产物存放 | `internal/api/docs/` | 与项目 `docs/`（需求文档）隔离；import 路径清晰 |
| 产物是否提交 | 提交 git | 构建不依赖 swag CLI 安装，任何环境 `go build` 即得完整文档 |
| 生成命令 | `swag init -g ./internal/api/router.go -o ./internal/api/docs` | 扫描 router 可达的全部 handler 注解 |
| 挂载位置 | 认证组之外（公开） | 文档可匿名浏览（N3：与 auth_enabled 无关，文档页本身无需 Key；接口实测仍需 Key）；若需保护可后续加 |
| 安全定义 | `securityDefinitions: ApiKeyAuth` | Swagger UI 提供 Authorize 输入框，满足 F5 可交互测试 |
| ChatDispatch 分流 | 为 Chat（普通）与 ChatStream（SSE）分别注解 | 两个实际 handler 行为不同（JSON vs SSE），文档需分别描述 |
