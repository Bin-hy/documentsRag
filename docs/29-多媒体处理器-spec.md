# 多媒体处理器（图片/音频/视频）Spec

## 背景

BinRag 是 Go 实现的企业知识库问答系统，入库链路为：上传 → Load（loader）→ Chunk → Embed → Store，检索走文本向量 + BM25。
`internal/loader` 已支持 txt / markdown / pdf / docx / csv / excel / html 七种文本类格式，并已接入上传校验与入库 pipeline。
企业知识库中普遍存在图片（截图、扫描件、设计稿）、音频（会议录音、语音备忘）、视频（培训录像、操作演示）等非文本资料，目前这些格式无法上传，知识无法被检索问答。
现有 `internal/llm` 为纯文本 Chat 能力，无多模态/语音接口；配置体系为「Provider + BaseURL + APIKey」模式。

## 目标

- G1：新增图片/音频/视频三类 Processor，复用 loader 框架（Parser 接口 + 注册表），现有 7 种文本格式行为不变。
- G2：Processor 职责边界——只负责「原始媒体 → 结构化 Document（Blocks + Metadata）」，不负责 Chunk / Embed / Store（仍在 pipeline 中完成）。
- G3：输出为可检索的结构化内容：文本描述/转写进入 Block.Content 参与现有 Chunk/Embed/Retrieval；结构化 metadata 进入 Block.Metadata / DocumentMeta.Extra，为未来多模态 RAG 保留扩展接口。
- G4：multimedia 独立配置节 + Provider 抽象：视觉理解、语音转写属于 ingestion 阶段能力，与问答阶段的 llm 职责分离；不绑定具体厂商，支持 OpenAI Compatible、本地 VLM、Whisper 等实现。
- G5：未配置对应能力时上传明确失败（返回配置缺失错误），不静默跳过、不产生空文档或脏数据。

## 功能需求

- F1 Processor 注册与接入：新增图片/音频/视频三个 Parser，注册进 `NewDefaultRegistry`；注册与解析同时基于扩展名与 MIME Type（双路匹配），避免仅依赖后缀导致错误类型文件进入处理链路。
- F2 支持格式：图片 `.png .jpg .jpeg .webp .gif .bmp`；音频 `.mp3 .wav .m4a .flac .ogg .aac`；视频 `.mp4 .avi .mkv .mov .webm`。MIME 表与之一一对应。
- F3 图片处理：调用 `multimedia.vision` 模型生成视觉理解文本（含版面/文字识别能力）。输出一个或多个结构化 Block：Block.Content 保存视觉理解文本，Block.Metadata 保存图片类型、宽高尺寸、来源；新增 BlockType `image_description` 标记图片类 Block，便于未来扩展多模态检索。
- F4 音频处理：调用 `multimedia.speech` 语音转写模型生成转写文本；按时间戳分段写入多个 Block，每段 Block.Metadata 携带起止时间戳；DocumentMeta.Extra 记录总时长、来源。
- F5 视频处理：ffmpeg 抽帧 → 逐帧调用 `vision` 生成描述。抽帧策略可配置（默认固定时间间隔；预留场景变化检测策略扩展），禁止默认逐帧调用，避免长视频造成海量模型调用。视觉轨与音频轨独立：未配置 speech 时仍处理视觉部分，仅跳过音频转写并记录 warning（不阻断）。metadata 记录时长、分辨率、关键帧时间点列表。
- F6 multimedia 配置与 Provider 抽象：新增独立 `multimedia` 配置节（不复用 llm），含 `vision`（图片/视频帧理解）与 `speech`（语音转写；命名预留 speaker diarization、时间戳对齐等扩展）。每组服务配置 Provider / BaseURL / APIKey / Model。Provider 采用接口抽象，默认实现 OpenAI Compatible（`/v1/chat/completions` 视觉 + `/v1/audio/transcriptions`），保留本地 VLM / Whisper 扩展能力。
- F7 能力缺失与失败语义：未配置 `vision` → 图片/视频上传 400「multimedia.vision 未配置」；未配置 `speech` → 音频上传 400「multimedia.speech 未配置」（视频降级见 F5）。服务调用失败（网络/鉴权/超时）→ 上传失败并返回可读错误；不产生空文档/脏数据（复用 ValidateReadable 预检兜底）。
- F8 检索与问答：文本化后的多媒体内容参与现有向量 + BM25 检索，引用来源展示为原文件名；不新增检索路径。
- F9 统一 Document Block 模型：多媒体 Processor 输出与文本 Loader 完全同构的 Document{Blocks, Metadata}。Processor 只负责「原始数据 → Document」，Block.Content 进入现有 Chunk/Embed/Retrieval，Block.Metadata 保存扩展信息；不引入第二套处理流程。

## 非功能需求

- N1 性能：视频抽帧频率受配置约束（默认固定间隔，避免长视频大量 VLM 调用）；多媒体服务调用带超时控制，超时按 F7 失败语义处理。
- N2 兼容性：现有 7 种文本格式的上传、入库、检索行为完全不变（回归安全）。
- N3 可扩展性：Provider 接口抽象，可插拔实现（OpenAI Compatible / 本地 VLM / Whisper）；BlockType 可扩展。
- N4 可观测性：配置缺失/服务失败有明确、可读的错误信息；视频音轨降级等非阻断问题记录 warning（随任务状态可见）。
- N5 安全：复用现有 UploadMaxSizeMB 文件大小限制；ffmpeg 参数以受控方式构造（不拼接用户输入），防止参数注入。
- N6 配置向后兼容：新增 `multimedia` 配置节不影响现有配置加载与校验。

## 不做的事

- 不做多模态向量检索（本阶段统一转文本；metadata 与 BlockType 已预留扩展接口）。
- 不做 speaker diarization / 说话人分离、字幕对齐等高级语音能力（speech 配置命名已预留）。
- 不做独立 OCR 引擎（视觉模型的文字识别能力已覆盖）。
- 不做 GIF 动图逐帧分析（按静态图处理）。
- 不做 PDF 内嵌图片的提取分析（PDF 保持现有行为）。
- 不内置本地模型（仅提供 Provider 接口与 OpenAI Compatible 默认实现）。

## 验收标准

- AC1（F1/F2）：图片/音频/视频三类格式可上传，经 loader 解析出 Document 并走完 Chunk→Embed→Store 入库成功；未知扩展名/错误 MIME 依旧被拒。
- AC2（F3）：上传图片后，文档含类型为 `image_description` 的 Block，Content 为视觉描述文本，Metadata 含类型、宽高、来源。
- AC3（F4）：上传音频后，转写文本按时间戳分段，每段 Block 含起止时间戳 metadata，文档 Extra 含总时长。
- AC4（F5）：上传视频后，按配置间隔产出关键帧描述 Blocks，metadata 含关键帧时间点列表；未配置 speech 时视觉部分照常入库且任务带 warning。
- AC5（F6/F7）：`multimedia.vision` 未配置时图片/视频上传返回 400 及明确配置缺失信息；`multimedia.speech` 未配置时音频上传同理。
- AC6（F7）：模拟服务调用失败（超时/鉴权失败），上传/入库返回可读错误，库中无空文档/脏数据记录。
- AC7（F8）：入库的多媒体内容可被检索命中，引用来源为原文件名。
- AC8（N2）：现有 txt/markdown/pdf/docx/csv/excel/html 上传入库行为不变（全量回归通过）。
- AC9（F9）：多媒体 Processor 输出 Document{Blocks, Metadata} 与文本 loader 同构，无第二套处理流程。
