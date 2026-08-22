# 多媒体处理器（图片/音频/视频）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [x] 图片 Parser 已实现且注册进默认注册表（验证：`go test ./internal/loader/...` 通过；图片上传可解析）
- [x] 音频 Parser 已实现且注册（验证：同包测试通过）
- [x] 视频 Parser 已实现且注册（验证：同包测试通过；mock 抽帧+vision 路径）
- [x] `multimedia` 配置节可加载、默认值正确（验证：config 测试断言 FrameIntervalSec=10、Timeout=30）
- [x] Vision/Speech Provider 的 OpenAI Compatible 实现请求/响应正确（验证：httptest 单测）
- [x] ffmpeg 抽帧器命令构造正确（验证：frame_extractor 单测断言 argv）

## 集成
- [x] 多媒体 Parser 输出 Document{Blocks, Metadata} 与文本 loader 同构，走同一 pipeline（验证：pipeline 测试用多媒体 Document 走完 Ingest）
- [x] `pipeline.Ingest` 透传 `LoadResult.Warnings`（验证：pipeline 测试断言 warnings 返回）
- [x] worker 将 warnings 写入 `task.warning_message`（验证：worker 测试或集成断言）
- [x] 上传预检对多媒体只做能力检查、不真实解析（验证：handler 测试中 mock parser 的 Parse 未被调用）
- [x] 未配置能力时上传返回 400 且错误信息含「multimedia.vision/speech 未配置」（验证：handler 测试）
- [x] 错误扩展名 / MIME 依旧被拒（验证：handler 测试）

## 编译与测试
- [x] `go build ./...` 编译无错误
- [x] `go test ./...` 全部通过（含新增测试与既有回归）
- [x] config 校验通过（BaseURL 缺 "://" 报错）

## 端到端场景
- [x] 场景 1（图片）：配置 `multimedia.vision`（mock/真实）→ 上传 png → 任务 completed → 文档含 image_description Block、描述文本可检索命中、引用来源为原文件名
- [x] 场景 2（音频）：配置 `multimedia.speech` → 上传 mp3 → 入库成功 → 转写分段含起止时间戳 metadata、Extra 含总时长、检索命中
- [x] 场景 3（视频）：配置 vision + speech → 上传 mp4 → 按配置间隔产出关键帧描述、metadata 含关键帧时间点列表、音轨转写并入
- [x] 场景 4（视频降级）：仅配置 vision → 上传 mp4 → 入库成功且任务 warning_message 含音轨跳过提示
- [x] 场景 5（未配置）：无任何多媒体配置 → 上传 png / mp3 → 均返回 400 配置缺失，库中无脏数据记录
- [x] 场景 6（回归）：上传 md / txt / pdf / docx / csv / excel / html → 行为与改造前一致（全量测试 + 冒烟）
