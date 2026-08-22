# 多模态检索结果查看（Reader + 原文定位）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/chunker/types.go` | ChunkMeta 增 PageNumber/Heading/Anchor |
| 修改 | `internal/chunker/chunker.go` | slugifyHeading、PDF 按页分块、heading/anchor 传递 |
| 修改 | `internal/chunker/strategy_heading.go` | headingSection 增 Heading/Anchor |
| 修改 | `internal/chunker/chunker_test.go` | PDF page / Markdown anchor / slugify 用例 |
| 修改 | `internal/pipeline/pipeline.go` | payload 增 page_number/heading/anchor |
| 修改 | `internal/rag/context.go` | Source 增 PageNumber/Anchor |
| 修改 | `internal/rag/context_test.go` | 定位字段贯通用例 |
| 修改 | `internal/api/handler_chunk.go` | GetChunk 增 page_number/heading/anchor |
| 修改 | `internal/api/handler_doc.go` | 新增 GetRawDocument（Range + MIME + 鉴权） |
| 修改 | `internal/api/router.go` | 注册 /documents/:id/raw |
| 修改 | `internal/api/api_test.go` | raw 接口 + chunk 详情定位字段用例 |
| 修改 | `frontend/src/api/types.ts` | ChatSource/ChunkDetail 定位字段 |
| 修改 | `frontend/src/api/chunk.ts` | ChunkDetail 类型 |
| 修改 | `frontend/src/api/doc.ts` | rawDocumentUrl |
| 新建 | `frontend/src/components/viewer/types.ts` | ViewerType/ViewerLocation/ViewerProps |
| 新建 | `frontend/src/components/viewer/ViewerRegistry.ts` | 注册/解析 |
| 新建 | `frontend/src/components/viewer/DocumentViewer.vue` | 统一入口 |
| 新建 | `frontend/src/components/viewer/PdfViewer.vue` | PDF 阅读器 |
| 新建 | `frontend/src/components/viewer/VideoViewer.vue` | 视频播放器 |
| 新建 | `frontend/src/components/viewer/AudioViewer.vue` | 音频播放器 |
| 新建 | `frontend/src/components/viewer/MarkdownViewer.vue` | Markdown 阅读器 |
| 修改 | `frontend/src/components/SourceCard.vue` | 接入 DocumentViewer |

## T1: ChunkMeta 扩展 + slugifyHeading

**文件：** `internal/chunker/types.go`、`internal/chunker/chunker.go`
**依赖：** 无

**步骤：**
1. `types.go` 的 `ChunkMeta` 新增 `PageNumber int`、`Heading string`、`Anchor string`（注释说明用途）。
2. `chunker.go` 新增包级函数 `slugifyHeading(s string) string`：
   - `strings.TrimSpace`；遍历 rune：`# * _ \` [ ] ( ) >` 跳过；空白（空格/tab/换行/回车）写 `-`；其余原样。
   - 结果为空时返回原输入。

**验证：** `go build ./internal/chunker/...` 编译通过

## T2: headingStrategy 增加 Heading/Anchor

**文件：** `internal/chunker/strategy_heading.go`
**依赖：** T1

**步骤：**
1. `headingSection` 增加 `Heading`、`Anchor` 字段。
2. `SplitByBlocks` 的 `flushSection`：取 `headingStack` 最后一个元素作为 `Heading`（栈空则空串），`Anchor = slugifyHeading(Heading)`；超长降级子 chunk 沿用同一 `Heading/Anchor`。

**验证：** `go build ./internal/chunker/...` 编译通过

## T3: chunker 主流程（heading 传递 + PDF 按页分块）

**文件：** `internal/chunker/chunker.go`
**依赖：** T2

**步骤：**
1. `rawChunk` 增加 `heading`、`anchor`、`pageNumber` 字段。
2. `StrategyHeading` 分支：把 `sec.Heading`、`sec.Anchor` 写入 `rawChunk`。
3. 组装最终 Chunk 时，`ChunkMeta` 写入 `Heading/Anchor/PageNumber`。
4. 新增 `isPagedDoc(blocks)`：存在 `Metadata["page"]` 的 block 且非 media doc。
5. 新增 `chunkPagedBlocks(doc, config)`：按 `page` 分组，每组 `blockToText` 拼接为一个 chunk，`PageNumber=page`；单页 token 超 `ChunkSize` 用 recursive 切分，子 chunk 沿用同 `PageNumber`。
6. `Chunk()` 主流程：media doc 分支之后、文本策略之前，插入 `if isPagedDoc(...) { return chunkPagedBlocks(...) }`。

**验证：** `go build ./internal/chunker/...` 编译通过

## T4: chunker 单测

**文件：** `internal/chunker/chunker_test.go`
**依赖：** T3

**步骤：** 新增表驱动用例：
1. `slugifyHeading`：`"## 模块设计"` → `"模块设计"`；`"A  B"` → `"A--B"`；`"#*_x"` → `"x"`。
2. PDF：构造带 `page` metadata 的 blocks，断言 chunk `PageNumber` 正确、同页合并。
3. Markdown heading：断言 chunk 带 `Heading` 与 `Anchor`。

**验证：** `go test ./internal/chunker/...` 全部通过

## T5: pipeline payload 与 rag.Source

**文件：** `internal/pipeline/pipeline.go`、`internal/rag/context.go`
**依赖：** T3

**步骤：**
1. `pipeline.go` payload 增加 `page_number`、`heading`、`anchor`（取自 `c.Metadata`）。
2. `context.go` `Source` 增加 `PageNumber int`（json `page_number,omitempty`）、`Anchor string`（json `anchor,omitempty`）；`buildContext` 用 `metaInt/metaString` 提取。

**验证：** `go build ./...` 编译通过

## T6: pipeline / rag 测试

**文件：** `internal/rag/context_test.go`
**依赖：** T5

**步骤：** 新增/扩展用例：chunk metadata 含 `page_number/anchor` 时，`Source` 正确透出。

**验证：** `go test ./internal/rag/... ./internal/pipeline/...` 通过

## T7: chunk 详情 + 原文访问接口 + 路由

**文件：** `internal/api/handler_chunk.go`、`internal/api/handler_doc.go`、`internal/api/router.go`
**依赖：** T5

**步骤：**
1. `handler_chunk.go` `GetChunk`：从 payload 读 `page_number/heading/anchor`（复用 `payloadInt` + 类型断言），加入返回。
2. `handler_doc.go` 新增 `GetRawDocument`：
   - `uuid.Parse` 校验 id；`store.GetDocument` + `ensureKBAccess`（失败 404）。
   - 定义 `rawMIMEs` map（`.pdf/.mp3/.wav/.m4a/.flac/.ogg/.aac/.md/.markdown/.txt/.html` 及图片）；`doc.Format` 小写查表，未知 → 400「不支持原文查看」。
   - `os.Open(doc.FilePath)` + `http.ServeContent`（Content-Type + Accept-Ranges）。
3. `router.go`：文档组新增 `v1.GET("/documents/:id/raw", h.GetRawDocument)`。

**验证：** `go build ./...` 编译通过

## T8: api 测试

**文件：** `internal/api/api_test.go`
**依赖：** T7

**步骤：**
1. raw 接口：构造文档记录 + 落盘文件，断言 200 / Content-Type / `Accept-Ranges: bytes`；越权（不同 kb）断言 404；未知扩展名断言 400。
2. chunk 详情：mock payload 含 `page_number/anchor`，断言返回字段。

**验证：** `go test ./internal/api/...` 通过

## T9: 前端类型与 API

**文件：** `frontend/src/api/types.ts`、`frontend/src/api/chunk.ts`、`frontend/src/api/doc.ts`
**依赖：** 无（与后端并行）

**步骤：**
1. `types.ts` `ChatSource` 增加 `source_type/start_ms/end_ms/page_number/anchor?`。
2. `chunk.ts` `ChunkDetail` 增加 `source_type/start_ms/end_ms/page_number/heading/anchor`。
3. `doc.ts` 新增 `rawDocumentUrl(documentId)` 返回 `` `/api/v1/documents/${encodeURIComponent(id)}/raw` ``。

**验证：** `cd frontend && npx vue-tsc --noEmit`（随 T16 一并）

## T10: Viewer 核心

**文件：** `frontend/src/components/viewer/types.ts`、`ViewerRegistry.ts`、`DocumentViewer.vue`
**依赖：** T9

**步骤：**
1. `types.ts`：定义 `ViewerType`、`ViewerLocation`、`ViewerProps`。
2. `ViewerRegistry.ts`：`registerViewer(type, comp)`、`resolveViewer(type)`（Map 存储）。
3. `DocumentViewer.vue`：props `{ documentId, fileType, location, filename }`，`resolveViewer` 后 `<component :is>` 渲染并透传；未注册时显示「暂不支持该类型查看」。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T11: PdfViewer

**文件：** `frontend/src/components/viewer/PdfViewer.vue`
**依赖：** T10

**步骤：**
1. 引入 `pdfjs-dist`；`getDocument({ url: rawDocumentUrl(documentId), httpHeaders: { Authorization } })`。
2. 渲染到 canvas/页面容器，跳转 `location.page`；提供上一页/下一页。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T12: VideoViewer

**文件：** `frontend/src/components/viewer/VideoViewer.vue`
**依赖：** T10

**步骤：**
1. axios 带鉴权 fetch 视频 → `URL.createObjectURL` → `<video controls>`。
2. `onLoadedMetadata` 时 `currentTime = location.startMs/1000`；展示命中区间文本；卸载时 `revokeObjectURL`。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T13: AudioViewer

**文件：** `frontend/src/components/viewer/AudioViewer.vue`
**依赖：** T10

**步骤：**
1. axios 带鉴权 fetch 音频 → objectURL → `wavesurfer.js`（或 `<audio>` + 自绘进度条）加载。
2. `seek(location.startMs/1000)`；高亮命中区间；卸载清理。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T14: MarkdownViewer

**文件：** `frontend/src/components/viewer/MarkdownViewer.vue`
**依赖：** T10

**步骤：**
1. axios fetch 原文文本 → `marked` 渲染（自定义 heading renderer 生成 `id=slugifyHeading(text)`）。
2. 前端实现 `slugifyHeading`（与后端同规则）；按 `location.anchor` `getElementById` 滚动/高亮。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T15: SourceCard 接入

**文件：** `frontend/src/components/SourceCard.vue`
**依赖：** T11-T14

**步骤：**
1. 点击结果 → `getChunk(id)` 取 `document_id/source_type/page_number/start_ms/end_ms/anchor`。
2. 由 `source_type`（video/audio）或扩展名（pdf/md）推断 `fileType`；组装 `location`。
3. 有对应 Viewer → 打开 `DocumentViewer` 弹窗；否则回退现有纯文本 `<pre>` 弹窗。

**验证：** `cd frontend && npx vue-tsc --noEmit`

## T16: 前端构建与全量回归

**文件：** 无
**依赖：** T15

**步骤：**
1. `cd frontend && npm run build`
2. `cd frontend && npm run test`
3. `go build ./... && go test ./...`
4. `gofmt -l internal/chunker internal/pipeline internal/rag internal/api` 无输出

**验证：** 上述全部通过

## 执行顺序

```
T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8
T9 → T10 → T11 ─┐
              T12 ├─→ T15 → T16
              T13 │
              T14 ┘
```
