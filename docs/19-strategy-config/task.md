# 策略可选化配置 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | StrategyConfig + RAGConfig.Strategy + 默认值 |
| 修改 | `configs/config.yaml` | rag.strategy 段注释示例 |
| 修改 | `internal/store/schema.go` | ALTER TABLE 迁移 |
| 修改 | `internal/store/kb.go` | SQL 含 strategy 列 |
| 修改 | `internal/store/store.go` | KnowledgeBase 加 Strategy 字段 |
| 新建 | `internal/rag/strategy.go` | ResolveStrategy / ValidateStrategy |
| 新建 | `internal/rag/strategy_test.go` | 合并/校验测试 |
| 修改 | `internal/rag/engine.go` | strategy 字段 + 生效策略接入 |
| 修改 | `internal/api/handler_chat.go` | chatRequest.strategy |
| 修改 | `internal/api/handler_kb.go` | kb 请求 strategy |
| 修改 | `internal/api/api_test.go` | 请求级覆盖用例 |
| 修改 | `internal/app/app.go` | NewEngine 传全局 strategy |
| 修改 | `frontend/src/api/types.ts` | StrategyConfig 接口 |
| 修改 | `frontend/src/api/kb.ts` | create/update 支持 strategy |
| 修改 | `frontend/src/api/chat.ts` | 请求体 strategy |
| 修改 | `frontend/src/views/KbDetailView.vue` | 策略设置区块 |
| 修改 | `frontend/src/views/ChatView.vue` | 高级策略（可选） |

## T1: config StrategyConfig

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. 定义 `StrategyConfig` 结构（六字段：Query/Fusion/Decomposition/StepBack/HyDE/Routing，string 类型 + yaml/json tag）
2. `RAGConfig` 加 `Strategy StrategyConfig \`yaml:"strategy"\``
3. `applyDefaults`：strategy 默认 query=multi、fusion=rrf、其余 off（空串按 off 处理，multi/rrf 为显式默认）
4. config.yaml rag 段加 `strategy:` 注释示例

**验证：** `go build ./internal/config/...` 通过

## T2: store strategy 列

**文件：** `internal/store/schema.go`、`internal/store/kb.go`、`internal/store/store.go`
**依赖：** 无（与 T1 并行）
**步骤：**
1. `schema.go`：`Migrate` 中追加 `ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT '';`
2. `store.go`：`KnowledgeBase` 加 `Strategy string \`json:"strategy"\``
3. `kb.go`：CreateKB INSERT 含 strategy；UpdateKB UPDATE 含 strategy；ListKBs/GetKB SELECT 含 strategy 并 Scan
4. 确认 fakeStore（api_test）的 KnowledgeBase 构造不受影响（零值 Strategy=""）

**验证：** `go build ./internal/store/...` + `go test ./internal/store/` 通过

## T3: rag strategy.go 合并与校验

**文件：** `internal/rag/strategy.go`、`internal/rag/strategy_test.go`
**依赖：** T1
**步骤：**
1. `EffectiveStrategy` 结构（六字段，含 Query/Fusion/Decomposition/StepBack/HyDE/Routing）
2. `ResolveStrategy(global, kb, req config.StrategyConfig) (EffectiveStrategy, error)`：
   - 字段级合并：req 非空覆盖 kb，kb 非空覆盖 global，空继承
   - 默认值兜底：Query 空→multi、Fusion 空→rrf、其余空→off
3. `ValidateStrategy(s EffectiveStrategy) error`：
   - `query=single && fusion=rrf` → 错误（无多路可融合）
   - `routing=auto && decomposition!=off` → 错误（路由已含分流，冲突）
   - `routing=auto && step_back=on` → 错误（路由分流已决策，重复）
   - 其他非法值（未知枚举）→ 错误
4. `strategy_test.go`：合并优先级（请求覆盖 kb、kb 覆盖全局）；默认值兜底；非法组合 3 例

**验证：** `go test ./internal/rag/ -run TestStrategy -v` 通过

## T4: engine 生效策略接入

**文件：** `internal/rag/engine.go`
**依赖：** T3
**步骤：**
1. `RAGEngine` 加 `strategy config.StrategyConfig` 字段（全局）；`NewEngine` 签名不变（strategy 从 cfg.Strategy 取）
2. 新增方法 `effectiveStrategy(kbStrategy, reqStrategy config.StrategyConfig) (EffectiveStrategy, error)`：调 ResolveStrategy，失败返回 err（调用方降级）
3. Ask/StreamAsk 策略分支改造：
   - 原 `if e.cfg.MultiQueryOn()` → `if eff.Query == "multi"`
   - 原 `if e.cfg.DecompositionOn()` → `if eff.Decomposition != "off"`
   - 原 `else if e.cfg.StepBackOn()` → `else if eff.StepBack == "on"`
   - 原 `if e.cfg.RoutingOn()` → `if eff.Routing == "auto"`
   - HyDE 检索：`if e.cfg.HyDEOn()` → `if eff.HyDE == "on"`
4. 兼容：未提供 kb/req strategy 时，eff = 全局默认（与现状一致）；保留 `e.cfg.*On()` 供现有测试用（T4 后改为从 eff 计算或直接替换）

**验证：** `go build ./internal/rag/...` 通过；现有测试不回归（未提供 strategy 时行为不变）

## T5: API 请求级 + 知识库级

**文件：** `internal/api/handler_chat.go`、`internal/api/handler_kb.go`、`internal/api/api_test.go`
**依赖：** T2、T4
**步骤：**
1. `handler_chat.go`：`chatRequest` 加 `Strategy *config.StrategyConfig \`json:"strategy,omitempty"\``；Chat/ChatStream 构造 engine 调用时把 kb 的 strategy（`h.store.GetKB` 取）与请求 strategy 传入（通过新增 `rag.WithStrategy(kbStrategy, reqStrategy)` AskOption 或 engine 方法）
2. `handler_kb.go`：`createKBRequest`/`updateKBRequest` 加 `Strategy *config.StrategyConfig \`json:"strategy,omitempty"\``；CreateKB/UpdateKB 把 strategy JSON 序列化存 store（kb.Strategy = json.Marshal）
3. `api_test.go`：新增用例——请求带 strategy（如 query=single）→ 走单查询路径；kb 带 strategy → 该 kb 生效
4. 需新增 `rag.WithStrategy` AskOption 或让 engine 方法接收 strategy 参数（选 AskOption 更小改动）

**验证：** `go build ./...` + `go test ./internal/api/` 通过

## T6: app 装配 + 前端

**文件：** `internal/app/app.go`、`frontend/src/api/types.ts`、`frontend/src/api/kb.ts`、`frontend/src/api/chat.ts`、`frontend/src/views/KbDetailView.vue`、`frontend/src/views/ChatView.vue`
**依赖：** T5（API 字段）+ T4
**步骤：**
1. `app.go`：确认 NewEngine 已传 cfg.Strategy（T4 已处理）；handler 无需额外装配（strategy 从 store 读）
2. `types.ts`：`StrategyConfig` 接口（六字段可选）
3. `kb.ts`：`createKb(name, desc, strategy?)` / `updateKb(id, name, desc, strategy?)` 传 strategy
4. `chat.ts`：`ChatRequest` 加 `strategy?`；chatStream/chat 传参
5. `KbDetailView.vue`：加「检索策略」设置区块（表单：查询模式单选、分解/回退/HyDE/路由开关），保存调 updateKb
6. `ChatView.vue`：输入区加「策略」高级下拉（可选，简单实现）

**验证：** `cd frontend && npm run build` 通过；`go build ./...` 通过

## T7: 全量验证 + 冒烟

**文件：** 无新增
**依赖：** T1-T6
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿；`cd frontend && npm run build` + `npm test` 通过
2. 冒烟：创建带 strategy 的知识库 → 问答按该策略路径（日志断言）；请求带 strategy 覆盖（日志断言）
3. 前端浏览器操作 KbDetailView 策略设置保存（用户本机）

**验证：** 上述命令全绿；冒烟日志符合预期

## 执行顺序

```
T1 ─┐
    ├→ T3 → T4 → T5 → T6 → T7
T2 ─┘
```

T1（config）与 T2（store）并行；T3 依赖 T1；T4 依赖 T3；T5 依赖 T2+T4；T6 依赖 T5；T7 收尾。
