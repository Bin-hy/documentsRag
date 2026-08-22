# 文件上传支持判断（Support）Plan

## 架构概览

核心判断能力放在 `internal/loader`（格式识别与能力预检的既有归属），复用已注册的 parser 与 `MediaCapabilityChecker`，不新维护「类型→能力」映射表：

- **后端**：`Registry` 新增两个方法——`Support(info FileInfo) SupportResult`（单个文件是否可处理 + 原因）和 `SupportedTypes() []SupportedType`（枚举当前注册的全部扩展名及支持状态，供前端/查询入口使用）。多媒体 parser 额外实现一个可选接口 `MediaCategory` 暴露类别（image/audio/video），文本 parser 不实现即视为 `text`。
- **API**：上传预检改用 `Support` 作为接受/拒绝的唯一判定；新增 `GET /api/v1/documents/supported-types` 返回枚举列表。
- **前端**：上传面板挂载时拉取支持列表，动态生成文件选择器 `accept`、拖拽校验与提示文案，替换写死的扩展名列表。

判断依据（与 spec F2 对齐）：文本无需外部能力恒支持；图片需 vision；音频需 speech；视频需 vision（scene 策略额外需 vision_embedding）。这些已由各 parser 的 `CheckCapabilities()` 表达，`Support`/`SupportedTypes` 直接复用。

## 核心数据结构

### SupportResult（internal/loader/support.go）

单个文件的支持判断结果。

```go
type SupportResult struct {
    Supported bool   `json:"supported"`
    Reason    string `json:"reason,omitempty"` // 不支持原因；Supported 时为空
}
```

### SupportedType（internal/loader/support.go）

单个扩展名类型的支持状态（枚举给查询入口/前端）。

```go
type SupportedType struct {
    Ext       string `json:"ext"`                 // 如 ".mp3"（小写）
    Category  string `json:"category"`            // text / image / audio / video
    Supported bool   `json:"supported"`
    Reason    string `json:"reason,omitempty"`    // 不支持原因
}
```

### Registry 接口扩展（internal/loader/types.go）

```go
type Registry interface {
    Register(parser Parser)
    Resolve(info FileInfo) (Parser, error)
    Support(info FileInfo) SupportResult        // 新增：单个文件是否可处理 + 原因
    SupportedTypes() []SupportedType            // 新增：枚举全部已注册扩展名支持状态
}
```

### MediaCategory（internal/loader/capability.go，可选接口）

```go
// MediaCategory 多媒体 parser 暴露类别，供支持列表分组展示；文本 parser 不实现即视为 "text"
type MediaCategory interface {
    MediaCategory() string // "image" / "audio" / "video"
}
```

## 模块设计

### 模块 A：internal/loader 支持判断（support.go，新建）

**职责：** 提供 `Support` 与 `SupportedTypes` 两个判断入口，复用 `Resolve` + `MediaCapabilityChecker`。

**对外接口：**

```go
// Support 依据格式识别 + 能力配置判断单个文件当前是否可处理。
func (r *defaultRegistry) Support(info FileInfo) SupportResult
// SupportedTypes 枚举当前注册表全部扩展名的支持状态（稳定按 ext 排序）。
func (r *defaultRegistry) SupportedTypes() []SupportedType
```

**实现要点：**
- `Support`：`Resolve` 失败 → 不支持，reason 用 `ErrUnsupportedFormat.Error()`；成功且 parser 实现 `MediaCapabilityChecker` → 调用 `CheckCapabilities()`，错误即不支持并取其 `Error()` 为 reason；否则支持。
- `SupportedTypes`：遍历 `extMap`，每个 ext 取其 parser，类别 = `MediaCategory()`（缺省 "text"），支持状态同 `Support` 的能力检查逻辑，reason 取 `CheckCapabilities()` 错误文案。

**依赖：** `types.go`（Parser/FileInfo/Registry）、`capability.go`（MediaCapabilityChecker）、`errors.go`。

### 模块 B：多媒体 parser 类别标记（parser_image/audio/video.go，修改）

**职责：** 让多媒体 parser 能被 `SupportedTypes` 识别出类别。

**改动：** 三个 parser 各新增一个方法：

```go
func (p *imageParser) MediaCategory() string { return "image" }
func (p *audioParser) MediaCategory() string { return "audio" }
func (p *videoParser) MediaCategory() string { return "video" }
```

### 模块 C：API 层（internal/api，修改）

**职责：** 上传预检改用 `Support`；新增支持列表查询接口。

**改动：**

1. `handler_doc.go` 的 `UploadDocument`：
   - 用 `h.registry.Support(info)` 取代「`Resolve` 失败 → 400」+「`CheckCapabilities` → 400」两段分支；不支持时 `Fail(400, reason)`。
   - 支持时仍 `Resolve` 拿到 parser，仅对非 `MediaCapabilityChecker` 的文本 parser 执行 `precheckReadable`（内容可读性预检保持不变）。
2. 新增 `handler_doc.go` 的 `SupportedTypes` handler（或独立小文件）：
   - 返回 `OK(c, h.registry.SupportedTypes())`。
3. `router.go`：文档组新增 `v1.GET("/documents/supported-types", h.SupportedTypes)`。

### 模块 D：前端（frontend/src，修改）

**职责：** 用后端支持列表替换写死列表。

**改动：**

1. `api/types.ts`：新增 `SupportedType { ext; category; supported; reason? }` 类型。
2. `api/doc.ts`：新增 `getSupportedTypes(): Promise<SupportedType[]>`（GET `/api/v1/documents/supported-types`）。
3. `components/UploadPanel.vue`：
   - `onMounted` 拉取支持列表，失败时回退到文本扩展名旧列表。
   - 派生 `accept`（支持类型的 ext 用逗号连接）。
   - `isValidFile` 改为按动态支持列表（仅 `supported === true` 的 ext）判断。
   - 提示文案：展示支持的类别；对「已认识但能力未配置」的类型（`supported === false` 且 `reason` 非空）给出原因说明。

## 模块交互

```
前端 UploadPanel
   │ onMounted
   ▼
GET /api/v1/documents/supported-types ──► handler.SupportedTypes ──► registry.SupportedTypes()
   │                                                                    │
   │  返回 [{ext,category,supported,reason}...]                          ▼
   │◄───────────────────────────────────────────────  遍历 extMap + CheckCapabilities
   ▼
用户选择/拖拽文件（accept + isValidFile 基于支持列表过滤）
   │
   ▼
POST /api/v1/documents/upload ──► handler.UploadDocument ──► registry.Support(info)
   │                                                           │ Resolve + CheckCapabilities
   │ 不支持 → 400 reason                                        │
   │ 支持 → 保存文件 → 文本类 precheckReadable → 入队            ▼
   ▼
返回 task_id
```

## 文件组织

```
internal/loader/
├── support.go          — 新建：SupportResult、SupportedType、Support、SupportedTypes
├── types.go            — 修改：Registry 接口新增 Support/SupportedTypes
├── capability.go       — 修改：新增 MediaCategory 接口
├── parser_image.go     — 修改：MediaCategory() "image"
├── parser_audio.go     — 修改：MediaCategory() "audio"
├── parser_video.go     — 修改：MediaCategory() "video"
└── support_test.go     — 新建：Support/SupportedTypes 单测

internal/api/
├── handler_doc.go      — 修改：UploadDocument 改用 Support；新增 SupportedTypes handler
├── router.go           — 修改：新增 GET /documents/supported-types
└── api_test.go         — 修改：上传拒绝文案、supported-types 端点用例

frontend/src/
├── api/types.ts        — 修改：SupportedType 类型
├── api/doc.ts          — 修改：getSupportedTypes
└── components/UploadPanel.vue — 修改：动态支持列表
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 判断来源 | 复用 Registry 已注册 parser + `CheckCapabilities` | 与上传预检同源，不重复维护「类型→能力」映射表（DRY） |
| Support 返回形状 | `SupportResult{Supported, Reason}` | 满足 spec F1 的 bool + 原因 |
| 枚举维度 | `SupportedTypes` 只枚举扩展名，不枚举 MIME | 前端按文件名判断、`accept` 用扩展名、`Resolve` 优先扩展名；MIME 对前端无用，避免冗余 |
| 类别标记 | 新增可选接口 `MediaCategory`，文本 parser 不实现视为 "text" | 零侵入：文本 parser 无需改动 |
| 上传预检 | `Support()` 作为接受/拒绝唯一判定；内容可读性预检仅文本类保留 | 统一「格式不支持 / 能力未配置」语义，内容级扫描件检查不改 |
| 前端动态化 | 挂载时拉取 `supported-types`，据此生成 accept 与校验 | 替代硬编码列表，支持多媒体类型 |
| 能力判定边界 | 只判配置是否就绪（`Available()`），不探测服务在线可用性 | 与 spec N2 一致，避免上传/查询产生网络调用 |
| 前端兜底 | 拉取失败回退到旧文本扩展名列表 | 后端异常时上传面板仍可用，不白屏 |
