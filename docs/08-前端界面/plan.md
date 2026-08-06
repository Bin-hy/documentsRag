# 前端界面（Web + Wails 桌面）Plan

## 架构概览

五个组件：

1. **frontend/** — Vue 3 + TypeScript 前端应用。浏览器 Web 与 Wails 桌面共用**同一份代码**，仅通过 HTTP/SSE 与后端通信，不依赖 Wails 专有 API
2. **internal/webui/** — Go 侧静态资源托管：`go:embed` 前端构建产物（dist），注册 SPA 路由（静态文件 + 404 回退 index.html）
3. **internal/app/** — 服务装配提取：把 `cmd/server/main.go` 现有的「加载配置 → 连接 PostgreSQL → 初始化 pipeline/worker/引擎 → 建路由」逻辑提取为可复用包，Web 与桌面共用，避免两份装配代码漂移
4. **cmd/server/** — Web 形态入口（修改）：装配 → 托管前端静态资源 → 监听配置端口
5. **cmd/desktop/** — Wails v3 桌面入口（新建）：装配 → 启动内嵌 HTTP 服务监听 `127.0.0.1:0`（随机端口，仅本机）→ 用 Wails 窗口加载该 URL → 窗口关闭时优雅退出

### Spec 需求映射

| Spec | 归属 |
|------|------|
| F1 登录 | frontend `stores/auth.ts` + `api/client.ts` 请求拦截器 + 路由守卫 |
| F2 对话问答 | frontend `views/ChatView.vue` + `api/chat.ts`（SSE 客户端）+ `stores/chat.ts` |
| F3 知识库管理 | frontend `views/KbListView.vue` + `api/kb.ts` |
| F4 文档上传与任务 | frontend `views/KbDetailView.vue` + `api/doc.ts`、`api/task.ts` + 任务轮询 |
| F5 API Key 管理 | frontend `views/ApiKeysView.vue` + `api/key.ts` |
| F6 Web 形态 | internal/webui embed + cmd/server 接入 |
| F7 桌面形态 | cmd/desktop（Wails v3）+ internal/app 装配复用 |
| N2 交互体验 | Element Plus + marked + highlight.js + 流式打字机 + 拖拽上传 |
| N6 前后端解耦 | 前端只走 HTTP API；桌面窗口直接加载内嵌 Gin 页面，同源无代理层 |

## 核心数据结构

### 前端类型（`src/api/types.ts`，对齐后端 JSON 字段）

```ts
// 统一响应包装
interface ApiResponse<T> { code: number; message: string; data: T }

interface Kb { id: string; name: string; description: string; createdAt: string; updatedAt: string }
interface Document {
  id: string; kbId: string; filename: string; format: string;
  size: number; status: 'pending'|'processing'|'completed'|'failed';
  taskId: string; createdAt: string;
}
interface Task {
  id: string; kbId: string; documentId: string;
  status: 'pending'|'processing'|'completed'|'failed';
  retryCount: number; errorMessage: string; createdAt: string; updatedAt: string;
}
interface ApiKeyView { id: string; name: string; enabled: boolean; lastUsedAt: string|null; createdAt: string }
// 创建 API Key 的响应含明文一次：{ key: string }
interface CreateKeyResult { key: string }

// 对话
interface ChatSource { id: string; filename: string; heading: string; score: number }
interface ChatMessage { role: 'user'|'assistant'; content: string }
interface ChatRequest { session_id: string; question: string; kb_id: string }
// SSE 事件（后端序列：sources → chunk×N → done，或 error）
type SSEEvent =
  | { type: 'sources'; sources: ChatSource[] }
  | { type: 'chunk'; content: string }
  | { type: 'done' }
  | { type: 'error'; message: string }

// 会话索引（仅前端本地，localStorage 持久化）
interface SessionMeta { id: string; title: string; updatedAt: string; kbId: string }
```

### 后端接口（Go）

```go
// internal/app —— 服务装配（从 cmd/server 提取）
type App struct { /* cfg、store、vs、bm25、engine、worker、history 等 */ }
func New(cfg *config.Config) (*App, error)  // 连接 PostgreSQL、初始化 pipeline/worker/引擎
func (a *App) Router() *gin.Engine          // API 路由 + webui 静态托管（SPA 回退）
func (a *App) Close() error                 // worker.Shutdown + store.Close

// internal/webui —— 静态资源托管
func FS() (fs.FS, error)                    // go:embed dist 子文件系统
func Register(r *gin.Engine)                // 静态资源路由 + NoRoute 回退 index.html

// cmd/desktop —— Wails 入口
func main()                                  // flag -c → config.LoadConfig → app.New
                                             // → net.Listen("127.0.0.1:0") → http.Serve
                                             // → wails 窗口 WithURL("http://127.0.0.1:<port>")
```

## 模块设计

### frontend/src/api（HTTP 层）

- **client.ts** — axios 实例：`baseURL: '/'`（同源）；请求拦截器从 localStorage 读 API Key 附加 `Authorization: Bearer <key>`；响应拦截器统一解包 `ApiResponse`，遇 401 清除凭据并跳转登录页
- **chat.ts** — `chatStream(req, onEvent): Promise<void>`：用 `fetch` 发 POST（带 Authorization 头），`ReadableStream` 逐行解析 `data:` 事件，按事件类型回调。**不用 EventSource**（无法携带 Authorization 头）
- **kb.ts / doc.ts / task.ts / key.ts** — 对应 REST 端点封装：`listKbs / createKb / updateKb / deleteKb`、`uploadDocument(formData) / listDocuments / deleteDocument`、`getTask / retryTask / listTasks(kbId)`、`createKey / listKeys / toggleKey / deleteKey`

### frontend/src/stores（Pinia）

- **auth.ts** — state：`apiKey`；actions：`login(key)`（存 localStorage）、`logout()`；getter：`isAuthenticated`
- **chat.ts** — state：`sessions: SessionMeta[]`、`activeSessionId`、`messages: ChatMessage[]`、`sources: ChatSource[]`、`streaming: boolean`；actions：`newSession(kbId)`、`switchSession(id)`（拉取后端历史）、`deleteSession(id)`、`send(question)`（追加用户消息 → chatStream → 增量追加 assistant 消息 → 完成时更新会话标题）
- **kb.ts** — `kbs: Kb[]`、`load()`、`create/update/remove`
- **doc.ts** — 按知识库的 `documents`、`tasks`、`load(kbId)`、`upload`、`retry`、`remove`、`startPolling(kbId)`（有活动任务时每 3s 轮询，空闲自动停止）

### frontend/src/views

- **LoginView.vue** — API Key 输入框 + 登录按钮；校验通过（调一次 `listKbs` 成功即视为有效）进入主界面
- **ChatView.vue** — 三栏布局：会话侧栏（新建/切换/删除）+ 消息区（Markdown 渲染、流式打字机、引用来源卡片）+ 底部输入区（知识库选择器、发送/停止）
- **KbListView.vue** — 知识库卡片列表 + 新建/编辑对话框 + 删除确认；点击进入详情
- **KbDetailView.vue** — 知识库信息头 + 文档表格（状态徽标、大小、时间、删除）+ 拖拽上传面板 + 任务列表（进度、错误原因、重试按钮）
- **ApiKeysView.vue** — 密钥表格（名称/状态开关/最后使用时间/创建时间/删除）+ 创建对话框（创建后明文一次性展示弹窗，提示复制）

### frontend/src/components

- **AppLayout.vue** — 侧边导航（对话 / 知识库 / API Key）+ 顶栏（用户/登出）
- **MarkdownRenderer.vue** — marked 渲染 + highlight.js 代码高亮（异步加载语言）
- **SourceCard.vue** — 引用来源卡片（文件名、标题、分数）
- **ChatMessage.vue** — 单条消息气泡（用户/助手区分，助手侧含 Markdown 与来源）
- **SessionList.vue** — 会话列表（标题截断、激活态、删除按钮）
- **UploadPanel.vue** — 拖拽/点击选择文件（多文件、格式过滤），上传后展示任务入口
- **TaskList.vue** — 任务表格（状态徽标、重试次数、错误信息 tooltip、重试按钮）

### frontend/src/router

`/login`、`/`（重定向 `/chat`）、`/chat`、`/kb`、`/kb/:id`、`/keys`；全局前置守卫：未登录且目标非 `/login` 时重定向登录页

### 后端模块

- **internal/webui/embed.go** — `//go:embed all:dist` + `fs.Sub` 提取 dist 子目录
- **internal/webui/router.go** — `Register`：`GET /assets/*` 静态文件（带缓存头）+ `GET /` 返回 index.html + `NoRoute` 回退 index.html（SPA 路由刷新可用）
- **internal/app/app.go** — 装配逻辑（从 cmd/server 平移：store → migrate → seedAPIKey → embedder/vectorstore/bm25 → pipeline → worker → llm/reranker/retriever/engine → api.NewRouter）+ `RegisterWebUI()` 挂载静态托管
- **cmd/server/main.go** — 精简为：解析 flag → `config.LoadConfig` → `app.New` → `http.ListenAndServe`（配置端口）；保留退出信号处理
- **cmd/desktop/main.go** — `config.LoadConfig` → `app.New` → `net.Listen("127.0.0.1:0")` → 启动 http.Server → Wails v3 `application.Run`：`webview.NewWebviewWindow(WithURL("http://127.0.0.1:"+port))`；`OnShutdown` 调 `app.Close()` 与 http.Server 优雅关闭

## 模块交互

### Web 形态数据流

```
浏览器
  └─ frontend (Vue SPA, 由 Gin 托管)
       ├─ stores → api/client.ts (axios + Bearer) ──→ internal/api handlers
       └─ stores/chat → api/chat.ts (fetch + ReadableStream) ──→ POST /api/v1/chat (SSE)
                                                            ←─ sources → chunk×N → done
```

### 桌面形态启动序列

```
cmd/desktop main
  → config.LoadConfig（-c / BINRAG_CONFIG / ./configs/config.yaml）
  → app.New(cfg)（PostgreSQL、worker、引擎、路由 + 静态托管）
  → net.Listen("127.0.0.1:0") 取随机端口
  → http.Server 启动（goroutine）
  → Wails 窗口 WithURL("http://127.0.0.1:<port>/")
  → 用户操作走与 Web 完全相同的 HTTP 路径（同源，无代理）
  → 窗口关闭：http.Server.Shutdown + app.Close()
```

### 任务状态刷新

上传文档后 `stores/doc.startPolling(kbId)`：每 3s 拉取该知识库任务列表；无 pending/processing 任务时自动停止轮询；KbDetailView 卸载时停止。

## 文件组织

```
project/
├── frontend/                          # 新建：Vue 3 + TS 前端
│   ├── package.json
│   ├── vite.config.ts                 # dev server + /api 代理到后端
│   ├── tsconfig.json
│   ├── index.html
│   └── src/
│       ├── main.ts / App.vue
│       ├── router/index.ts
│       ├── api/ types.ts client.ts chat.ts kb.ts doc.ts task.ts key.ts
│       ├── stores/ auth.ts chat.ts kb.ts doc.ts
│       ├── views/ LoginView.vue ChatView.vue KbListView.vue KbDetailView.vue ApiKeysView.vue
│       ├── components/ AppLayout.vue MarkdownRenderer.vue SourceCard.vue
│       │               ChatMessage.vue SessionList.vue UploadPanel.vue TaskList.vue
│       └── styles/ main.css            # CSS 变量 + 亮/暗主题
├── internal/
│   ├── app/app.go                      # 新建：装配提取
│   └── webui/                          # 新建：静态托管
│       ├── embed.go  (dist 目录需存在，构建时由前端产物填充)
│       └── router.go
├── cmd/
│   ├── server/main.go                  # 修改：改用 internal/app
│   └── desktop/main.go                 # 新建：Wails v3 桌面入口
├── wails.json / Taskfile.yml           # 新建：Wails 构建/打包配置
└── docs/08-前端界面/ plan.md task.md checklist.md
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| Wails 版本 | v3.0.0-beta.4 | 原生 `WebviewWindowOptions.URL` 支持加载远程 URL（v2 无此能力）；已含 macOS .app/.dmg 打包；桌面 API 已声明稳定。风险：beta——降级路径为 webview_go（Go 生态唯一维护中的 webview 封装，需手写打包脚本） |
| 桌面架构 | 内嵌 Gin 监听 127.0.0.1:0，窗口直接加载该 URL | 前端同源零差异、SSE 直连无代理层、不依赖 Wails 专有 API（满足 N6），浏览器调试路径与桌面路径完全一致 |
| 装配提取 | internal/app 包 | cmd/server 与 cmd/desktop 共用一套装配逻辑，避免双份代码漂移；Web/桌面行为天然一致 |
| 静态托管 | go:embed dist + NoRoute 回退 index.html | 单一可执行文件（F6）；SPA 深链刷新可用 |
| 会话索引 | 前端 localStorage 管理会话列表（id/标题/时间），消息内容仍从后端拉取 | 后端无会话列表 API（spec 约定不改业务 API）；实现成本最低且功能完整 |
| UI 组件库 | Element Plus | 中文中后台生态最成熟、组件齐全（表格/对话框/消息/上传）、支持暗色主题与 CSS 变量 |
| Markdown/代码高亮 | marked + highlight.js | 轻量、成熟、按需加载语言包 |
| SSE 客户端 | fetch + ReadableStream 手写解析 | EventSource 无法携带 Authorization 头，与 F1 冲突；手写解析 ~40 行，可控 |
| 任务状态刷新 | 前端 3s 轮询，无活动任务自动停止 | 后端无推送通道；轮询范围限定单知识库、自动启停，开销可控 |
| 构建工具链 | Vite + TypeScript + Vue 3 | Wails 官方推荐组合；dev server 代理 /api 便于浏览器调试 |
| 桌面版配置 | 沿用 -c flag / BINRAG_CONFIG / ./configs/config.yaml | 与现有 cmd/server 行为一致，无新配置机制 |
| 暗色主题 | CSS 变量 + Element Plus dark 变量覆盖 | 满足 N2 交互体验，成本低 |

## 待确认风险

- Wails v3 为 beta：安装 `wails3` CLI 与 macOS 打包链路若在本机受阻，按降级路径切 webview_go（打包脚本手写 .app）
- 前端构建需要 Node.js 工具链（Vite），桌面打包在装有 Node + Go 的机器执行；纯 Go 侧编译不依赖 Node（dist 预构建后 `go:embed` 生效）
