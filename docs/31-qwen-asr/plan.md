# 阿里云百炼语音转写（qwen ASR）Provider Plan

## 架构概览

复用现有语音转写能力抽象，新增一个 Provider 实现，不改接口、不改调用链：

- **loader 层**：`SpeechProvider` 接口（`Transcribe(ctx, audio []byte, opts) ([]SpeechSegment, error)`）保持不变；`audioParser` 与视频音轨转写继续调用该接口，自动复用新 Provider。
- **multimedia 层**：新增 `dashscopeSpeechProvider`，实现 `chat/completions` + `input_audio`（base64 data URL）调用 qwen ASR；`NewSpeechProvider` 按配置的 `provider` 字段分发。
- **config 层**：结构不变，`multimedia.speech.provider` 新增取值 `dashscope`（现有 `openai_compat` 保持默认）。

能力缺失语义沿用现状：`NewSpeechProvider` 在 `api_key` 为空时返回 nil，`audioParser.CheckCapabilities()` 返回 `speech` 缺失，上传预检 400（spec F4）。

## 核心数据结构

### dashscopeSpeechProvider（internal/multimedia/dashscope_speech.go）

```go
type dashscopeSpeechProvider struct {
    cfg    config.MultimediaServiceConfig
    client *http.Client
    run    commandRunner // 复用 frame_extractor.go 的 commandRunner，便于单测注入
}
```

实现 `loader.SpeechProvider.Transcribe`。

### 请求/响应结构（对齐 qwen ASR 文档）

```go
type dashscopeASRRequest struct {
    Model      string                `json:"model"`
    Messages   []dashscopeMessage    `json:"messages"`
    Stream     bool                  `json:"stream"`
    ASROptions *dashscopeASROptions  `json:"asr_options,omitempty"`
}
type dashscopeMessage struct {
    Role    string               `json:"role"`
    Content []dashscopeContent   `json:"content"`
}
type dashscopeContent struct {
    Type       string                `json:"type"`                 // "input_audio"
    InputAudio *dashscopeInputAudio  `json:"input_audio,omitempty"`
}
type dashscopeInputAudio struct {
    Data string `json:"data"` // "data:audio/wav;base64,<...>"
}
type dashscopeASROptions struct {
    EnableITN bool `json:"enable_itn"`
}

type dashscopeASRResponse struct {
    Choices []struct {
        Message struct {
            Content string `json:"content"`
        } `json:"message"`
    } `json:"choices"`
}
```

## 模块设计

### 模块 A：dashscopeSpeechProvider（internal/multimedia/dashscope_speech.go，新建）

**职责：** 音频字节 → 转码 → qwen ASR → 单段转写文本。

**对外接口：**

```go
func NewDashscopeSpeechProvider(cfg config.MultimediaServiceConfig) loader.SpeechProvider
func (p *dashscopeSpeechProvider) Transcribe(ctx context.Context, audio []byte, opts loader.SpeechOptions) ([]loader.SpeechSegment, error)
```

**Transcribe 流程：**
1. 将 `audio` 写入临时文件（`os.CreateTemp`，供 ffmpeg 读取，ffmpeg 自动探测输入格式）。
2. 调用 `p.run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", inPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", outWav)` 转码为 16kHz 单声道 WAV；失败返回可读错误。
3. 读 `outWav` → base64 → `data:audio/wav;base64,<b64>`。
4. 构造 `dashscopeASRRequest`（`Stream:false`、`ASROptions.EnableITN:false`），POST `endpoint(cfg.BaseURL, "/chat/completions")`，带 `Authorization: Bearer`。
5. 非 200 → 包装 `multimedia.speech 服务调用失败`（截断响应体）；解析 `choices[0].message.content`，空 → 错误。
6. 返回 `[]loader.SpeechSegment{{Text: content}}`（无时间戳，与现有兜底一致）。
7. `defer` 清理两个临时文件。

**依赖：** `loader`（SpeechProvider/SpeechSegment/SpeechOptions）、`config`、`provider.go` 的 `endpoint`/`newHTTPClient`/`errProvider`/`truncate`、`frame_extractor.go` 的 `commandRunner`。

### 模块 B：构造分发（internal/multimedia/provider.go，修改）

**职责：** 按 `provider` 字段选择实现。

**改动：**

```go
func NewSpeechProvider(cfg config.MultimediaServiceConfig) loader.SpeechProvider {
    if !cfg.Available() { return nil }
    switch cfg.Provider {
    case "dashscope":
        return NewDashscopeSpeechProvider(cfg)
    default: // openai_compat（默认）
        return &openaiSpeechProvider{cfg: cfg, client: newHTTPClient(cfg.Timeout)}
    }
}
```

### 模块 C：配置示例（configs/config.yaml，修改）

**职责：** 补充 `multimedia.speech` 的 `dashscope` 取值与 base_url 示例注释（不改结构、不改默认值）。

## 模块交互

```
audioParser.Parse(reader)
  └─ speech.Transcribe(ctx, audioBytes, opts)
       └─ dashscopeSpeechProvider.Transcribe
            ├─ ffmpeg 转码 (16kHz WAV)
            ├─ base64 → data URL
            ├─ POST /compatible-mode/v1/chat/completions  (input_audio)
            └─ 解析 choices[0].message.content → 单段 SpeechSegment
```

视频音轨转写（`videoParser`）同样走 `speech.Transcribe`，无需额外改动即自动支持 dashscope。

## 文件组织

```
internal/multimedia/
├── dashscope_speech.go       — 新建：dashscopeSpeechProvider + 请求/响应结构
├── dashscope_speech_test.go  — 新建：转码命令、请求体、响应解析、错误处理单测
└── provider.go               — 修改：NewSpeechProvider 增加 dashscope 分发

configs/
└── config.yaml               — 修改：speech 注释补 dashscope 示例（文档性）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| Provider 分发 | `multimedia.speech.provider` 新增取值 `dashscope`，`NewSpeechProvider` switch 分发 | 复用现有配置结构与 `Available()`；可插拔，与 spec G4 一致 |
| 接口复用 | 不新增接口，实现现有 `SpeechProvider` | 音频/视频音轨转写链路自动复用，改动面最小 |
| 转码 | 内部 ffmpeg 转 16kHz 单声道 WAV（pcm_s16le） | 实测 `.m4a` 直接传被拒，WAV 兼容最好；覆盖全部已声明音频格式 |
| 转码实现 | 落盘临时文件 + ffmpeg，`defer` 清理 | ffmpeg 需文件路径；复用 `commandRunner` 便于单测 |
| 请求格式 | `chat/completions` + `input_audio` + `data:audio/wav;base64` | 与 qwen ASR 文档一致，已实测 HTTP 200 |
| 响应处理 | `choices[0].message.content` 作为单段 `SpeechSegment`（无时间戳） | qwen 不返回时间戳，与现有 whisper 兜底语义一致 |
| 错误处理 | 复用 `errProvider`/`truncate` | 与现有 provider 错误语义一致，可读且不泄露 key |
| 音频长度 | 不额外限制 | 沿用上传大小限制，避免过度设计 |
