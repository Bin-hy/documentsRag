# 修复空列表返回 null 导致前端白屏 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/store/kb.go` | ListKBs 返回空 slice |
| 修改 | `internal/store/document.go` | ListDocuments 返回空 slice |
| 修改 | `internal/store/task.go` | ListTasks / ClaimPendingTasks 返回空 slice |
| 修改 | `internal/store/apikey.go` | ListAPIKeys 返回空 slice |
| 修改 | `internal/store/history.go` | Get 返回空 slice |
| 修改 | `frontend/src/stores/kb.ts` | load() 赋值 `?? []` |
| 修改 | `frontend/src/stores/doc.ts` | load() 赋值 `?? []` |
| 修改 | `frontend/src/views/ApiKeysView.vue` | load() 赋值 `?? []` |

## T1: 后端 store 列表方法返回空 slice

**文件：** `internal/store/kb.go`、`internal/store/document.go`、`internal/store/task.go`、`internal/store/apikey.go`、`internal/store/history.go`
**依赖：** 无
**步骤：**
1. `kb.go` ListKBs：`var kbs []KnowledgeBase` → `kbs := make([]KnowledgeBase, 0)`
2. `document.go` ListDocuments：`var docs []Document` → `docs := make([]Document, 0)`
3. `task.go` ListTasks：`var tasks []Task` → `tasks := make([]Task, 0)`
4. `task.go` ClaimPendingTasks：`var tasks []Task` → `tasks := make([]Task, 0)`
5. `apikey.go` ListAPIKeys：`var keys []APIKey` → `keys := make([]APIKey, 0)`
6. `history.go` Get：`var msgs []llm.Message` → `msgs := make([]llm.Message, 0)`

**验证：** `go build ./...` 编译通过；`go test ./internal/store/... ./internal/api/...` 通过（api_test 中空列表场景应输出 `[]`）

## T2: 前端 store 赋值兜底

**文件：** `frontend/src/stores/kb.ts`、`frontend/src/stores/doc.ts`
**依赖：** 无（与 T1 并行）
**步骤：**
1. `stores/kb.ts` load()：`this.kbs = await kbApi.listKbs()` → `this.kbs = (await kbApi.listKbs()) ?? []`
2. `stores/doc.ts` load()：`this.documents = await docApi.listDocuments(kbId)` → `this.documents = (await docApi.listDocuments(kbId)) ?? []`

**验证：** `npm run build` 通过（frontend 目录）；类型检查无错误

## T3: ApiKeysView 赋值兜底

**文件：** `frontend/src/views/ApiKeysView.vue`
**依赖：** T2（同批前端改动，可合并执行）
**步骤：**
1. `load()` 内：`keys.value = await keyApi.listKeys()` → `keys.value = (await keyApi.listKeys()) ?? []`

**验证：** `npm run build` 通过

## T4: 全量验证

**文件：** 无新增
**依赖：** T1、T2、T3
**步骤：**
1. `go build ./...` 全项目编译
2. `go test ./...` 全部测试通过
3. `go vet ./...` 无告警
4. `cd frontend && npm run build` 前端构建通过
5. 手工验证：空库时 curl `GET /api/v1/knowledge-bases` 返回 `{"data":[]}`

**验证：** 上述命令全部通过

## 执行顺序

```
T1 ──┐
     ├→ T4
T2 → T3（可合并为一次前端改动）
```

T1（后端）与 T2/T3（前端）互不依赖，可并行；T4 汇总验证。
