# 视频处理增强（拆流 + 双抽帧 + 时间戳定位）Spec

## 背景

上一迭代已实现图片/音频/视频三类 Processor（统一转文本进 RAG），视频采用「ffmpeg 固定间隔抽帧 → 逐帧 VLM → 音频轨转写」。
现有视频处理有三点不足：
1. 未真正拆流——音频转写直接把整个视频文件交给 ASR，而非先拆出音频流；
2. 仅固定抽帧——无场景检测能力，长视频逐固定间隔抽帧会产生大量冗余帧与 VLM 调用；
3. 时间戳未贯通——时间戳只存在 Block.Metadata，未进入 chunk payload、检索结果 Source 与 chunk 详情 API，无法作为「视频页码」定位锚点。

参考开源视频 RAG 方案的标准做法：拆流 → 视频流抽帧（固定/场景检测）+ VLM、音频流 ASR，产出带时间戳的 Image Block 与 Audio Block，时间戳作为视频定位锚点。
现状基础：internal/multimedia 已有 ffmpeg 抽帧器、OpenAI Compatible vision/speech；internal/embedding 已有 embedding 能力（可复用于场景检测）；检索 payload 与 rag.Source、chunk 详情 API 目前均无时间戳字段。

## 目标

- G1 拆流处理：用 ffmpeg 将视频拆分为视频流与音频流，视频流用于抽帧 + VLM 得 Image Block，音频流独立送 ASR 得 Audio Block，二者均携带时间戳 metadata。
- G2 双抽帧策略：支持固定抽帧与场景检测两种，全局配置选择；场景检测用独立视觉 embedding 计算帧间语义相似度识别场景切换，保留代表帧，避免冗余帧与过量 VLM 调用。
- G3 时间戳贯通：时间戳从 Block.Metadata 进入 chunk payload，经检索结果 Source 与 chunk 详情 API 返回，作为视频「页码」定位锚点。
- G4 视频流式访问：提供支持 HTTP Range 的视频文件访问接口（认证 + 防路径穿越），为后续前端播放器跳转打基础。
- G5 回归安全：图片/音频处理器与既有文本格式行为不变。

## 功能需求

- F1 视频拆流：视频入库时用 ffmpeg 将视频拆分为视频流与音频流，分别处理；无音频轨时不拆音频（不报错）。ffmpeg demux（copy）不重编码；拆流产物为临时文件用完即删；记录媒体信息（duration_ms / video_codec / audio_codec / has_audio）。多音频轨默认主音轨；损坏音频轨记 warning 不阻断。
- F2 固定抽帧：视频流按配置的固定时间间隔抽帧，每帧送视觉模型（VLM）生成描述，产出带时间戳的 Image Block（含 timestamp_ms、frame_index）。
- F3 场景检测抽帧：两阶段模式——先低频预抽帧（如 2fps）→ 独立视觉 embedding 计算相邻帧语义相似度 → 相似度显著下降处判定场景切换 → 保留场景代表帧送 VLM。禁止逐帧 embedding。
- F4 抽帧策略配置：全局配置 frame_strategy: fixed | scene；scene 依赖独立视觉 embedding 配置；未配置视觉 embedding 时选 scene 应报配置缺失错误。scene 可配 sample_fps / similarity_threshold / min_scene_duration_ms。
- F5 音频流独立 ASR：拆出的音频流独立送语音转写（ASR），产出带起止时间戳的 Audio Block；未配置 speech 时跳过音轨并记录 warning（不阻断）。ASR 返回的时间戳原样保留，不做二次切割。**降级声明：provider=dashscope 时 qwen ASR 不返回时间戳，该 provider 下所有音频 chunk 时间戳为 0，定位锚点不可用。**
- F6 时间戳语义：Image Block 与 Audio Block 的时间戳作为「视频页码」，统一毫秒整数（StartMs/EndMs）。
- F7 时间戳贯通检索链路：Block 时间戳写入 chunk 检索 payload（start_ms/end_ms/source_type/video_id），经检索结果 Source 返回；chunk 详情接口返回该 chunk 对应时间戳。
- F8 视频流式访问：提供视频文件访问接口，支持 HTTP Range（返回 206）；video_id → 数据库查询 → 受控存储路径；沿用既有认证与知识库访问权校验，防路径穿越。
- F9 回归安全：图片、音频处理器与 txt/md/pdf/docx/csv/excel/html 的行为保持不变。

## 非功能需求

- N1 性能：场景检测两阶段（低频预抽帧 → embedding 相似度 → 代表帧 → VLM），禁止逐帧 embedding；预抽帧率、固定抽帧间隔可配。
- N2 成本：拆流 ffmpeg demux（copy 不重编码）；视频/音频临时产物用完即删。
- N3 时间戳一致性：统一毫秒整数；ASR 返回时间戳原样保留，不做二次切割或重算。
- N4 安全：视频访问仅 video_id → 数据库查询 → 受控存储路径；禁止请求直接携带路径；支持 HTTP Range；沿用 Bearer 认证与知识库访问权校验。
- N5 错误语义：frame_strategy=scene 但视觉 embedding 未配置 → 明确配置错误；多音轨默认主音轨；损坏音频轨记 warning 不阻断。
- N6 可观测：记录媒体信息、场景数、抽帧数、降级 warning。
- N7 兼容：图片/音频处理器与 txt/md/pdf/docx/csv/excel/html 行为不变。

## 不做的事

- 不做前端视频播放器与时间戳跳转交互（本次打通时间戳数据链路 + Range 接口，前端跳转留待下一迭代）。
- 不做音频流重编码/转码（demux 原样提取，转码由 ASR 端点自行处理）。
- 不做逐帧 embedding（场景检测为两阶段）。
- 不做多音轨选择（默认主音轨）、说话人分离、字幕对齐。
- 不做场景边界的帧级精确定位（场景边界为预抽帧粒度）。
- 不做视频缩略图/封面生成。

## 验收标准

- AC1（F1）：上传含音视频轨的视频 → 拆流出视频流与音频流，媒体信息含 duration_ms、has_audio、audio_codec。
- AC2（F2）：fixed 策略 → 按间隔产出 Image Block，含 timestamp_ms 与 frame_index。
- AC3（F3）：scene 策略 → 两阶段处理后产出的场景代表帧数显著小于预抽帧数，且每个 Image Block 带场景时间戳。
- AC4（F4）：frame_strategy=fixed/scene 均可用；scene 且未配置视觉 embedding → 明确配置错误。
- AC5（F5）：音频流独立送 ASR，Audio Block 保留 ASR 原始起止时间戳；未配置 speech → warning 降级不阻断。
- AC6（F6）：所有时间戳为毫秒整数，无 float 秒。
- AC7（F7）：chunk payload 含 start_ms/end_ms/source_type（及视频标识）；检索结果 Source 与 chunk 详情接口均返回时间戳。
- AC8（F8）：GET /api/v1/videos/{id}/stream 支持 Range（返回 206）；不存在/越权返回 404；无法通过请求参数进行路径穿越。
- AC9（F9）：图片/音频处理器与 7 种文本格式的上传、入库、检索回归全绿。
