# 阿里云百炼语音转写（qwen ASR）Provider Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/multimedia/dashscope_speech.go` | dashscopeSpeechProvider + 请求/响应结构 |
| 新建 | `internal/multimedia/dashscope_speech_test.go` | 转码命令、请求体、响应解析、错误处理单测 |
| 修改 | `internal/multimedia/provider.go` | NewSpeechProvider 增加 dashscope 分发 |
| 修改 | `configs/config.yaml` | speech 注释补 dashscope 示例 |

## T1: 实现 dashscopeSpeechProvider

**文件：** `internal/multimedia/dashscope_speech.go`
**依赖：** 无

**步骤：**
1. 定义请求/响应结构（与 qwen ASR 文档对齐）：
   - `dashscopeASRRequest{Model, Messages, Stream, ASROptions}`、`dashscopeMessage{Role, Content}`、`dashscopeContent{Type, InputAudio}`、`dashscopeInputAudio{Data}`、`dashscopeASROptions{EnableITN}`、`dashscopeASRResponse{Choices[].Message.Content}`。
2. 定义 `dashscopeSpeechProvider{cfg, client, run commandRunner}`，构造器 `NewDashscopeSpeechProvider(cfg)`。
3. 实现 `Transcribe(ctx, audio, opts)`：
   - `os.CreateTemp` 落盘原始音频，`os.CreateTemp` 生成 `.wav` 输出路径。
   - `p.run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", in, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", out)`；失败返回 `errProvider("speech", ...)`。
   - 读 `out` → base64 → `data:audio/wav;base64,<b64>`。
   - 构造请求，`http.NewRequestWithContext` POST `endpoint(p.cfg.BaseURL, "/chat/completions")`，`setBearer` 设置鉴权头。
   - 非 200 → `errProvider("speech", fmt.Errorf("HTTP %d: %s", ...))`（用 `truncate` 截断响应体）。
   - 解析响应，取 `choices[0].message.content`；为空 → 返回错误。
   - 返回 `[]loader.SpeechSegment{{Text: content}}`。
   - `defer os.Remove` 清理两个临时文件。

**验证：** `go build ./internal/multimedia/...` 编译通过

## T2: NewSpeechProvider 分发

**文件：** `internal/multimedia/provider.go`
**依赖：** T1

**步骤：**
1. `NewSpeechProvider` 在 `Available()` 检查之后加 switch：
   - `case "dashscope": return NewDashscopeSpeechProvider(cfg)`
   - `default:` 保持现有 `openaiSpeechProvider`。

**验证：** `go build ./...` 编译通过

## T3: dashscopeSpeechProvider 单测

**文件：** `internal/multimedia/dashscope_speech_test.go`
**依赖：** T2

**步骤：** 用 mock `commandRunner` + `httptest.Server` 编写表驱动测试，覆盖：
1. 转码命令正确：断言 `run` 收到的 ffmpeg 参数含 `-ar 16000`、`-ac 1`、`-c:a pcm_s16le`，且输入/输出路径为临时文件。
2. 请求正确：server 记录请求路径为 `/compatible-mode/v1/chat/completions`（或 `/chat/completions`），请求体含 `"input_audio"` 与 `data:audio/wav;base64,`，model 正确。
3. 响应解析：server 返回 `{"choices":[{"message":{"content":"你好世界"}}]}`，断言返回单段 `Text=="你好世界"`、`StartMs==0 && EndMs==0`。
4. 服务端非 200：返回 500，断言错误含 `multimedia.speech`。
5. 转码失败：`run` 返回错误，断言错误含 `ffmpeg`。

**验证：** `go test ./internal/multimedia/... -run TestDashscope` 全部通过

## T4: 配置注释示例

**文件：** `configs/config.yaml`
**依赖：** 无（可与 T1-T3 并行）

**步骤：**
1. `multimedia.speech` 段注释补充：`provider` 支持 `openai_compat`（默认）/ `dashscope`；`dashscope` 时 `base_url` 填百炼 MaaS 端点 `https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1`，`model` 填 `qwen3-asr-flash`。

**验证：** 检查 yaml 结构不破坏（`go run ./cmd/server/main.go -c configs/config.local.yaml` 能正常启动加载配置，或 `go test ./internal/config/...` 通过）

## T5: 全量回归

**文件：** 无
**依赖：** T3、T4

**步骤：**
1. `go build ./...`
2. `go test ./...`
3. `gofmt -l internal/multimedia` 无输出

**验证：** 上述命令全部通过，现有 whisper 风格 provider 相关测试不回归（spec AC5）

## 执行顺序

```
T1 → T2 → T3 → T5
        ↘
T4（可并行）→ T5
```
