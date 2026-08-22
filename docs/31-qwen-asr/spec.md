# 阿里云百炼语音转写（qwen ASR）Provider Spec

## 背景

- 现有语音转写能力（speech）实现为 OpenAI 兼容的 whisper 风格：`POST /audio/transcriptions`（multipart 表单），依赖 `response_format=verbose_json` + `timestamp_granularities=segment` 返回带时间戳的分段。
- 阿里云百炼的 qwen ASR（如 `qwen3-asr-flash`）不提供 `/audio/transcriptions`，而是走 `/chat/completions`：音频以 `input_audio` + base64 data URL 传入消息，返回整段转写文本（`choices[0].message.content`），**无分段时间戳**。
- 已实测验证：qwen ASR 经 `/chat/completions` 可成功转写；但 `.m4a` 直接提交会返回 400（`The audio format is illegal`），转成 16kHz WAV 后转写成功。

## 目标

- 新增一个语音转写 Provider，接入阿里云百炼 qwen ASR（`chat/completions` + `input_audio`），使用户配置 qwen 后即可上传音频并转写入库。
- 与现有 whisper 风格实现并存，通过配置切换，互不影响。

## 功能需求

- F1：支持通过配置指定语音转写使用阿里云百炼 qwen ASR，与现有 whisper 风格实现可区分、可切换。
- F2：将音频转换为 qwen 可接受的格式（如 WAV）后再提交，覆盖系统已声明支持的音频格式（`.mp3/.wav/.m4a/.flac/.ogg/.aac`）；转码失败时返回可读错误。
- F3：调用 qwen ASR 接口（`chat/completions` + `input_audio` + base64），返回整段转写文本；无时间戳时按单段处理，与现有兜底语义一致。
- F4：未配置 qwen ASR（api_key 为空）时，沿用现有能力缺失语义，音频上传被明确拒绝（400）。
- F5：转写结果写入音频 Block，保持现有入库链路（`BlockAudioSegment`、`metadata.source` 等）不变。

## 非功能需求

- N1：复用项目现有 ffmpeg 依赖做转码，不引入新的外部二进制。
- N2：转码产生的临时文件落盘后及时清理，不残留。
- N3：不泄露 api_key；服务端返回的错误信息做截断展示，保持可读。

## 不做的事

- 不做时间戳分段（qwen ASR 接口不返回时间戳；该 provider 下视频音轨定位锚点缺失，接受现状）。
- 不改动视觉（vision）与视觉 embedding 的现有实现。
- 不改动前端（前端已能选择/上传音频，文件支持判断已就绪）。

## 验收标准

- AC1：配置 qwen ASR 后，上传 `.m4a` 音频能成功转写入库（内部转码生效）。
- AC2：未配置 qwen ASR（api_key 为空）时，音频上传返回 400 能力缺失。
- AC3：转码失败或服务调用失败时返回可读错误，不产生空文档/脏数据。
- AC4：转写结果为整段文本 Block（无时间戳），入库后可被检索。
- AC5：现有 whisper 风格 provider 行为不变（回归测试通过）。
