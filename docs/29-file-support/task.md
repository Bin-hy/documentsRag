# 文件上传支持判断（Support）Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/loader/support.go` | SupportResult、SupportedType、Support、SupportedTypes 实现 |
| 修改 | `internal/loader/types.go` | Registry 接口新增 Support / SupportedTypes |
| 修改 | `internal/loader/capability.go` | 新增 MediaCategory 接口 |
| 修改 | `internal/loader/parser_image.go` | MediaCategory() "image" |
| 修改 | `internal/loader/parser_audio.go` | MediaCategory() "audio" |
| 修改 | `internal/loader/parser_video.go` | MediaCategory() "video" |
| 新建 | `internal/loader/support_test.go` | Support / SupportedTypes 单测 |
| 修改 | `internal/api/handler_doc.go` | UploadDocument 改用 Support；新增 SupportedTypes handler |
| 修改 | `internal/api/router.go` | 新增 GET /documents/supported-types 路由 |
| 修改 | `internal/api/api_test.go` | 上传拒绝文案 + supported-types 端点用例 |
| 修改 | `frontend/src/api/types.ts` | SupportedType 类型 |
| 修改 | `frontend/src/api/doc.ts` | getSupportedTypes |
| 修改 | `frontend/src/components/UploadPanel.vue` | 动态支持列表 |

## T1: 注册表接口扩展与支持判断实现

**文件：** `internal/loader/types.go`、`internal/loader/support.go`
**依赖：** 无

**步骤：**
1. `types.go` 的 `Registry` 接口新增两个方法：
   - `Support(info FileInfo) SupportResult`
   - `SupportedTypes() []SupportedType`
2. `support.go` 定义 `SupportResult`（`Supported bool`、`Reason string`，带 json tag）与 `SupportedType`（`Ext/Category/Supported/Reason`，带 json tag）。
3. 实现 `(r *defaultRegistry) Support(info FileInfo) SupportResult`：
   - `r.Resolve(info)` 失败 → `{Supported:false, Reason: err.Error()}`。
   - parser 实现 `MediaCapabilityChecker` 时调用 `CheckCapabilities()`，错误 → `{false, err.Error()}`。
   - 否则 → `{Supported:true}`。
4. 实现 `(r *defaultRegistry) SupportedTypes() []SupportedType`：
   - 遍历 `r.extMap`，对每个 ext：类别 = parser 若实现 `MediaCategory` 取其值，否则 `"text"`；能力检查同步骤 3 得 supported/reason。
   - 结果按 `Ext` 升序排序后返回。

**验证：** `go build ./internal/loader/...` 编译通过

## T2: MediaCategory 接口与多媒体 parser 类别

**文件：** `internal/loader/capability.go`、`parser_image.go`、`parser_audio.go`、`parser_video.go`
**依赖：** T1

**步骤：**
1. `capability.go` 新增接口：
   ```go
   type MediaCategory interface {
       MediaCategory() string // "image" / "audio" / "video"
   }
   ```
2. `parser_image.go` 新增 `func (p *imageParser) MediaCategory() string { return "image" }`。
3. `parser_audio.go` 新增 `func (p *audioParser) MediaCategory() string { return "audio" }`。
4. `parser_video.go` 新增 `func (p *videoParser) MediaCategory() string { return "video" }`。

**验证：** `go build ./internal/loader/...` 编译通过

## T3: loader 支持判断单测

**文件：** `internal/loader/support_test.go`
**依赖：** T2

**步骤：** 编写表驱动测试，覆盖：
1. 文本类型（如 `.txt`）`Support` 返回 `Supported=true`，不依赖能力。
2. 用 `NewDefaultRegistry()`（多媒体为 nil 能力）：`.mp3` → `Supported=false` 且 `Reason` 含 `speech`；`.png` → false 且含 `vision`；`.mp4` → false 且含 `vision`。
3. 用 `buildLoaderRegistry` 同等装配（仅 speech 就绪）：`.mp3` → true。
4. 未知格式 `.exe` → false，Reason 含「不支持的文件格式」。
5. `SupportedTypes()`：`.mp3` 的 `Category=="audio"`、`.png=="image"`、`.mp4=="video"`、`.txt=="text"`；列表按 Ext 升序。

**验证：** `go test ./internal/loader/...` 全部通过

## T4: API 层改造

**文件：** `internal/api/handler_doc.go`、`internal/api/router.go`
**依赖：** T2

**步骤：**
1. `handler_doc.go` 的 `UploadDocument`：
   - 用 `h.registry.Support(info)` 取代原「`Resolve` 失败 → 400」+「`CheckCapabilities` → 400」两段；`!Supported` 时 `Fail(c, CodeBadRequest, reason)`。
   - `Supported` 时仍 `h.registry.Resolve(info)` 拿 parser，仅对非 `loader.MediaCapabilityChecker` 的 parser 且 `MinReadableChars>0` 执行 `precheckReadable`。
2. `handler_doc.go` 新增 `SupportedTypes(c *gin.Context)`，返回 `OK(c, h.registry.SupportedTypes())`（带 swagger 注释）。
3. `router.go` 文档组新增 `v1.GET("/documents/supported-types", h.SupportedTypes)`。

**验证：** `go build ./...` 编译通过

## T5: API 测试

**文件：** `internal/api/api_test.go`
**依赖：** T4

**步骤：** 参照现有 fakeRegistry/fakeParser 模式扩展：
1. supported-types 端点：用能返回 `SupportedTypes()` 的 fake registry 构造 handler，断言响应 code=0 且 data 为预期列表。
2. 上传不支持格式：断言 400 且 message 含「不支持的文件格式」。
3. 上传能力未配置（fake 多媒体 parser 实现 `MediaCapabilityChecker` 返回 `ErrMediaCapabilityMissing`）：断言 400 且 message 含 `speech` 或 `vision`。

**验证：** `go test ./internal/api/...` 全部通过

## T6: 前端类型与 API 函数

**文件：** `frontend/src/api/types.ts`、`frontend/src/api/doc.ts`
**依赖：** T1（SupportType JSON 形状）

**步骤：**
1. `types.ts` 新增：
   ```ts
   export interface SupportedType {
     ext: string
     category: 'text' | 'image' | 'audio' | 'video'
     supported: boolean
     reason?: string
   }
   ```
2. `doc.ts` 新增：
   ```ts
   export function getSupportedTypes(): Promise<SupportedType[]> {
     return request<SupportedType[]>({ method: 'GET', url: '/api/v1/documents/supported-types' })
   }
   ```

**验证：** `cd frontend && npx vue-tsc --noEmit`（待 T7 一并跑）

## T7: 前端上传面板动态化

**文件：** `frontend/src/components/UploadPanel.vue`
**依赖：** T6

**步骤：**
1. 引入 `onMounted`/`ref`、`getSupportedTypes` 与 `SupportedType` 类型。
2. 定义 `supportedTypes = ref<SupportedType[]>([])`；`onMounted` 拉取，失败回退到旧文本扩展名列表（构造 `supported:true` 的文本项）。
3. 派生 `accept`：`supportedTypes` 中 `supported===true` 的 `ext` 用逗号连接；模板 `el-upload` 的 `accept` 与 `isValidFile` 改用该列表。
4. 拖拽 `handleFiles` 的 `isValidFile` 改为按 `supported===true` 的 ext 判断；`rejected>0` 的提示展示当前支持类型。
5. 提示文案：列出支持类别；对「已认识但未配置能力」的类型（`supported===false` 且 `reason` 非空）展示原因。

**验证：** `cd frontend && npm run build`（vue-tsc + vite 构建通过）

## 执行顺序

```
T1 → T2 → T3 → T4 → T5
  ↘
    T6 → T7（前端，依赖 T1 的类型形状，可与 T2-T5 并行）
```
