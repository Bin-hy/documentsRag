# 视频处理增强（拆流 + 双抽帧 + 时间戳定位）Checklist

> 每一项通过运行代码或观察行为来验证。

## 实现完整性
- [x] 配置 video/scene/vision_embedding 可加载、默认值正确（验证：config 测试断言 frame_strategy=fixed、sample_fps=2、threshold=0.85）
- [x] ffprobe 探测返回 MediaInfo（duration_ms/has_audio/audio_codec）（验证：单测 + 真实 ffprobe）
- [x] 音频流拆出（demux copy，无音轨不报错）（验证：参数构造单测 + 真实 ffmpeg）
- [x] 视觉 embedding 请求/响应正确（验证：httptest）
- [x] 场景检测两阶段正确切分场景、代表帧数 < 预抽帧数（验证：mock 向量断言）

## 集成
- [x] 视频 Parser 拆流后分别产出 Image Block（时间戳）与 Audio Block（原时间戳）（验证：parser 单测）
- [x] frame_strategy=scene 且无 vision_embedding → 明确配置错误（验证：CheckCapabilities 单测）
- [x] 多媒体 block 按块切 chunk，时间戳 1:1（验证：chunker 单测）
- [x] payload 含 source_type/start_ms/end_ms（验证：pipeline 测试）
- [x] 检索 Source 携带 StartMs/EndMs（验证：rag 测试）
- [x] chunk 详情返回 start_ms/end_ms/source_type（验证：api 测试）
- [x] 未配置 speech 时视频音轨降级 warning 透传（验证：parser/pipeline 测试）

## 编译与测试
- [x] go build ./... 编译无错误
- [x] go test ./... 全部通过（含回归）

## 端到端场景
- [x] 场景 1（fixed）：配置 vision+speech+frame_strategy=fixed → 视频入库 → Image Block 含 timestamp_ms/frame_index，Audio Block 含起止时间戳，chunk payload 含时间戳
- [x] 场景 2（scene）：frame_strategy=scene + vision_embedding → 视频入库 → 场景代表帧数显著小于预抽帧数，每帧带场景时间戳
- [x] 场景 3（配置错误）：frame_strategy=scene 且无 vision_embedding → 装配期/上传报配置错误
- [x] 场景 4（降级）：仅 vision 无 speech → 视频入库成功且任务 warning 含音轨跳过
- [x] 场景 5（视频访问）：Range 请求 → 206；不存在/越权 → 404；路径穿越无法绕过
- [x] 场景 6（回归）：图片/音频与 txt/md/pdf/docx/csv/excel/html 行为不变（全量测试）
