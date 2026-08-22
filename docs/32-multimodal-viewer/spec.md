# 多模态检索结果查看（Reader + 原文定位）Spec

## 背景

- 检索结果当前只能查看 chunk 纯文本：前端 `SourceCard` 弹窗用 `<pre>` 展示 chunk 内容，无法回到原始文件的具体位置查看/播放原文。
- 后端已贯通视频/音频定位信息：chunk payload 与 chunk 详情接口已返回 `source_type / start_ms / end_ms`；视频原文已有支持 HTTP Range 的流式接口（`/videos/{id}/stream`）。
- 缺口：PDF 未存 `page_number`；Markdown 未存 `anchor`；PDF / 音频 / Markdown 缺少原文文件流接口；前端无专用阅读器。

## 目标

- 检索结果 → 按来源文件类型打开对应专用阅读器 → 定位到原文位置（PDF 页码 / 视频时间 / 音频时间 / Markdown 章节）。
- 建立统一的 Reader 抽象与可插拔注册机制。
- 补全 PDF 页码与 Markdown anchor 的索引定位 metadata，并补齐原文文件访问接口。

## 功能需求

- F1：检索结果携带来源文件类型与定位信息，前端据此选择对应阅读器。
- F2：提供统一阅读器入口，按文件类型解析出对应 Reader 并把定位信息传给它。
- F3：PDF 阅读器：加载 PDF 原文、跳转到指定页码、支持翻页与命中位置定位。
- F4：视频阅读器：加载视频、seek 到 `start_ms`、展示命中时间范围。
- F5：音频阅读器：加载音频、seek 到 `start_ms`、展示命中时间区间（含波形可视化）。
- F6：Markdown 阅读器：按 Markdown 结构渲染（标题/段落/代码块/表格/列表/引用等），定位到对应章节并滚动/高亮。
- F7：索引 metadata 补全：PDF 保存 `page_number`，Markdown 保存 `heading` 与 `anchor`；视频/音频沿用已有 `start_ms/end_ms`。
- F8：提供原文文件访问接口（PDF / 音频 / Markdown，含 HTTP Range 与鉴权）；视频复用现有流式接口。
- F9：阅读器可插拔（注册机制），第三方前端组件通过 Adapter 接入，不写死 `if PDF / if Video ...` 分支。

## 非功能需求

- N1：优先使用成熟前端组件（PDF.js / HTML5 video / 音频 waveform / Markdown 渲染），通过 Adapter 隔离第三方依赖。
- N2：视频、音频、PDF 等大文件通过 HTTP Range 流式加载，不整包下载。
- N3：原文访问接口鉴权与知识库访问控制一致，越权一律 404，不泄露文件存在性。
- N4：时间统一使用毫秒（ms）；页码从 1 开始。

## 不做的事

- 不做 FFmpeg Docker 化（独立子项目，另行立项）。
- 不做 Word / Excel / PowerPoint / HTML / Image 阅读器（需求文档列为后续扩展）。
- 不做 PDF 全文搜索、目录、缩放等高级能力（后续扩展）。
- 不做多结果在同一视频/音频 Timeline 上的多段标记（本次仅单个命中时间范围）。

## 验收标准

- AC1：点击 PDF 检索结果 → 打开 PDF 阅读器并跳转到对应页。
- AC2：点击视频检索结果 → 播放器 seek 到 `start_ms` 并展示命中时间范围。
- AC3：点击音频检索结果 → 播放器 seek 到 `start_ms` 并展示命中区间。
- AC4：点击 Markdown 检索结果 → Markdown 阅读器渲染并定位到对应章节。
- AC5：各类型阅读器通过统一入口 + 注册机制接入，新增阅读器类型不改核心逻辑。
- AC6：原文访问接口对 PDF/音频/Markdown 返回正确 MIME 并支持 Range；鉴权失败/越权返回 404。
- AC7：后端索引链路为 PDF 写 `page_number`、为 Markdown 写 `heading/anchor`，chunk 详情与检索来源返回这些字段。
- AC8：现有纯文本查看与视频流不回归（回归测试通过）。
