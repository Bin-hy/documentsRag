# 阿里云百炼语音转写（qwen ASR）Provider Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [x] `dashscopeSpeechProvider` 已实现且实现 `SpeechProvider` 接口（验证：`go build ./...`）
- [x] `NewSpeechProvider` 按 `provider: "dashscope"` 分发（验证：`go test ./internal/multimedia/...` 通过）
- [x] ffmpeg 转码为 16kHz 单声道 WAV（验证：单测断言 ffmpeg 参数 `-ar 16000 -ac 1 -c:a pcm_s16le`）

## 集成
- [x] 请求打到 `/chat/completions` 且消息含 `input_audio` + base64 data URL（验证：单测用 httptest 捕获请求）
- [x] 响应解析为单段 `SpeechSegment`（无时间戳），走现有入库链路（验证：单测 + `go test ./...`）
- [x] 音频与视频音轨转写共用该 Provider，无需改动调用方（验证：`go test ./...` 现有 audio/video 用例不回归）

## 编译与测试
- [x] 后端编译无错误（验证：`go build ./...`）
- [x] 后端单元测试全部通过（验证：`go test ./...`）
- [x] gofmt 检查通过（验证：`gofmt -l internal/multimedia` 无输出）

## 端到端场景
- [x] 场景 1（AC1/AC4）：配置 `dashscope` 后 `.m4a` 音频能转写——真实 qwen 接口已用用户音频 curl 实测 HTTP 200、转写正确（内部 ffmpeg 转码路径由单测覆盖；完整入库链路需 Postgres/Qdrant 运行环境，待用户本地跑服务确认）
- [x] 场景 2（AC2）：`multimedia.speech.api_key` 为空时音频上传返回 400 能力缺失（验证：`TestUploadMultimediaCapabilityMissing` 通过）
- [x] 场景 3（AC3）：转码失败或服务返回错误时返回可读错误、不产生脏数据（验证：单测覆盖 ffmpeg 失败与 HTTP 非 200 分支）
