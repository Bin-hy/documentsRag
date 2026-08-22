# 多媒体处理器（图片/音频/视频）Plan

## 架构概览

沿用现有分层，只做「新增能力 + 复用链路」：

```
上传(multipart)
   │ 扩展名/MIME 解析 → Registry 命中多媒体 Parser
   ▼
多媒体 Parser（loader 包）──依赖抽象──▶ VisionProvider / SpeechProvider / FrameExtractor（接口）
   │ 产出 Document{Blocks, Metadata}（与文本 loader 同构）
   ▼
pipeline.Ingest（不变的主体：ValidateReadable → Chunk → Embed → Store → BM25）
   │ 新增：透传 LoadResult.Warnings
   ▼
worker → task.warning_message → API 可见
```

- loader 包：新增 3 个 Parser 实现 + 能力接口定义（VisionProvider/SpeechProvider/FrameExtractor）——接口定义放 loader 包，保证 Parser 只依赖抽象，loader 不反向依赖实现包。
- multimedia 包（新）：OpenAI Compatible 的 vision / speech 实现 + ffmpeg 抽帧器；app.go 装配层根据配置构造 Provider 注入 Parser。
- config 包：新增 MultimediaConfig 纯数据结构（vision / speech / 抽帧间隔）。
- 不改：chunker（新 BlockType 走 blockToText default 分支）、registry 机制、检索链路、API 路由。

## 核心数据结构

```go
// loader/types.go（修改）——新增 BlockType，其余不变
const (
    BlockParagraph BlockType = iota
    BlockHeading
    BlockListItem
    BlockCode
    BlockTable
    BlockImageDescription // 图片/视频帧视觉描述
    BlockAudioSegment     // 音频转写分段
)

// loader/capability.go（新建）——能力抽象接口
type VisionProvider interface {
    Describe(ctx context.Context, image []byte, opts VisionOptions) (string, error)
}
type VisionOptions struct{ Filename string }

type SpeechSegment struct {
    StartMs int64 // 起止时间戳（毫秒）
    EndMs   int64
    Text    string
}
type SpeechProvider interface {
    Transcribe(ctx context.Context, audio []byte, opts SpeechOptions) ([]SpeechSegment, error)
}
type SpeechOptions struct{ Filename string }

type VideoFrame struct {
    TimeMs int64 // 关键帧时间点
    Data   []byte
}
type FrameExtractor interface {
    ExtractFrames(ctx context.Context, videoPath string, intervalSec int) ([]VideoFrame, error)
}

type MediaCapabilityChecker interface {
    CheckCapabilities() error // 缺配置返回 *ErrMediaCapabilityMissing
}

// loader/errors.go（修改）
type ErrMediaCapabilityMissing struct {
    Capability string // "vision" / "speech"
}
// Error(): "multimedia.vision 未配置，无法处理该类型文件"
```

```go
// config（修改）——独立配置节，不复用 llm
type MultimediaConfig struct {
    Vision           MultimediaServiceConfig `yaml:"vision"`
    Speech           MultimediaServiceConfig `yaml:"speech"`
    FrameIntervalSec int                     `yaml:"frame_interval_sec"` // 默认 10
}
type MultimediaServiceConfig struct {
    Provider string `yaml:"provider"` // openai_compat（默认）
    BaseURL  string `yaml:"base_url"`
    APIKey   string `yaml:"api_key"`
    Model    string `yaml:"model"`
    Timeout  int    `yaml:"timeout"` // 秒，默认 30
}
func (s MultimediaServiceConfig) Available() bool { return s.APIKey != "" }
```

```go
// store（修改）——任务 warning 透传
type Task struct {
    // ...现有字段
    WarningMessage string // 新增：ALTER TABLE tasks ADD COLUMN IF NOT EXISTS warning_message TEXT NOT NULL DEFAULT ''
}
```

## 模块设计

### 模块 A：图片 Parser（loader/parser_image.go）
- 职责：读图 → 调用 VisionProvider.Describe → 产出 Block（BlockImageDescription + metadata 含类型/宽高/来源）。
- 宽高通过 image.DecodeConfig（标准库）读取。
- 依赖：VisionProvider（构造注入 NewImageParser(v VisionProvider)）。

### 模块 B：音频 Parser（loader/parser_audio.go）
- 职责：读音频 → 调用 SpeechProvider.Transcribe → 按段产出多个 BlockAudioSegment（metadata 含 StartMs/EndMs）→ DocumentMeta.Extra 记录总时长（末段 EndMs）、来源。
- 依赖：SpeechProvider。

### 模块 C：视频 Parser（loader/parser_video.go）
- 职责：reader 落临时文件 → FrameExtractor.ExtractFrames → 逐帧 VisionProvider.Describe → 每帧一个 BlockImageDescription（metadata 含 TimeMs、分辨率、来源）→ 若配置了 speech，追加音轨转写 Blocks；未配置 speech 则跳过并追加 warning → 清理临时文件。
- 依赖：VisionProvider、FrameExtractor、（可选）SpeechProvider。

### 模块 D：multimedia 包（internal/multimedia）
- openai_vision.go：OpenAI Compatible /v1/chat/completions，messages 携带 image_url（base64 data URI），prompt 固定为「描述图片内容，包括版面与可识别文字」。
- openai_speech.go：OpenAI Compatible /v1/audio/transcriptions（whisper 模型），multipart 上传，返回带起止时间的分段（timestamp_granularities[]=segment）。
- frame_extractor.go：exec.Command("ffmpeg", ...) 固定间隔抽帧（-vf fps=1/interval），输出 JPEG 到临时目录；参数数组化构造，不拼接 shell。
- 构造入口：NewVisionProvider(cfg) / NewSpeechProvider(cfg) / NewFrameExtractor()。

### 模块 E：装配与上传校验（app.go / handler_doc.go）
- app.go：读 cfg.Multimedia → 构造 providers → loader.NewDefaultRegistry(cfg.Multimedia)（签名扩展，兼容无参调用）；pipeline 与 API 共用同一注册表。
- handler_doc.go precheckReadable：多媒体 Parser 实现 MediaCapabilityChecker，预检时先 CheckCapabilities()（缺失即 400），命中多媒体类型则跳过真实解析（避免上传+入库双次 VLM 调用）；入库阶段由 pipeline 的 ValidateReadable 兜底防脏数据。

## 模块交互

**上传阶段（同步，handler）：**
```
UploadDocument
  → h.registry.Resolve(info)            // 扩展名/MIME 命中多媒体 Parser
  → precheckReadable(file, parser)
       → 若 parser 实现 MediaCapabilityChecker：
            CheckCapabilities()         // 缺失 → 400「multimedia.vision/speech 未配置」
            跳过真实解析（省一次 VLM 调用）
       → 否则（文本 parser）：现状不变（真实解析 + ValidateReadable）
  → 保存文件、创建 document/task 记录（现状不变）
```

**入库阶段（异步，worker → pipeline）：**
```
worker.process
  → pipeline.Ingest(req)
       → loader.Load → 多媒体 Parser 解析：
            图片:   vision.Describe → Blocks(image_description)
            音频:   speech.Transcribe → Blocks(audio_segment, 含时间戳)
            视频:   FrameExtractor.ExtractFrames → 逐帧 vision.Describe
                   （speech 可用时附加音轨转写；否则 warning 追加进 result.Warnings）
       → ValidateReadable（兜底防脏数据，空描述拒绝）
       → chunker.Chunk → embedder.Embed → vectorstore.Upsert → bm25.Add（现状不变）
       → 返回 (chunkIDs, result.Warnings)          // 变更：Warnings 透传
  → worker：warnings 非空 → task.WarningMessage = join("; ") → UpdateTask
```

**查询阶段：** 无改动（文本化内容走现有向量 + BM25，来源展示原文件名）。

## 文件组织

```
internal/loader/
├── capability.go     — 新建：VisionProvider/SpeechProvider/FrameExtractor 接口、
│                         SpeechSegment/VideoFrame/MediaCapabilityChecker
├── parser_image.go   — 新建：图片 Parser（vision.Describe + image.DecodeConfig 尺寸）
├── parser_audio.go   — 新建：音频 Parser（speech.Transcribe 按时间戳分段）
├── parser_video.go   — 新建：视频 Parser（临时文件 → 抽帧 → 逐帧描述 → 可选音轨转写）
├── types.go          — 修改：新增 BlockImageDescription / BlockAudioSegment 两个 BlockType
├── errors.go         — 修改：新增 ErrMediaCapabilityMissing
├── loader.go         — 修改：NewDefaultRegistry 接受可选的 MultimediaConfig（默认无多媒体能力）
└── （registry.go / validate.go / 现有 parser 不变）

internal/multimedia/  — 新建包（实现层，依赖 loader 的接口 + config 的数据结构）
├── provider.go       — 新建：NewVisionProvider / NewSpeechProvider / NewFrameExtractor 构造
├── openai_vision.go  — 新建：/v1/chat/completions 视觉实现（base64 data URI）
├── openai_speech.go  — 新建：/v1/audio/transcriptions 实现（timestamp_granularities=segment）
└── frame_extractor.go— 新建：ffmpeg 固定间隔抽帧（exec.Command 数组传参，JPEG 输出）

internal/config/config.go  — 修改：新增 MultimediaConfig / MultimediaServiceConfig + applyDefaults + Validate
internal/api/handler_doc.go — 修改：precheckReadable 支持 MediaCapabilityChecker
internal/pipeline/pipeline.go — 修改：Ingest 返回 (chunkIDs, warnings, err)，透传 result.Warnings
internal/task/worker.go — 修改：warnings 写入 task.WarningMessage
internal/store/store.go  — 修改：Task 增加 WarningMessage 字段
internal/store/schema.go — 修改：ALTER TABLE tasks ADD COLUMN IF NOT EXISTS warning_message TEXT NOT NULL DEFAULT ''
internal/app/app.go      — 修改：装配 cfg.Multimedia → providers → NewDefaultRegistry(cfg)
Dockerfile / Dockerfile.deploy — 修改：安装 ffmpeg
docs/29-多媒体处理器-plan.md — 本文件
```

测试文件（随各任务）：
```
internal/loader/parser_image_test.go / parser_audio_test.go / parser_video_test.go — mock provider
internal/loader/capability_test.go   — 能力缺失错误语义
internal/multimedia/frame_extractor_test.go — 抽帧命令构造与输出
internal/config/config_test.go       — multimedia 默认值/校验
internal/pipeline/pipeline_test.go   — warnings 透传、多媒体入库（mock）
internal/api/handler_doc_test.go     — 未配置能力 400、上传成功、错误 MIME 拒绝
```

## 技术决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| D1 | 能力接口定义位置 | 放 loader 包，实现放 multimedia 包 | Parser 只依赖抽象、loader 不反向依赖实现，消除循环依赖；多媒体实现可插拔替换 |
| D2 | 配置节 | 独立 multimedia.vision / multimedia.speech，不复用 llm | 视觉/语音是 ingestion 阶段能力，与问答阶段 llm 职责分离（spec G4）；复用 llm 会让「未配 llm 但配了多媒体」等状态纠缠 |
| D3 | 上传预检策略 | 多媒体只做 CheckCapabilities()，跳过真实解析 | 预检真实解析 = 上传与入库各一次 VLM 调用，成本翻倍；入库阶段 ValidateReadable 仍兜底防脏数据（F7/AC6 满足） |
| D4 | 视频抽帧 | ffmpeg 外部命令 + 固定时间间隔（默认 10s） | Go 无成熟纯 Go 视频解码库；ffmpeg 功能全、部署加一行；间隔可配，避免长视频海量 VLM 调用（N1） |
| D5 | ffmpeg 安全 | exec.Command 数组传参 + 服务端临时文件，不拼接 shell、不接收用户路径 | 防参数注入（N5）；临时文件由 os.CreateTemp 生成 |
| D6 | warning 透传 | pipeline.Ingest 返回值扩展为 (chunkIDs, warnings, err)；tasks 表加 warning_message 列 | LoadResult.Warnings 目前被丢弃；需让视频音轨降级等非阻断问题随任务可见（F5/N4/AC4）。Ingest 仅 worker 一个生产调用方，签名变更可控 |
| D7 | BlockType 扩展 | 新增 BlockImageDescription / BlockAudioSegment | 与文本 Block 同构（F9）；chunker blockToText default 分支自动文本化，零改动；为未来多模态检索按类型召回留标记 |
| D8 | 能力缺失错误 | 新错误类型 ErrMediaCapabilityMissing{Capability}，预检阶段 400 返回 | 明确、可读、可被 handler 类型断言；与 F7 语义一致 |
| D9 | Store 迁移 | ALTER TABLE ... ADD COLUMN IF NOT EXISTS（沿用现有 schema.go 模式） | 幂等、与已有迁移风格一致 |
| D10 | 部署依赖 | Dockerfile 安装 ffmpeg | ffmpeg 为视频抽帧运行时依赖（系统包，非 Go 依赖） |

## 自检（spec 覆盖）

| Spec 需求 | Plan 归属 |
|-----------|-----------|
| F1 注册/双路匹配 | D1 注册表机制不变，新增 3 Parser 注册 |
| F2 格式清单 | parser_image/audio/video 的 SupportedExts/SupportedMIMEs |
| F3 图片结构化 Block | parser_image.go + BlockImageDescription |
| F4 音频时间戳分段 | parser_audio.go + SpeechSegment |
| F5 视频抽帧/降级 | parser_video.go + FrameExtractor + warning 透传（D6） |
| F6 配置与 Provider | D2 + multimedia 包 + config.MultimediaConfig |
| F7 缺失/失败语义 | D8 + ValidateReadable 兜底 |
| F8 检索复用 | 无检索改动，文本化内容自然命中 |
| F9 统一模型 | D7 Block 同构 |
| N1 性能 | D4 抽帧间隔 + 超时 |
| N2 兼容 | 现有 parser/registry 不动，回归测试 |
| N3 可扩展 | Provider/FrameExtractor 接口 |
| N4 可观测 | D6 warning + D8 可读错误 |
| N5 安全 | D5 ffmpeg 安全 + 现有大小限制 |
| N6 配置兼容 | D2 独立节，applyDefaults 不动现有键 |
