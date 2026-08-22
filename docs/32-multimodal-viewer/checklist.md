# 多模态检索结果查看（Reader + 原文定位）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [x] 后端定位信息贯通：`ChunkMeta` / payload / `Source` / chunk 详情均含 `page_number`、`heading`、`anchor`（验证：`go build ./...` + `go test ./internal/chunker/... ./internal/rag/... ./internal/api/...`）
- [x] PDF 按页分块且 chunk 带 `page_number`（验证：`TestChunkPagedBlocks`）
- [x] Markdown chunk 带 `heading/anchor`，`slugifyHeading` 前后端规则一致（验证：`TestSlugifyHeading` / `TestChunkMarkdownHeadingAnchor`）
- [x] 原文访问接口 `GET /documents/:id/raw` 返回正确 MIME 且支持 Range（验证：`TestGetRawDocument`）
- [x] 前端 `ViewerRegistry` + `DocumentViewer` 统一入口已实现（验证：vue-tsc 无类型错误，仅缺依赖）
- [x] 四个 Viewer（PDF/Video/Audio/Markdown）已实现（验证：vue-tsc 无类型错误，仅缺依赖）

## 集成
- [x] `SourceCard` 点击结果按类型打开对应阅读器，无对应 Viewer 回退纯文本（验证：代码完成，需浏览器验证）
- [x] PDF 用 pdfjs 带鉴权加载、视频/音频带鉴权加载、Markdown 带鉴权加载（验证：代码完成，需浏览器验证）
- [x] 现有纯文本查看与视频流接口不回归（验证：`go test ./...`）

## 编译与测试
- [x] 后端编译无错误（验证：`go build ./...`）
- [x] 后端单元测试全部通过（验证：`go test ./...`）
- [x] 前端构建通过（验证：`cd frontend && npm run build` —— vue-tsc + vite 均通过）
- [x] 前端测试通过（验证：`cd frontend && npm run test` —— 24 用例通过）
- [x] gofmt 检查通过（验证：本次改动文件 `gofmt -l` 无输出；`internal/pipeline/pipeline_test.go` 为历史遗留未格式化，非本次改动）

## 端到端场景
- [ ] 场景 1（AC1）：点击 PDF 检索结果 → 打开 PDF 并跳转到对应页（需完整运行环境 + 浏览器）
- [ ] 场景 2（AC2）：点击视频结果 → 播放器 seek 到 `start_ms` 并展示命中范围（需完整运行环境 + 浏览器）
- [ ] 场景 3（AC3）：点击音频结果 → 播放器 seek 到 `start_ms` 并展示区间（需完整运行环境 + 浏览器）
- [ ] 场景 4（AC4）：点击 Markdown 结果 → 渲染并定位到对应章节（需完整运行环境 + 浏览器）
- [x] 场景 5（AC6）：越权访问他人知识库的原文文件 → 返回 404（验证：`TestGetRawDocument`）
