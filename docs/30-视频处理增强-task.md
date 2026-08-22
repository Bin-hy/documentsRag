# 视频处理增强（拆流 + 双抽帧 + 时间戳定位）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | MultimediaConfig 增 video/scene/vision_embedding |
| 修改 | `internal/loader/capability.go` | 增 FrameStrategy/VisualEmbedder/MediaProber/AudioExtractor 接口 |
| 新建 | `internal/multimedia/probe.go` | ffprobe 媒体探测 |
| 新建 | `internal/multimedia/audio_extractor.go` | ffmpeg demux 拆音频流 |
| 新建 | `internal/multimedia/visual_embedder.go` | OpenAI Compatible 视觉 embedding |
| 新建 | `internal/multimedia/scene_sampler.go` | 两阶段场景检测 |
| 修改 | `internal/multimedia/frame_extractor.go` | 抽取 FixedFrameStrategy |
| 修改 | `internal/multimedia/provider.go` | 构造入口 |
| 重构 | `internal/loader/parser_video.go` | 拆流 + 双抽帧 + 时间戳 |
| 修改 | `internal/chunker/types.go` / `chunker.go` | ChunkMeta 扩展 + 按块切分 |
| 修改 | `internal/pipeline/pipeline.go` | payload 写时间戳 |
| 修改 | `internal/rag/context.go` | Source 扩展时间戳 |
| 修改 | `internal/api/handler_chunk.go` | GetChunk 返回时间戳 |
| 新建 | `internal/api/handler_video.go` | 视频 Range 流式接口 |
| 修改 | `internal/api/router.go` | 注册路由 |
| 修改 | `internal/app/app.go` | 装配 |
| 修改 | `configs/config.yaml` | video 配置示例 |
| 新建/改 | 各测试文件 | 随任务 |

## T1: 配置结构（video/scene/vision_embedding）

**文件：** `internal/config/config.go` + `config_test.go`
**依赖：** 无
**步骤：**
1. `MultimediaConfig` 增 `Video VideoConfig`；定义 `VideoConfig{FrameStrategy, FrameIntervalSec, Scene, VisionEmbedding}`、`SceneConfig{SampleFPS, SimilarityThreshold, MinSceneDurationMs}`
2. applyDefaults：FrameStrategy 默认 fixed；Video.FrameIntervalSec 默认取顶层 FrameIntervalSec；Scene.SampleFPS=2、SimilarityThreshold=0.85、MinSceneDurationMs=3000
3. Validate：frame_strategy 仅 fixed/scene 合法；scene 时 vision_embedding.api_key 必填
**验证：** `go test ./internal/config/...`

## T2: 能力接口扩展

**文件：** `internal/loader/capability.go` + `types.go`
**依赖：** 无
**步骤：**
1. 定义 FrameStrategy / FrameStrategyConfig / VisualEmbedder / MediaProber / AudioExtractor 接口
2. 定义 MediaInfo / VideoStreams
3. VideoFrame 复用
**验证：** `go build ./internal/loader/...` + 既有 loader 测试通过

## T3: 媒体探测 + 拆音频流

**文件：** `internal/multimedia/probe.go`、`audio_extractor.go`（+ 测试）
**依赖：** T2
**步骤：**
1. probe.go：ffprobe 解析为 MediaInfo（HasAudio = 存在 audio 流）
2. audio_extractor.go：ffmpeg demux copy 拆音频流；无音轨返回空不报错
3. 单测：JSON 解析 + 参数构造（注入 runner）+ 真实集成
**验证：** `go test ./internal/multimedia/... -run 'TestProbe|TestAudioExtract'`

## T4: 视觉 embedding + 场景检测

**文件：** `internal/multimedia/visual_embedder.go`、`scene_sampler.go`（+ 测试）
**依赖：** T2
**步骤：**
1. visual_embedder.go：POST /v1/embeddings，base64 data URI，返回向量
2. scene_sampler.go：预抽帧 → EmbedImage → 相邻余弦相似度 < threshold 切场景 → 每场景取中间帧
3. 单测：httptest + mock 断言场景切分
**验证：** `go test ./internal/multimedia/... -run 'TestVisualEmbed|TestSceneSampler'`

## T5: FixedFrameStrategy 抽取

**文件：** `internal/multimedia/frame_extractor.go` + `provider.go`
**依赖：** T2
**步骤：**
1. 现有 ExtractFrames 逻辑封装为 FixedFrameStrategy
2. provider.go 增 NewFrameStrategy / NewVisualEmbedder / NewMediaProber / NewAudioExtractor
**验证：** `go test ./internal/multimedia/... -run TestExtract`

## T6: 视频 Parser 重构

**文件：** `internal/loader/parser_video.go` + `parser_video_test.go`
**依赖：** T2-T5
**步骤：**
1. 构造改 NewVideoParser(v, s, strategy, prober, extractor, cfg)
2. Parse：probe → 拆流 → SampleFrames → 逐帧 VLM → 音频流 ASR → Extra 记录媒体信息
3. CheckCapabilities：vision 缺失报缺；scene 无 vision_embedding 报配置错误
4. 测试：mock 断言拆流调用、时间戳、降级
**验证：** `go test ./internal/loader/...`

## T7: chunker 时间戳贯通

**文件：** `internal/chunker/types.go`、`chunker.go`（+ 测试）
**依赖：** 无
**步骤：**
1. ChunkMeta 增 SourceType/StartMs/EndMs
2. Chunk：带时间戳的 block 按块切分，每 block 一个 chunk，时间戳取自 metadata
3. 测试：多媒体时间戳 1:1；文本文档不受影响
**验证：** `go test ./internal/chunker/...`

## T8: 检索链路贯通（pipeline + rag + chunk 详情）

**文件：** `internal/pipeline/pipeline.go`、`internal/rag/context.go`、`internal/api/handler_chunk.go`（+ 测试）
**依赖：** T7
**步骤：**
1. pipeline：payload 增 source_type/start_ms/end_ms
2. rag：Source 增 StartMs/EndMs；buildContext 读 start_ms/end_ms
3. handler_chunk：GetChunk 返回 start_ms/end_ms/source_type
4. 测试：payload / Source / chunk 详情时间戳
**验证：** `go test ./internal/pipeline/... ./internal/rag/... ./internal/api/...`

## T9: 视频流式访问接口

**文件：** `internal/api/handler_video.go`、`internal/api/router.go`（+ 测试）
**依赖：** 无
**步骤：**
1. GET /api/v1/videos/{id}/stream：id=document_id → 查库 → 校验 format 视频 + ensureKBAccess → http.ServeContent
2. router 注册路由
3. 测试：Range 206、不存在/越权 404、文本格式拒绝
**验证：** `go test ./internal/api/...`

## T10: 装配 + 配置模板

**文件：** `internal/app/app.go`、`configs/config.yaml`
**依赖：** T1-T9
**步骤：**
1. app.go 装配各能力构造 video parser 注册
2. configs/config.yaml 增 video 配置示例
**验证：** `go build ./...` + 全量 `go test ./...`

## 执行顺序

```
T1 → T2 → T3 ─┬→ T6 → T7 → T8 ─┐
              ├→ T4 ────────────┤
              └→ T5 ────────────┤→ T10
T9（可并行）
```
