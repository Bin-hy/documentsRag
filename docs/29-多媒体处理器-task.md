# 多媒体处理器（图片/音频/视频）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/loader/capability.go` | 能力接口 + 数据结构 |
| 修改 | `internal/loader/types.go` | 新增 2 个 BlockType |
| 修改 | `internal/loader/errors.go` | 新增 ErrMediaCapabilityMissing |
| 新建 | `internal/loader/parser_image.go` | 图片 Parser |
| 新建 | `internal/loader/parser_audio.go` | 音频 Parser |
| 新建 | `internal/loader/parser_video.go` | 视频 Parser |
| 修改 | `internal/loader/loader.go` | NewDefaultRegistry 支持多媒体 |
| 新建 | `internal/multimedia/provider.go` | Provider 构造 |
| 新建 | `internal/multimedia/openai_vision.go` | OpenAI Compatible 视觉实现 |
| 新建 | `internal/multimedia/openai_speech.go` | OpenAI Compatible 语音转写 |
| 新建 | `internal/multimedia/frame_extractor.go` | ffmpeg 抽帧 |
| 修改 | `internal/config/config.go` | MultimediaConfig |
| 修改 | `internal/pipeline/pipeline.go` | Ingest 透传 warnings |
| 修改 | `internal/task/worker.go` | warning 写任务 |
| 修改 | `internal/store/store.go` | Task.WarningMessage |
| 修改 | `internal/store/schema.go` | tasks 表加列 |
| 修改 | `internal/app/app.go` | 装配 providers |
| 修改 | `internal/api/handler_doc.go` | precheck 能力检查 |
| 修改 | `Dockerfile` / `Dockerfile.deploy` | 安装 ffmpeg |
| 新建 | 各测试文件 | 随任务 |

## T1: 配置结构

**文件：** `internal/config/config.go`
**依赖：** 无
**步骤：**
1. Config 增加 `Multimedia MultimediaConfig` 字段（yaml: multimedia）
2. 定义 MultimediaConfig{ Vision, Speech MultimediaServiceConfig; FrameIntervalSec int } 与 MultimediaServiceConfig{ Provider/BaseURL/APIKey/Model/Timeout } 及 Available()
3. applyDefaults：FrameIntervalSec 默认 10、Timeout 默认 30、Provider 默认 openai_compat
4. Validate：BaseURL 非空时须含 "://"（与其他服务一致）
**验证：** `go test ./internal/config/...` 通过（含新增默认值断言）

## T2: 能力接口与类型

**文件：** `internal/loader/capability.go`（新建）、`internal/loader/types.go`、`internal/loader/errors.go`
**依赖：** 无
**步骤：**
1. types.go 新增 BlockType：BlockImageDescription、BlockAudioSegment
2. capability.go 定义 VisionProvider/SpeechProvider/FrameExtractor 接口、VisionOptions/SpeechOptions/SpeechSegment/VideoFrame、MediaCapabilityChecker
3. errors.go 新增 ErrMediaCapabilityMissing{Capability}（Error 文本含配置指引）
**验证：** `go build ./internal/loader/...` 通过 + `go test ./internal/loader/...` 通过

## T3: multimedia 包 Provider 实现

**文件：** `internal/multimedia/provider.go`、`openai_vision.go`、`openai_speech.go`（新建）
**依赖：** T1、T2
**步骤：**
1. provider.go：NewVisionProvider/NewSpeechProvider 构造（未配置 APIKey 时返回 nil，配合 Available 判定）
2. openai_vision.go：POST /v1/chat/completions，messages 含 image_url（base64 data URI）+ 固定系统提示；超时/错误映射为可读错误
3. openai_speech.go：POST /v1/audio/transcriptions，multipart（file+model+timestamp_granularities[]=segment），解析分段返回 []SpeechSegment
**验证：** 用 httptest 模拟服务端，单测断言请求体与响应解析正确

## T4: ffmpeg 抽帧器

**文件：** `internal/multimedia/frame_extractor.go`（新建）
**依赖：** T2
**步骤：**
1. 实现 FrameExtractor：exec.Command("ffmpeg", "-i", path, "-vf", "fps=1/N", "-vsync", "vfr", outDir/...)
2. 输出 JPEG 到 os.MkdirTemp 目录，按文件名时间点排序读回 []VideoFrame（TimeMs 从文件名解析）
3. 抽帧间隔 ≤0 时用默认 10
**验证：** 单测断言命令参数构造（注入 command runner）；有 ffmpeg 环境时跑真实小视频集成

## T5: 图片 Parser

**文件：** `internal/loader/parser_image.go` + `parser_image_test.go`（新建）
**依赖：** T2（接口）
**步骤：**
1. NewImageParser(v VisionProvider) Parser；SupportedExts/MIMEs 按 F2 图片清单
2. Parse：io.ReadAll → image.DecodeConfig 得宽高 → v.Describe → 单个 BlockImageDescription，Metadata{type,width,height,source}
3. 实现 MediaCapabilityChecker：v == nil → ErrMediaCapabilityMissing{vision}
4. 注册进 NewDefaultRegistry（loader.go）
**验证：** mock provider 单测：描述文本进 Content、宽高/来源进 Metadata；nil provider 时 CheckCapabilities 报 vision 缺失

## T6: 音频 Parser

**文件：** `internal/loader/parser_audio.go` + `parser_audio_test.go`（新建）
**依赖：** T2
**步骤：**
1. NewAudioParser(s SpeechProvider) Parser；格式按 F2 音频清单
2. Parse：读入 → s.Transcribe → 每段一个 BlockAudioSegment，Metadata{start_ms,end_ms}；DocumentMeta.Extra 记 duration_ms、source
3. 实现 MediaCapabilityChecker：s == nil → ErrMediaCapabilityMissing{speech}
4. 注册进 NewDefaultRegistry
**验证：** mock provider 单测：分段与时间戳正确、总时长=末段 EndMs；nil provider 报 speech 缺失

## T7: 视频 Parser

**文件：** `internal/loader/parser_video.go` + `parser_video_test.go`（新建）
**依赖：** T2、T4（FrameExtractor）
**步骤：**
1. NewVideoParser(v VisionProvider, s SpeechProvider, fx FrameExtractor, interval int) Parser；格式按 F2 视频清单
2. Parse：reader 写临时文件 → fx.ExtractFrames → 逐帧 v.Describe → BlockImageDescription（Metadata{time_ms,width,height,source}）
3. s 可用时追加音轨转写 Blocks；否则 warnings 追加「未配置 speech，跳过音轨转写」
4. defer 清理临时文件/目录
5. 实现 MediaCapabilityChecker：v == nil → ErrMediaCapabilityMissing{vision}
6. 注册进 NewDefaultRegistry
**验证：** mock extractor+vision 单测：帧数=抽帧数、时间戳在 metadata、降级 warning 非空；nil vision 报缺失

## T8: warning 透传（pipeline + worker + store + schema）

**文件：** `internal/pipeline/pipeline.go`、`internal/task/worker.go`、`internal/store/store.go`、`internal/store/schema.go`
**依赖：** T5/T6/T7（Warnings 来源）
**步骤：**
1. store.go：Task 增加 WarningMessage string
2. schema.go：`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS warning_message TEXT NOT NULL DEFAULT ''`
3. pipeline.go：Ingest 返回 ([]string, []string, error)；透传 result.Warnings；更新测试调用方
4. worker.go：warnings 非空 → t.WarningMessage = strings.Join(warnings, "; ")
**验证：** pipeline 单测断言 warnings 透传；`go build ./...` 编译通过（含 schema_test mock 更新）

## T9: 装配与上传校验

**文件：** `internal/app/app.go`、`internal/api/handler_doc.go`、`internal/loader/loader.go`
**依赖：** T1-T8
**步骤：**
1. loader.go：NewDefaultRegistry(media ...MultimediaConfig) 兼容无参；有配置时注册 3 个多媒体 Parser（provider 由 multimedia 包构造）
2. app.go：装配 cfg.Multimedia → providers → 两处 loader 创建点传入
3. handler_doc.go precheckReadable：parser 实现 MediaCapabilityChecker 时先 CheckCapabilities（缺失→400），跳过真实解析
**验证：** api 测试：未配置能力 → 400 配置缺失；配置后上传成功；错误 MIME 仍拒绝；全量 `go test ./...` 回归

## T10: 部署与文档

**文件：** `Dockerfile`、`Dockerfile.deploy`、`docs/29-多媒体处理器-task.md`、`docs/29-多媒体处理器-checklist.md`
**依赖：** 无（可并行）
**步骤：**
1. Dockerfile 增加 ffmpeg 安装（apt-get install -y ffmpeg，deploy 镜像同理）
2. 归档 task.md；生成 checklist.md（阶段四）
**验证：** 文档齐全；docker build 语法检查

## 执行顺序

```
T1 → T2 → T3 ─┬→ T5 ─┐
              ├→ T6 ─┤→ T8 → T9
T4 ───────────┴→ T7 ─┘
T10（独立并行）
```
