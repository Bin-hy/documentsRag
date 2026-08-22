# 多模态检索结果查看（Reader + 原文定位）Plan

## 架构概览

分「后端定位信息贯通」与「前端统一阅读器」两条线，中间由原文文件访问接口衔接：

- **后端定位链路**：`loader.Block.Metadata` → `chunker.ChunkMeta` → `pipeline` payload → `rag.Source` / `chunk 详情`。本次补 `page_number`（PDF）、`heading/anchor`（Markdown），视频/音频的 `start_ms/end_ms` 已贯通无需改。
- **原文文件访问**：新增统一 `GET /api/v1/documents/{id}/raw`（Range + MIME + 知识库鉴权）；视频保留既有 `/videos/{id}/stream`。
- **前端 Reader**：`ViewerRegistry`（注册表）+ `DocumentViewer`（统一入口）+ 四个 Viewer 组件（PDF/Video/Audio/Markdown），第三方组件经 Adapter 接入。

## 核心数据结构

### ChunkMeta 扩展（internal/chunker/types.go）

```go
type ChunkMeta struct {
    DocFilename    string
    HeadingContext string // 所属标题上下文路径（现有）
    TokenCount     int
    SourceType     string // video/audio/image/""（现有）
    StartMs        int64
    EndMs          int64
    PageNumber     int    // 新增：PDF 页码（从 1 开始）
    Heading        string // 新增：Markdown 当前块最近一级标题文本
    Anchor         string // 新增：标题锚点（slugify 后的 heading）
}
```

### Source 扩展（internal/rag/context.go）

```go
type Source struct {
    ID         string
    Filename   string
    Heading    string  // 现有（标题上下文）
    Score      float32
    SourceType string  // 现有
    StartMs    int64
    EndMs      int64
    PageNumber int     `json:"page_number,omitempty"` // 新增
    Anchor     string  `json:"anchor,omitempty"`      // 新增
}
```

### 前端 Viewer 抽象（frontend/src/components/viewer/）

```ts
// types.ts
export type ViewerType = 'pdf' | 'video' | 'audio' | 'markdown'
export interface ViewerLocation {
  page?: number            // pdf
  startMs?: number         // video / audio
  endMs?: number
  anchor?: string          // markdown
  heading?: string
}
export interface ViewerProps {
  documentId: string
  filename: string
  location: ViewerLocation
}

// ViewerRegistry.ts
registerViewer(type: ViewerType, comp: Component)
resolveViewer(type: ViewerType): Component | undefined
```

### 原文访问响应

`GET /api/v1/documents/{id}/raw` → `http.ServeContent`，Content-Type 按扩展名映射，`Accept-Ranges: bytes`，鉴权失败/越权/非文件一律 404。

## 模块设计

### 模块 A：chunker 定位信息贯通（internal/chunker，修改）

**职责：** 让 PDF chunk 带 `PageNumber`、Markdown chunk 带 `Heading/Anchor`。

**改动：**
1. `types.go` 的 `ChunkMeta` 增加 `PageNumber/Heading/Anchor`。
2. `chunker.go`：
   - 检测文档含带 `page` metadata 的 block（PDF）时，走「按页分块」：同页 block 合并为一个 chunk，`PageNumber=page`；单页超长时按 token 再切，子 chunk 沿用同 `PageNumber`。
   - Markdown `heading` 策略：每个 section 记录最近标题文本，`Heading=标题`、`Anchor=slugify(标题)`。
   - 新增 `slugifyHeading(text)`（与前端同一规则，见技术决策）。

**依赖：** `loader.Block.Metadata["page"]`（parser_pdf 已产出）。

### 模块 B：pipeline 与检索来源透出（internal/pipeline、internal/rag，修改）

**改动：**
1. `pipeline.go` payload 增加 `page_number` / `heading` / `anchor`。
2. `rag/context.go` `Source` 增加 `PageNumber/Anchor`，`buildContext` 从 chunk metadata 提取（复用 `metaInt/metaString`）。

### 模块 C：chunk 详情与原文访问接口（internal/api，修改）

**改动：**
1. `handler_chunk.go` `GetChunk` 增加返回 `page_number/heading/anchor`。
2. `handler_doc.go` 新增 `GetRawDocument`：
   - 按 `id` 查文档 + `ensureKBAccess`（越权/不存在 404）。
   - 按 `doc.Format` 映射 MIME（pdf/音频/markdown/文本/图片）。
   - `http.ServeContent` 输出（原生 Range / 206）。
3. `router.go` 新增 `v1.GET("/documents/:id/raw", h.GetRawDocument)`。

### 模块 D：前端类型与 API（frontend/src/api，修改）

**改动：**
1. `types.ts`：`ChatSource` 增加 `source_type/start_ms/end_ms/page_number/anchor`；新增 `ChunkDetail` 字段对齐（`page_number/heading/anchor/source_type/start_ms/end_ms`）。
2. `chunk.ts`：`ChunkDetail` 接口补字段。
3. `doc.ts`：新增 `rawDocumentUrl(documentId)` 返回原文访问 URL。

### 模块 E：前端 Viewer 核心（frontend/src/components/viewer/，新建）

**职责：** 统一 Reader 注册与入口。

**文件：**
- `types.ts`：`ViewerType/ViewerLocation/ViewerProps`。
- `ViewerRegistry.ts`：`registerViewer/resolveViewer`。
- `DocumentViewer.vue`：接收 `{documentId, fileType, location}`，resolve 组件并渲染，把 `documentId/location/filename` 传给子 Viewer。

### 模块 F：四个 Viewer 组件（frontend/src/components/viewer/，新建）

- `PdfViewer.vue`：pdfjs-dist 加载 `rawDocumentUrl(documentId)`（httpHeaders 带鉴权），跳 `location.page`。
- `VideoViewer.vue`：axios 带鉴权 fetch 视频 → objectURL → `<video>`，`seek(startMs)`，展示命中区间。
- `AudioViewer.vue`：wavesurfer.js + `<audio>`，`seek(startMs)`，高亮命中区间。
- `MarkdownViewer.vue`：axios fetch 文本 → marked 渲染（自定义 heading renderer 生成 `id=slugify(heading)`）→ 定位 `location.anchor` 滚动/高亮。

### 模块 G：SourceCard 接入（frontend/src/components/SourceCard.vue，修改）

**改动：** 点击结果 → `getChunk(id)` 取 `document_id/source_type/定位字段` → 按 `source_type`（video/audio/…）或扩展名推断 `fileType` → 打开 `DocumentViewer`；无对应 Viewer 时回退现有纯文本弹窗。

## 模块交互

```
点击检索结果（SourceCard）
  └─ getChunk(chunk_id) → {document_id, source_type, page_number/start_ms/anchor, ...}
  └─ DocumentViewer({documentId, fileType, location})
       └─ ViewerRegistry.resolve(fileType)
            ├─ PdfViewer → pdfjs(rawDocumentUrl, httpHeaders) → 跳页
            ├─ VideoViewer → fetch(raw) → <video> seek startMs
            ├─ AudioViewer → wavesurfer + <audio> seek startMs
            └─ MarkdownViewer → fetch(raw) → marked 渲染 → 定位 anchor
```

## 文件组织

```
internal/chunker/
├── types.go         — 修改：ChunkMeta 增 PageNumber/Heading/Anchor
├── chunker.go       — 修改：PDF 按页分块、Markdown heading/anchor、slugifyHeading
└── chunker_test.go  — 修改：补 PDF page、Markdown anchor 用例

internal/pipeline/
└── pipeline.go      — 修改：payload 增 page_number/heading/anchor

internal/rag/
├── context.go       — 修改：Source 增 PageNumber/Anchor
└── context_test.go  — 修改：补定位字段贯通用例

internal/api/
├── handler_chunk.go — 修改：GetChunk 增 page_number/heading/anchor
├── handler_doc.go   — 修改：新增 GetRawDocument（Range + MIME + 鉴权）
├── router.go        — 修改：注册 /documents/:id/raw
└── api_test.go      — 修改：raw 接口 + chunk 详情定位字段用例

frontend/src/
├── api/types.ts     — 修改：ChatSource/ChunkDetail 定位字段
├── api/chunk.ts     — 修改：ChunkDetail 类型
├── api/doc.ts       — 修改：rawDocumentUrl
├── components/
│   ├── SourceCard.vue — 修改：接入 DocumentViewer
│   └── viewer/
│       ├── types.ts          — 新建
│       ├── ViewerRegistry.ts — 新建
│       ├── DocumentViewer.vue — 新建
│       ├── PdfViewer.vue     — 新建
│       ├── VideoViewer.vue   — 新建
│       ├── AudioViewer.vue   — 新建
│       └── MarkdownViewer.vue — 新建
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| PDF 页码贯通 | chunker 识别 block 的 `page` metadata，按页分块，`PageNumber` 透传 | parser_pdf 已按页产出 block，改动最小且语义正确 |
| Markdown anchor | `slugifyHeading`：去首尾空白、内部空白转 `-`、移除 `#*_`\`\`[]()> ` 等，中文保留 | HTML5 id 允许中文，前后端同一规则即可精确定位，避免复杂拼音 slug |
| 原文接口 | 新增统一 `GET /documents/:id/raw`（Range + MIME + 鉴权），视频保留既有 stream | 阅读器统一加载入口；复用 `http.ServeContent` 原生 Range |
| 前端加载方式 | PDF 用 pdfjs（httpHeaders 带鉴权，内部 Range 分页）；视频/音频 axios fetch 为 Blob + objectURL；Markdown fetch 文本 | 浏览器 media/iframe 无法带 Authorization header；上传有 50MB 上限，视频/音频整体加载可接受且本地 seek 流畅（对 spec N2 的落地：PDF 分页按需加载，视频/音频因大小受限整体加载） |
| 组件选型 | pdfjs-dist / HTML5 video / wavesurfer.js / marked（已有） | 成熟组件，需求文档「第一优先级」 |
| Reader 抽象 | `ViewerRegistry`（map）+ `DocumentViewer` 统一入口 | 可插拔，新增类型只 register 不改核心 |
| 类型推断 | 以 `source_type`（video/audio/image）优先，PDF/Markdown 按扩展名推断 | chunk 详情已含 source_type；文本类需按文件名 |
| 回退 | 无对应 Viewer 时回退现有纯文本弹窗 | 保证 image/txt 等未实现类型仍可查看 |
