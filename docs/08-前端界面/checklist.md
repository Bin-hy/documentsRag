# 前端界面（Web + Wails 桌面）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。
> 联调前置条件：后端依赖可用（PostgreSQL + Qdrant，可用 `docker compose up -d` 启动）+ LLM 配置有效（否则问答返回错误提示也可验证降级路径）。

## 实现完整性

- [x] 登录闭环（验证：API 层 401 校验返回「无效或已停用的 API Key」；未登录重定向与登录流程由路由守卫实现，浏览器交互需用户本机确认）
- [x] 对话问答核心（验证：SSE 事件序列 sources（5 条检索结果含 filename/heading/score）→ chunk → done / error 与前端解析逻辑逐字段匹配；流式打字机与 Markdown 渲染需用户本机确认）
- [x] 会话管理（验证：会话索引 localStorage 持久化 + 历史消息接口 `GET /chat/history` 联调通过；界面交互需用户本机确认）
- [x] 知识库 CRUD（验证：创建/列表/删除接口联调通过，删除后列表为空；表单校验与确认框需用户本机确认）
- [x] 文档上传与任务（验证：上传→任务 pending→completed 全链路实测通过，并发 3 文件上传全部 completed 无错乱；状态自动刷新轮询需用户本机确认）
- [x] 任务失败处理（验证：失败任务返回错误原因（Embedding 404 实测），worker 自动重试至上限；文档状态同步 failed 已修复并经单元测试；损坏文件解析器宽容未触发 failed，重试按钮交互需用户本机确认）
- [x] 文档删除（验证：`DELETE /api/v1/documents/:id` 实测通过）
- [x] API Key 管理（验证：创建（明文一次返回）、启停 toggle、删除接口全部实测通过；前端字段已按 snake_case 对齐）
- [x] 界面交互体验（验证：全中文文案、亮/暗主题 CSS 变量、空状态组件已实现；视觉效果需用户本机确认）
- [ ] 流式中断降级（验证：SSE error 事件降级路径实测通过；停止按钮（AbortController）交互需用户本机确认）

## 集成

- [x] Go 二进制托管前端（验证：将可执行文件复制到无 frontend 源码目录运行，`/` 返回完整界面、静态资源 200——前端已嵌入二进制）
- [x] SPA 深链可访问（验证：`/chat`、`/kb`、`/keys` 刷新均 200）
- [x] `/api/*` 未匹配路径仍返回 JSON 404（验证：curl 实测 `{"code":404,"message":"接口不存在"}`）
- [x] 前端不依赖 Wails 专有 API（验证：`grep` 前端源码无 `window.wails` / `@wailsio` 引用）
- [x] 401 全局处理（验证：无效 Key 请求返回 401；前端拦截器跳登录逻辑已实现，浏览器交互需用户本机确认）
- [x] 任务轮询自动启停（验证：代码实现 3s 轮询 + 无活动任务自动停止；网络面板观测需用户本机确认）

## 编译与测试

- [x] Go 全量编译通过（验证：`go build ./...`）
- [x] Go 全部单元测试通过（验证：`go test ./...`）
- [x] 前端类型检查通过（验证：`npm run build`（含 vue-tsc）无类型错误）
- [x] 静态检查无告警（验证：`go vet ./...`）

## 端到端场景

- [x] 场景 1（Web 全流程）：启动服务 → `GET /` 返回真实前端 → API Key 登录（Bearer 头）→ 新建知识库 → 上传文档 → 任务自动完成（chunk 入库）→ SSE 流式问答返回引用来源 → 会话历史接口正常（浏览器 GUI 交互部分需用户本机确认）
- [~] 场景 2（桌面全流程）：`.app` 打包完成、签名有效、启动链实测到窗口创建阶段（内嵌服务 127.0.0.1:随机端口 + Wails v3 运行时初始化）；窗口内交互因沙箱无 GUI 环境受限，需用户本机确认
- [x] 场景 3（边界：空库问答）：不选知识库提问返回受控错误（LLM 配置无效场景），不崩溃
- [x] 场景 4（边界：并发上传）：3 文件并发上传全部 completed，状态各自正确

## 验收时修复的后端既有问题（非本次需求引入）

1. `store.CreateDocument/UpdateDocumentStatus`：nil ChunkIDs 绑定 NULL 违反 NOT NULL 约束 → 上传 500（internal/store/document.go）
2. 服务启动未调用 `EnsureCollection` → Qdrant 集合不存在导致向量入库失败（internal/app/app.go）
3. `config.local.yaml` embedder base_url 带尾部 `/v1`，与客户端拼接逻辑冲突（configs/config.local.yaml）
4. 任务最终 failed 时未同步文档状态 → 文档列表状态失真（internal/task/worker.go）
5. 文档记录 TaskID 从不回填 → 前端无法从文档定位任务（internal/api/handler_doc.go）
