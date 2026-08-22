# 视频处理增强（拆流 + 双抽帧 + 时间戳定位）Plan

## 架构概览

```
视频入库
  │ ffprobe 读媒体信息（时长/编码/是否含音轨）
  ▼
拆流（ffmpeg demux，copy 不重编码）
  ├─ 视频流 ──▶ 抽帧策略（全局配置 fixed | scene）
  │             ├─ fixed：固定间隔抽帧
  │             └─ scene：低频预抽帧 → VisualEmbedder 帧向量 → 相邻相似度 → 场景代表帧
  │             └─▶ 逐帧 VLM（vision）→ Image Block（timestamp_ms + frame_index）
  └─ 音频流 ──▶ 独立 ASR（speech）→ Audio Block（start_ms/end_ms，保留原时间戳）
  ▼
Document{Blocks(带时间戳 metadata)}
  ▼
chunker：多媒体 block 按「块」切 chunk，ChunkMeta 携带 start_ms/end_ms/source_type
  ▼
pipeline：payload 写入 start_ms/end_ms/source_type/video_id
  ▼
检索：Source 携带时间戳 + chunk 详情 API 返回时间戳（视频「页码」定位锚点）
```

- loader 包：视频 parser 重构为「拆流 + 双抽帧」，依赖抽象 FrameStrategy（fixed/scene）与 AudioExtractor。
- multimedia 包：新增 VisualEmbedder 接口 + OpenAI Compatible 实现、SceneSampler（两阶段场景检测）、AudioExtractor（ffmpeg demux 拆音频流）、MediaProber（ffprobe）。
- config 包：multimedia 新增 video 子节（frame_strategy / scene / vision_embedding），frame_interval_sec 保留兼容。
- chunker 包：ChunkMeta 扩展时间戳字段；多媒体 Document 按块切分（时间戳 1:1）。
- pipeline / rag / api 包：payload → Source → chunk 详情贯通时间戳；新增视频 Range 流式接口。

## 核心数据结构

```go
// loader（parser_video.go 重构）——视频流/音频流拆分产物
type VideoStreams struct {
    VideoPath string      // 抽帧用临时视频（可为原文件路径）
    AudioPath string      // 拆出的音频流临时文件（无音轨为空）
    Media     MediaInfo
}
type MediaInfo struct {
    DurationMs int64
    VideoCodec string
    AudioCodec string
    HasAudio   bool
    Width      int
    Height     int
}

// loader 抽帧策略抽象（multimedia 实现）
type FrameStrategy interface {
    SampleFrames(ctx context.Context, videoPath string, cfg FrameStrategyConfig) ([]VideoFrame, error)
}
type FrameStrategyConfig struct {
    Mode          string  // "fixed" | "scene"
    IntervalSec   int
    SampleFPS     int     // scene 预抽帧率
    SimThreshold  float64 // 场景切换相似度阈值
    MinSceneDurMs int64   // 最小场景时长
}

// multimedia 视觉 embedding（场景检测用）
type VisualEmbedder interface {
    EmbedImage(ctx context.Context, image []byte) ([]float32, error)
}

// chunker（ChunkMeta 扩展时间戳）
type ChunkMeta struct {
    DocFilename    string
    HeadingContext string
    TokenCount     int
    SourceType     string // "video" | "audio" | "image" | ""
    StartMs        int64
    EndMs          int64
}

// rag.Source 扩展
type Source struct {
    ID         string
    Filename   string
    Heading    string
    Score      float32
    SourceType string
    StartMs    int64
    EndMs      int64
}
```

```go
// config（multimedia 扩展）
type MultimediaConfig struct {
    Vision           MultimediaServiceConfig `yaml:"vision"`
    Speech           MultimediaServiceConfig `yaml:"speech"`
    FrameIntervalSec int                     `yaml:"frame_interval_sec"` // 兼容保留
    Video            VideoConfig             `yaml:"video"`
}
type VideoConfig struct {
    FrameStrategy    string                  `yaml:"frame_strategy"`     // fixed | scene
    FrameIntervalSec int                     `yaml:"frame_interval_sec"` // 优先于顶层
    Scene            SceneConfig             `yaml:"scene"`
    VisionEmbedding  MultimediaServiceConfig `yaml:"vision_embedding"`
}
type SceneConfig struct {
    SampleFPS           int     `yaml:"sample_fps"`
    SimilarityThreshold float64 `yaml:"similarity_threshold"`
    MinSceneDurationMs  int     `yaml:"min_scene_duration_ms"`
}
```

## 模块设计

### 模块 A：媒体探测与拆流（multimedia：probe.go / audio_extractor.go）
- MediaProber：ffprobe 读时长/视频编码/音频编码/宽高/是否含音轨 → MediaInfo。
- AudioExtractor：ffmpeg -i input -map 0:a:0? -vn -acodec copy out.m4a（demux copy）；损坏音轨/无音轨返回空 + 说明，由 parser 记 warning。

### 模块 B：抽帧策略（loader 定义接口，multimedia 实现）
- FixedFrameStrategy：复用现有 ffmpeg fps=1/N 抽帧，返回 VideoFrame{TimeMs, Data}。
- SceneSampler（两阶段）：先 fps=sample_fps 预抽帧 → VisualEmbedder.EmbedImage 逐帧向量（批处理）→ 相邻帧余弦相似度 < threshold 判定场景切换（受 min_scene_duration_ms 约束）→ 取每场景中间帧为代表帧。禁止逐帧 VLM。

### 模块 C：视频 Parser 重构（loader/parser_video.go）
- 注入 FrameStrategy、VisionProvider、SpeechProvider、MediaProber、AudioExtractor。
- 流程：probe → 拆流 → 抽帧策略出代表帧 → 逐帧 VLM 得 Image Block（metadata：timestamp_ms、frame_index、start_ms/end_ms）→ 音频流送 ASR 得 Audio Block（保留原 start/end）→ 组装 Document.Extra。
- 能力检查：vision 缺失 → 报缺；frame_strategy=scene 且 vision_embedding 未配置 → 报配置错误；speech 缺失 → 跳过音轨 + warning。

### 模块 D：chunker 时间戳贯通（chunker）
- ChunkMeta 扩展 SourceType/StartMs/EndMs。
- 多媒体 Document（block 带时间戳）走「按块切分」：每个 Image/Audio Block 独立成 chunk；文本 Document 保持现状。

### 模块 E：检索链路贯通（pipeline / rag / api）
- pipeline：payload 增写 start_ms/end_ms/source_type。
- rag：Source 增 StartMs/EndMs；buildContext 从 metadata 读 start_ms/end_ms。
- api：GetChunk 返回 start_ms/end_ms/source_type。

### 模块 F：视频流式访问（api）
- GET /api/v1/videos/{id}/stream：id=document_id → 查库 → 校验 format 为视频 + KB 访问权 → http.ServeContent（原生 Range/206）。
- 路径只来自数据库 doc.FilePath。

## 模块交互

```
worker.process → pipeline.Ingest → loader.Load
  └─ videoParser.Parse:
       probe() → MediaInfo
       AudioExtractor.Extract() → audio.m4a（临时）
       FrameStrategy.SampleFrames(video) → []VideoFrame
         (scene: VisualEmbedder.EmbedImage 批处理 + 余弦相似度)
       vision.Describe(每帧) → Image Blocks
       speech.Transcribe(audio) → Audio Blocks（缺 speech → warning）
       cleanup 临时文件
  → ValidateReadable → chunker.Chunk（按块切分，带时间戳）
  → embedder.Embed → vectorstore.Upsert（payload 带 start_ms/end_ms/source_type）
  → 检索: buildContext 从 payload 读时间戳 → Source{StartMs,EndMs}
  → GetChunk 从 payload 返回时间戳
视频播放（后续前端）：GET /api/v1/videos/{document_id}/stream（Range → 206）
```

## 文件组织

```
internal/multimedia/
├── probe.go          — 新建：ffprobe 媒体探测 → MediaInfo
├── audio_extractor.go— 新建：ffmpeg demux 拆音频流（copy）
├── scene_sampler.go  — 新建：两阶段场景检测（VisualEmbedder + 余弦相似度）
├── visual_embedder.go— 新建：OpenAI Compatible 视觉 embedding 实现
├── frame_extractor.go— 修改：抽取 FixedFrameStrategy（复用现有 ffmpeg 抽帧）
└── provider.go       — 修改：NewVisualEmbedder/NewFrameStrategy 构造

internal/loader/
├── capability.go     — 修改：新增 FrameStrategy / VisualEmbedder / MediaProber / AudioExtractor 接口
├── parser_video.go   — 重构：拆流 + 双抽帧 + 时间戳 metadata
└── parser_video_test.go — 重构测试

internal/chunker/
├── types.go          — 修改：ChunkMeta 扩展 SourceType/StartMs/EndMs
└── chunker.go        — 修改：多媒体按块切分（时间戳 1:1）

internal/config/config.go — 修改：MultimediaConfig 增加 VideoConfig/SceneConfig/vision_embedding
internal/pipeline/pipeline.go — 修改：payload 写 start_ms/end_ms/source_type
internal/rag/context.go — 修改：Source 扩展 StartMs/EndMs + 读取
internal/api/handler_chunk.go — 修改：GetChunk 返回时间戳
internal/api/handler_video.go — 新建：视频 Range 流式接口
internal/api/router.go — 修改：注册 /videos/{id}/stream 路由
internal/app/app.go — 修改：装配 FrameStrategy/VisualEmbedder/AudioExtractor
configs/config.yaml — 修改：video 配置示例
docs/30-视频处理增强-plan.md — 本文件
```

## 技术决策

| # | 决策点 | 选择 | 理由 |
|---|--------|------|------|
| D1 | 拆流方式 | ffmpeg -map 0:a:0? -vn -acodec copy（demux） | 不重编码（N2），无音轨不报错，默认主音轨 |
| D2 | 媒体信息 | ffprobe -show_entries JSON | 结构化成 MediaInfo |
| D3 | 抽帧策略抽象 | 接口放 loader、实现放 multimedia | 依赖倒置，可插拔 |
| D4 | 场景检测 | 两阶段：预抽帧 → VisualEmbedder 余弦相似度 → 场景代表帧 | 避免逐帧 embedding/VLM（N1） |
| D5 | 时间戳贯通 | ChunkMeta 扩展 + 多媒体按块切分 + payload 增字段 | 每 chunk = 一个定位点，时间戳 1:1 |
| D6 | 帧 end_ms | end_ms = timestamp_ms + 抽帧间隔（fixed）/ 下一场景边界（scene） | 帧单点，需区间供前端定位 |
| D7 | 视频访问 | http.ServeContent + video_id→db→FilePath | 原生 Range/206/seek；防穿越（N4） |
| D8 | 视觉 embedding | 独立 multimedia.video.vision_embedding（OpenAI Compatible） | 视觉/文本 embedding 模型不同 |
| D9 | 配置兼容 | 保留顶层 frame_interval_sec，video.frame_interval_sec 优先 | 不破坏上一迭代配置 |
| D10 | scene 缺配置 | scene 且 vision_embedding 空 → 装配期报配置错误 | 明确失败（N5/AC4） |
