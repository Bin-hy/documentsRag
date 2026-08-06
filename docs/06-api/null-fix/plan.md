# 修复空列表返回 null 导致前端白屏 Plan

## 架构概览

修复分两层，均只做「初始化兜底」，不改任何查询逻辑与接口契约：

1. **后端 store 层**：6 处列表/历史方法的返回值从 `var x []T`（nil slice）改为 `make([]T, 0)`（空 slice），使 JSON 序列化输出 `[]` 而非 `null`。
2. **前端 store/视图层**：3 处从接口接收列表数据的赋值处追加 `?? []` 兜底，保证组件状态永远是数组，`null.length` 不再可能发生。

后端是根因修复（API 语义：列表接口空时返回空数组），前端是防御加固（防止未来新增接口再犯同样错误时白屏）。

## 核心数据结构

无新增/变更的数据结构与接口。仅修改既有方法的**返回值初始化**：

```go
// 修改前（nil slice → JSON null）
var kbs []KnowledgeBase

// 修改后（空 slice → JSON []）
kbs := make([]KnowledgeBase, 0)
```

## 模块设计

### store 层（internal/store/）

**职责：** 列表方法空数据时返回空 slice，不再返回 nil。

| 文件:行 | 方法 | 改动 |
|---------|------|------|
| `kb.go:31` | `ListKBs` | `var kbs []KnowledgeBase` → `kbs := make([]KnowledgeBase, 0)` |
| `document.go:39` | `ListDocuments` | `var docs []Document` → `docs := make([]Document, 0)` |
| `task.go:45` | `ListTasks` | `var tasks []Task` → `tasks := make([]Task, 0)` |
| `task.go:80` | `ClaimPendingTasks` | 同上 |
| `apikey.go:34` | `ListAPIKeys` | `var keys []APIKey` → `keys := make([]APIKey, 0)` |
| `history.go:53` | `PostgresHistoryStore.Get` | `var msgs []llm.Message` → `msgs := make([]llm.Message, 0)` |

### 前端层（frontend/src/）

**职责：** 接口数据赋值处 `?? []` 兜底，状态恒为数组。

| 文件 | 位置 | 改动 |
|------|------|------|
| `stores/kb.ts` | `load()` 内 `this.kbs = await kbApi.listKbs()` | → `this.kbs = (await kbApi.listKbs()) ?? []` |
| `stores/doc.ts` | `load()` 内 `this.documents = await docApi.listDocuments(kbId)` | → `this.documents = (await docApi.listDocuments(kbId)) ?? []` |
| `views/ApiKeysView.vue` | `load()` 内 `keys.value = await keyApi.listKeys()` | → `keys.value = (await keyApi.listKeys()) ?? []` |

**说明：**
- `KbListView.vue:88`、`KbDetailView.vue:22/56`、`ApiKeysView.vue:103` 的 `.length` 访问依赖 store 状态，store 兜底后自然安全，无需改动视图。
- `stores/chat.ts` 的 `switchSession` 已用 `Array.isArray(msgs)` 防御（chat.ts:71），`sessions` 来自 localStorage 且 `loadSessions` 已兜底返回 `[]`，聊天页不在此次修复范围，后端 `history.go` 改动仅为 API 语义一致性。

## 模块交互

```
GET /api/v1/knowledge-bases（空库）
  → store.ListKBs 返回 make([]KnowledgeBase, 0)
  → gin 序列化 {"data":[]}
  → 前端 kbStore.load() 赋值 (…) ?? [] → kbs = []
  → KbListView 模板 kbs.length === 0 → 正常显示空状态 + 新建按钮
```

## 文件组织

```
docs-rag/
├── internal/store/
│   ├── kb.go        — ListKBs 返回空 slice
│   ├── document.go  — ListDocuments 返回空 slice
│   ├── task.go      — ListTasks / ClaimPendingTasks 返回空 slice
│   ├── apikey.go    — ListAPIKeys 返回空 slice
│   └── history.go   — Get 返回空 slice
├── frontend/src/
│   ├── stores/kb.ts — load() ?? [] 兜底
│   ├── stores/doc.ts— load() ?? [] 兜底
│   └── views/ApiKeysView.vue — load() ?? [] 兜底
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 修复层级 | 后端 + 前端都修 | 后端修复 API 语义（列表接口空时返回 `[]` 是契约），前端兜底防未来回归；用户已确认 |
| 后端改法 | `make([]T, 0)` 而非 `[]T{}` 字面量 | 两种等价，`make` 意图更明确、惯用；不引入额外依赖 |
| 前端兜底位置 | store 赋值处而非视图模板 | 状态层保证不变式，视图无需逐个加防御；改动面最小 |
| 是否改 `client.ts` request | 否 | `data` 可能是对象（单条实体），统一 `?? []` 会破坏对象类型；按 store 逐个处理更精确 |
| 是否改 `chat.ts` | 否 | 已有 `Array.isArray` 防御；`history.go` 改 `[]` 仅为一致性 |
