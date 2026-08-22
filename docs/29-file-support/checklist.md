# 文件上传支持判断（Support）Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [x] 后端 `Support(filename|mime)` 判断已实现且可被调用（验证：`go build ./...` 通过）
- [x] `SupportedTypes()` 能枚举全部已注册扩展名并给出类别（验证：`go test ./internal/loader/...` 通过）
- [x] 新增 `GET /api/v1/documents/supported-types` 返回支持列表（验证：`TestSupportedTypesEndpoint` 通过）
- [x] 前端上传面板改用动态支持列表，不再写死扩展名（验证：`cd frontend && npm run build` 通过）

## 集成
- [x] 上传接口对「格式不支持」与「能力未配置」分别返回可读错误（验证：`TestUploadUnsupportedFormat` / `TestUploadMultimediaCapabilityMissing` 通过）
- [x] 上传预检的接受/拒绝判定与 `Support` 同源，不产生两套逻辑（验证：`go test ./...` 通过）

## 编译与测试
- [x] 后端编译无错误（验证：`go build ./...`）
- [x] 后端单元测试全部通过（验证：`go test ./...`）
- [x] 前端构建通过（验证：`cd frontend && npm run build`）
- [x] gofmt 格式检查通过（验证：`gofmt -l internal/loader internal/api` 无输出）

## 端到端场景
- [ ] 场景 1（AC2/AC6/AC7）：未配置 `multimedia.speech.api_key` 时，前端上传面板中音频类型不可选并提示缺少语音转写配置；配置 speech key 并重启后，音频类型变为可选且能上传成功（验证：需完整运行环境 + 浏览器手工验证；HTTP 层等价行为已由 `TestSupportedTypesEndpoint` / `TestUploadMultimediaCapabilityMissing` 覆盖）
- [ ] 场景 2（AC4 边界）：拖入一个系统不认识的格式（如 `.exe`），文件被跳过并提示不支持（验证：需浏览器手工验证；前端 `isValidFile` 对未知扩展名返回 false，已通过构建与逻辑审查）
