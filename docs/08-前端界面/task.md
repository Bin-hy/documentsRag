# 前端界面（Web + Wails 桌面）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/app/app.go` | 服务装配提取（PostgreSQL/worker/引擎/路由） |
| 修改 | `cmd/server/main.go` | 改用 internal/app + 接入 webui 静态托管 |
| 新建 | `internal/webui/embed.go` | go:embed dist（含占位 index.html） |
| 新建 | `internal/webui/router.go` | 静态资源路由 + SPA 回退 |
| 新建 | `internal/webui/dist/index.html` | embed 占位文件（前端构建后覆盖） |
| 新建 | `frontend/package.json` | 依赖与脚本（dev/build/type-check） |
| 新建 | `frontend/vite.config.ts` | Vite 配置：outDir 指向 `../internal/webui/dist`、dev 代理 /api |
| 新建 | `frontend/tsconfig.json` | TS 配置 |
| 新建 | `frontend/index.html` | SPA 入口 HTML |
| 新建 | `frontend/src/main.ts` | 应用引导（Element Plus/Pinia/router/主题） |
| 新建 | `frontend/src/App.vue` | 根组件（router-view） |
| 新建 | `frontend/src/styles/main.css` | CSS 变量 + 亮/暗主题 |
| 新建 | `frontend/src/router/index.ts` | 路由表 + 登录守卫 |
| 新建 | `frontend/src/api/types.ts` | 后端实体 TS 类型 |
| 新建 | `frontend/src/api/client.ts` | axios 实例 + Bearer 拦截器 + 401 处理 |
| 新建 | `frontend/src/api/kb.ts` | 知识库 REST 封装 |
| 新建 | `frontend/src/api/doc.ts` | 文档 REST 封装 |
| 新建 | `frontend/src/api/task.ts` | 任务 REST 封装 |
| 新建 | `frontend/src/api/key.ts` | API Key REST 封装 |
| 新建 | `frontend/src/api/chat.ts` | fetch + ReadableStream 的 SSE 客户端 |
| 新建 | `frontend/src/stores/auth.ts` | API Key 凭据管理 |
| 新建 | `frontend/src/stores/kb.ts` | 知识库状态 |
| 新建 | `frontend/src/stores/doc.ts` | 文档/任务状态 + 轮询 |
| 新建 | `frontend/src/stores/chat.ts` | 会话/消息/流式状态 |
| 新建 | `frontend/src/views/LoginView.vue` | 登录页 |
| 新建 | `frontend/src/views/ChatView.vue` | 对话页（三栏） |
| 新建 | `frontend/src/views/KbListView.vue` | 知识库列表页 |
| 新建 | `frontend/src/views/KbDetailView.vue` | 知识库详情页（文档/上传/任务） |
| 新建 | `frontend/src/views/ApiKeysView.vue` | API Key 管理页 |
| 新建 | `frontend/src/components/AppLayout.vue` | 侧边导航布局 |
| 新建 | `frontend/src/components/MarkdownRenderer.vue` | marked + highlight.js |
| 新建 | `frontend/src/components/SourceCard.vue` | 引用来源卡片 |
| 新建 | `frontend/src/components/ChatMessage.vue` | 消息气泡 |
| 新建 | `frontend/src/components/SessionList.vue` | 会话侧栏 |
| 新建 | `frontend/src/components/UploadPanel.vue` | 拖拽上传 |
| 新建 | `frontend/src/components/TaskList.vue` | 任务表格 |
| 新建 | `cmd/desktop/main.go` | Wails v3 桌面入口 |
| 新建 | `Taskfile.yml` | Wails 构建/打包任务 |
| 新建 | `docs/08-前端界面/checklist.md` | 验收清单 |

## T1: 提取服务装配到 internal/app

**文件：** `internal/app/app.go`
**依赖：** 无
**步骤：**
1. 定义 `App` 结构体，字段：`cfg config.Config`、`st store.Store`、`vs vectorstore.VectorStore`、`bm25 retriever.BM25Index`、`engine rag.Engine`、`worker *task.WorkerPool`
2. 实现 `New(cfg *config.Config) (*App, error)`：把 cmd/server/main.go 中「连接 PostgreSQL → Migrate → seedAPIKey → 初始化 embedder/vectorstore/bm25 → pipeline → worker → llm/reranker/retriever → rag engine」整体平移（含 `ragHistoryAdapter`）
3. 实现 `Router() *gin.Engine`：调 `api.NewRouter(...)` 返回路由
4. 实现 `Close() error`：`worker.Shutdown()` + `st.Close()`（worker 需先 Start，同现有逻辑）
5. 保留 cmd/server 中 `parseConfigFlag` 与 `seedAPIKey`（seedAPIKey 可留在 app 包内作为私有函数）

**验证：** `go build ./...` 编译通过（cmd/server 暂未改，函数重复定义会报错——若报错先删 cmd/server 中已平移代码）

## T2: cmd/server 改用 internal/app

**文件：** `cmd/server/main.go`
**依赖：** T1
**步骤：**
1. main() 改为：`parseConfigFlag` → `config.LoadConfig` → `app.New(cfg)` → `http.Server` 监听 `cfg.Server.Port` 提供 `app.Router()` → 信号退出时 `srv.Shutdown` + `app.Close()`
2. 删除已平移的装配代码与 `ragHistoryAdapter`（保留 parseConfigFlag 或一并移入 app 包后引用）

**验证：** `go build ./...` + `go test ./...` 全部通过

## T3: internal/webui 静态托管

**文件：** `internal/webui/embed.go`、`internal/webui/router.go`、`internal/webui/dist/index.html`
**依赖：** 无
**步骤：**
1. 创建 `internal/webui/dist/index.html` 占位文件（内容：`<title>BinRag</title>` 简单页，保证 embed 可编译）
2. `embed.go`：`//go:embed all:dist` + `fs.Sub` 返回 dist 子文件系统；`func FS() (fs.FS, error)`
3. `router.go`：`Register(r *gin.Engine)`——`r.GET("/", ...)` 返回 index.html；`r.GET("/assets/*filepath", ...)` 用 `http.FileServer` 服务静态文件（设置 Cache-Control）；`r.NoRoute(...)` 回退 index.html（仅对非 `/api/` 路径回退，`/api/*` 保持 404 JSON）

**验证：** `go build ./internal/webui/...` 编译通过；`go test ./...` 不破坏现有测试

## T4: cmd/server 接入 webui

**文件：** `cmd/server/main.go`
**依赖：** T2、T3
**步骤：**
1. `app.Router()` 之后调用 `webui.Register(router)`（或在 app.Router() 内注册——按 T1 实现位置决定，保持单一入口）
2. 确认监听端口后打印访问地址

**验证：** 启动服务（`go run ./cmd/server`），curl `http://127.0.0.1:<port>/` 返回 index.html 内容；curl `/api/v1/knowledge-bases` 返回 JSON（401 或正常）

## T5: 前端脚手架

**文件：** `frontend/package.json`、`frontend/vite.config.ts`、`frontend/tsconfig.json`、`frontend/index.html`、`frontend/src/main.ts`、`frontend/src/App.vue`
**依赖：** 无（与 T1-T4 并行）
**步骤：**
1. package.json：dependencies 含 `vue`、`vue-router`、`pinia`、`axios`、`element-plus`、`@element-plus/icons-vue`、`marked`、`highlight.js`；devDependencies 含 `vite`、`@vitejs/plugin-vue`、`typescript`、`vue-tsc`
2. scripts：`dev` = `vite`；`build` = `vue-tsc -b && vite build`（类型检查 + 构建）
3. vite.config.ts：`outDir: '../internal/webui/dist'`（构建产物直接进 Go embed 目录）、`emptyOutDir: true`；dev 下 `server.proxy['/api'] → http://127.0.0.1:<后端端口>`
4. main.ts：创建 app → use(Pinia) → use(router) → use(ElementPlus) → 挂载；导入 styles/main.css 与 Element Plus 样式
5. App.vue：仅 `<router-view />`

**验证：** `npm install` 成功；`npm run build` 产出 `internal/webui/dist/` 且 `vue-tsc` 无类型错误；`npm run dev` 可启动 dev server

## T6: 全局样式与主题

**文件：** `frontend/src/styles/main.css`
**依赖：** T5
**步骤：**
1. 定义 CSS 变量：页面背景、卡片背景、文字主/次色、边框色、主色
2. 实现亮/暗主题切换：`html.dark` 类切换变量组（配合 Element Plus dark 模式 `html.dark` 类）
3. 基础样式：滚动条、消息区排版、通用工具类

**验证：** `npm run build` 通过；dev 下切换 `html.dark` 类可见主题变化

## T7: 路由与登录守卫 + 布局

**文件：** `frontend/src/router/index.ts`、`frontend/src/components/AppLayout.vue`
**依赖：** T5
**步骤：**
1. 路由表：`/login` → LoginView；`/` 重定向 `/chat`；`/chat` → ChatView；`/kb` → KbListView；`/kb/:id` → KbDetailView；`/keys` → ApiKeysView；业务路由统一用 AppLayout 作父组件（children）
2. 全局前置守卫：`beforeEach`——未登录（无 apiKey）且目标非 `/login` → 重定向 `/login`
3. AppLayout.vue：左侧导航（对话/知识库/API Key 三个菜单项 + 图标）+ 顶栏（右侧登出按钮）+ `<router-view>`；当前路由高亮

**验证：** `npm run build` 通过；dev 下直接访问 `/kb` 被重定向 `/login`，访问 `/login` 正常

## T8: API 层

**文件：** `frontend/src/api/types.ts`、`client.ts`、`kb.ts`、`doc.ts`、`task.ts`、`key.ts`、`chat.ts`
**依赖：** T5
**步骤：**
1. types.ts：按 plan.md「核心数据结构」定义 `ApiResponse<T>`、`Kb`、`Document`、`Task`、`ApiKeyView`、`CreateKeyResult`、`ChatSource`、`ChatMessage`、`ChatRequest`、`SSEEvent`、`SessionMeta`
2. client.ts：axios 实例 `baseURL: '/'`；请求拦截器从 `localStorage.getItem('binrag_api_key')` 附加 `Authorization: Bearer <key>`；响应拦截器统一返回 `data.data`，遇 HTTP 401 清除凭据并 `location.href = '/login'`
3. kb.ts：`listKbs() / createKb({name, description}) / updateKb(id, ...) / deleteKb(id)`（对应 `GET/POST/PUT/DELETE /api/v1/knowledge-bases[/:id]`）
4. doc.ts：`uploadDocument(kbId, file)`（FormData，`POST /api/v1/documents/upload?kb_id=`——以 handler 实际参数名为准，开发时核对）、`listDocuments(kbId)`、`deleteDocument(id)`
5. task.ts：`getTask(id)`、`listTasks(kbId)`、`retryTask(id)`（对应 `POST /api/v1/tasks/:id/retry`）
6. key.ts：`createKey(name)`、`listKeys()`、`toggleKey(id, enabled)`、`deleteKey(id)`
7. chat.ts：`chatStream(req: ChatRequest, onEvent: (e: SSEEvent) => void): Promise<void>`——fetch POST `/api/v1/chat`（`Accept: text/event-stream`），`response.body.getReader()` + `TextDecoder` 逐行解析 `data:` 前缀 JSON，按事件分发；`error` 事件也触发 onEvent；网络异常 reject 由调用方处理

**验证：** `npm run build` 通过（类型检查）；对接真实后端手动验证在 T11+ 页面联调时进行

## T9: stores

**文件：** `frontend/src/stores/auth.ts`、`kb.ts`、`doc.ts`、`chat.ts`
**依赖：** T8
**步骤：**
1. auth.ts：state `apiKey`（初始化自 localStorage）；`login(key)` 保存 + 调 `listKbs()` 验证有效性（失败抛错）；`logout()` 清除
2. kb.ts：`kbs`、`loading`；`load()`；`create/update/remove`（操作成功后刷新列表）
3. doc.ts：`documents`、`tasks`、`polling`；`load(kbId)`；`upload(kbId, files)`（逐文件上传返回任务）；`retry(taskId)`；`remove(docId)`；`startPolling(kbId)`——立即拉取一次，若有 pending/processing 任务则 3s 定时器继续，否则停止；`stopPolling()`
4. chat.ts：`sessions: SessionMeta[]`（localStorage `binrag_sessions` 持久化）、`activeSessionId`、`messages`、`sources`、`streaming`；`newSession(kbId)`（生成 `crypto.randomUUID()` 存 SessionMeta）；`switchSession(id)`（从后端 `GET /api/v1/chat/history?session_id=` 拉消息）；`deleteSession(id)`（移除索引）；`send(question)`——追加用户消息 → `chatStream`：sources 存起、chunk 增量追加到当前 assistant 消息、done 收尾（若为空消息置「（无回答）」）、error 显示错误条；完成后用首条用户消息前 20 字更新会话标题

**验证：** `npm run build` 通过（类型检查）

## T10: 登录页

**文件：** `frontend/src/views/LoginView.vue`
**依赖：** T7、T9
**步骤：**
1. 居中卡片：标题「BinRag 知识库问答」+ API Key 输入框（password 类型，可切换可见）+ 登录按钮
2. 提交：调 `auth.login(key)`，成功 `router.push('/chat')`；失败 ElMessage 展示后端错误信息
3. 未登录访问 `/login` 时若已有凭据自动跳转 `/chat`

**验证：** `npm run build` 通过；dev + 后端联调：输入正确 Key 进入主界面，错误 Key 提示错误

## T11: 知识库列表页

**文件：** `frontend/src/views/KbListView.vue`
**依赖：** T9
**步骤：**
1. 页面头部「新建知识库」按钮 → ElDialog 表单（名称必填、描述）
2. 卡片/表格展示知识库（名称、描述、创建时间）；「进入」跳 `/kb/:id`；「编辑」「删除」（ElMessageBox 确认）
3. 空状态提示引导创建

**验证：** `npm run build` 通过；联调：创建/编辑/删除均实时反映

## T12: 知识库详情页（文档上传与任务）

**文件：** `frontend/src/views/KbDetailView.vue`、`frontend/src/components/UploadPanel.vue`、`frontend/src/components/TaskList.vue`
**依赖：** T9
**步骤：**
1. KbDetailView：知识库信息头（名称/描述/返回按钮）+ UploadPanel + 文档表格（文件名/格式/大小/状态徽标/创建时间/删除）+ TaskList
2. UploadPanel：拖拽区（`dragover`/`drop` 事件）+ 点击选择；多文件；按后端支持格式过滤（.txt/.md/.pdf/.docx/.csv/.xlsx/.html）；上传中显示进度（逐文件，ElMessage 或进度条）
3. TaskList：表格展示任务（文档名/状态徽标 pending=灰、processing=蓝、completed=绿、failed=红/重试次数/错误信息 tooltip/「重试」按钮调 `retryTask`）
4. 挂载时 `load(kbId)` + `startPolling(kbId)`；卸载时 `stopPolling()`
5. 上传成功后刷新文档与任务列表

**验证：** `npm run build` 通过；联调：上传文件 → 任务 pending→processing→completed 自动刷新；失败任务可查看错误并重试

## T13: API Key 管理页

**文件：** `frontend/src/views/ApiKeysView.vue`
**依赖：** T9
**步骤：**
1. 「创建 Key」按钮 → ElDialog 输入名称 → 提交后弹「明文一次性展示」对话框（`{key}` 内容 + 复制按钮 + 警告文案「关闭后将无法再次查看」）
2. 表格：名称/状态（ElSwitch 调 toggleKey）/最后使用时间/创建时间/删除（确认框）
3. 空状态引导

**验证：** `npm run build` 通过；联调：创建→复制→开关→删除全流程

## T14: 对话页（核心）

**文件：** `frontend/src/views/ChatView.vue`、`frontend/src/components/SessionList.vue`、`ChatMessage.vue`、`SourceCard.vue`、`MarkdownRenderer.vue`
**依赖：** T9、T11（知识库选择数据源）
**步骤：**
1. ChatView 三栏布局：左 SessionList / 中消息区 / 底部输入区（textarea 多行 + 知识库下拉选择（可选，含「不限」）+ 发送按钮；流式中发送按钮变「停止」——`AbortController` 中止 fetch）
2. SessionList：会话标题列表（选中高亮）、「新建会话」按钮、每条 hover 显示删除按钮；标题取 SessionMeta.title
3. ChatMessage：用户消息右对齐普通文本；助手消息左对齐含 MarkdownRenderer + SourceCard 列表；流式中显示光标动画；错误消息红色样式
4. SourceCard：文件名 + 标题 + 分数徽标；点击暂无跳转（预留）
5. MarkdownRenderer：marked.parse + highlight.js（`highlight.js/lib/core` 按需注册常见语言），代码块 class `hljs`
6. 进入页面：若无会话自动新建；切换会话拉取历史；发送后自动滚动到底部（流式期间持续滚动）
7. 知识库选择器：从 kb store 加载列表，新会话记录 kbId，发送时写入 `ChatRequest.kb_id`

**验证：** `npm run build` 通过；联调：新建会话 → 提问 → 流式打字机输出 + 引用来源卡片 → 切换/删除会话历史正确

## T15: 构建产物就位 + Go 全量验证

**文件：** `internal/webui/dist/*`（前端构建产物）
**依赖：** T4、T5-T14
**步骤：**
1. 执行 `npm run build`（产物已直出 `internal/webui/dist/`）
2. `go build ./...` 全量编译
3. `go test ./...` 全量测试
4. 启动 `go run ./cmd/server`，浏览器访问 `http://127.0.0.1:<port>`：登录 → 建知识库 → 上传文档 → 对话，全流程人工验证

**验证：** 全流程走通（对应 spec AC1-AC6）

## T16: Wails v3 桌面入口

**文件：** `cmd/desktop/main.go`、`Taskfile.yml`
**依赖：** T1、T15（前端产物）
**步骤：**
1. 安装 wails3 CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`
2. `cmd/desktop/main.go`：`flag -c` → `config.LoadConfig` → `app.New(cfg)` → `net.Listen("127.0.0.1:0")` 拿随机端口 → goroutine 起 `http.Server` 提供 `app.Router()` → Wails v3 `application.Run`：`webview.NewWebviewWindow(application.WebviewWindowOptions{ URL: "http://127.0.0.1:<port>/" })` → `OnShutdown` 中 `http.Server.Shutdown` + `app.Close()`
3. Taskfile.yml：wails3 标准任务（build / package darwin）
4. 本地跑通 `wails3 dev`（或等价运行命令），窗口打开即显示登录页

**验证：** 桌面窗口打开 → 登录 → 建库 → 上传 → 问答全流程；关闭窗口进程退出干净

## T17: Wails 打包 macOS 应用

**文件：** `Taskfile.yml`（package 任务完善）
**依赖：** T16
**步骤：**
1. 按 wails3 文档执行 macOS 打包命令（生成 `.app`，如支持则 `.dmg`）
2. 安装包启动 → 完成 T16 全流程验证

**验证：** 生成可启动的 `.app`，双击可运行，全流程可用

## 执行顺序

```
T1 → T2 → T3 → T4 ──────────────┐
                                ├─→ T15 → T16 → T17
T5 → T6 → T7 ─┐                 │
       T8 → T9 ┤                │
              ├→ T10 → T11 → T12 → T13 ─┘
              └→ T14（依赖 T11 的知识库选择器数据源，可后置到 T11 之后）
```

Go 侧（T1-T4）与前端侧（T5-T14）两条线并行，T15 汇合联调。
