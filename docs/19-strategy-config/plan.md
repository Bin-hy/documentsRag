# 策略可选化配置 Plan

## 架构概览

新增**策略配置层**，贯穿配置 → 存储 → API → 引擎 → 前端：

1. **config 层**：`StrategyConfig` 结构（六策略项），`rag.strategy` 段；全局默认。
2. **store 层**：`knowledge_bases` 表加 `strategy TEXT NOT NULL DEFAULT ''`（JSON 字符串）；`KnowledgeBase` 加 `Strategy string` 字段；CreateKB/UpdateKB/ListKBs/GetKB SQL 更新；schema.go 迁移（`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`）。
3. **API 层**：`createKBRequest`/`updateKBRequest` 加 `strategy`（JSON 对象可选）；`chatRequest` 加 `strategy` 字段；合并逻辑（请求 > 知识库 > 全局）在 handler 或 engine 层。
4. **引擎层**：`rag.Engine` 新增 `ResolveStrategy(global StrategyConfig, kbStrategy, reqStrategy)` → 合并后的生效策略；Ask/StreamAsk 改用生效策略决定路径（替代直接读 `e.cfg.*On()`）。
5. **前端层**：KbDetailView 加策略设置区块（开关组）；chat.ts 请求支持 strategy 字段。

### 关键设计：策略合并

```go
// StrategyConfig 策略配置（JSON 序列化，与 config.yaml rag.strategy 同构）
type StrategyConfig struct {
    Query        string `json:"query,omitempty"`        // single / multi
    Fusion       string `json:"fusion,omitempty"`       // rrf / none
    Decomposition string `json:"decomposition,omitempty"` // off / parallel / sequential
    StepBack     string `json:"step_back,omitempty"`    // off / on
    HyDE         string `json:"hyde,omitempty"`         // off / on
    Routing      string `json:"routing,omitempty"`      // off / auto
}
```
**合并规则**：请求级非空字段覆盖知识库级，知识库级非空覆盖全局；空字段继承低层级。合并后 `ValidateStrategy` 校验非法组合。

**向后兼容**：`StrategyConfig` 解析后生成 `EffectiveRAGConfig`（含 `MultiQueryOn/DecompositionOn/...` 等效值），engine 现有 `e.cfg.*On()` 改为从生效策略取值。未配置 strategy 时，生效策略 = 阶段一~三默认开关（`EnableRewrite` 保持现有语义）。

## 核心数据结构

### config（internal/config/config.go）
```go
type RAGConfig struct {
    // ... 现有字段
    Strategy StrategyConfig `yaml:"strategy"` // 全局默认策略
}
type StrategyConfig struct {
    Query         string `yaml:"query" json:"query"`
    Fusion        string `yaml:"fusion" json:"fusion"`
    Decomposition string `yaml:"decomposition" json:"decomposition"`
    StepBack      string `yaml:"step_back" json:"step_back"`
    HyDE          string `yaml:"hyde" json:"hyde"`
    Routing       string `yaml:"routing" json:"routing"`
}
// 默认值：query=multi, fusion=rrf, decomposition=off, step_back=off, hyde=off, routing=off
```

### store（internal/store/store.go、schema.go、kb.go）
```go
type KnowledgeBase struct {
    // ... 现有
    Strategy string `json:"strategy"` // JSON 字符串（StrategyConfig），空 = 用全局
}
// schema.go 迁移：ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT '';
// CreateKB/UpdateKB：INSERT/UPDATE 含 strategy 列
// ListKBs/GetKB：SELECT 含 strategy
```

### rag（internal/rag/strategy.go 新建）
```go
// EffectiveStrategy 合并后的生效策略
type EffectiveStrategy struct {
    Query         string
    Fusion        string
    Decomposition string
    StepBack      string
    HyDE          string
    Routing       string
}
// ResolveStrategy 合并三级：请求 > 知识库 > 全局
func ResolveStrategy(global, kb, req StrategyConfig) (EffectiveStrategy, error)
// ValidateStrategy 校验非法组合
func ValidateStrategy(s EffectiveStrategy) error
```

### engine 接入（internal/rag/engine.go）
```go
// RAGEngine 新增字段
type RAGEngine struct {
    // ... 现有
    strategy StrategyConfig // 全局策略（NewEngine 传入）
}
// Ask/StreamAsk 内：
//   kbStrategy := 从 store 读取（通过新接口 GetKBStrategy(kbID)）或 handler 传入
//   reqStrategy := 请求体 strategy
//   eff, err := ResolveStrategy(e.strategy, kbStrategy, reqStrategy)
//   用 eff 决定路径（替代 e.cfg.MultiQueryOn() 等）
```

### API（internal/api/handler_chat.go、handler_kb.go、router.go）
- `chatRequest` 加 `Strategy *store.StrategyConfig \`json:"strategy,omitempty"\``
- `createKBRequest`/`updateKBRequest` 加 `Strategy *store.StrategyConfig \`json:"strategy,omitempty"\``
- handler 需要访问 store 的 KB strategy（`h.store.GetKB` 已返回含 Strategy 的 KB）

### 前端（frontend/src/）
- `api/types.ts` 加 `StrategyConfig` 接口
- `api/kb.ts` createKb/updateKb 支持 strategy 参数
- `api/chat.ts` chatStream/chat 请求体支持 strategy
- `views/KbDetailView.vue` 加策略设置区块（开关组，保存调 updateKb）
- `views/ChatView.vue` 高级设置（可选，简单下拉）

## 模块设计

### config（T1）
**职责：** StrategyConfig 结构与默认值。
**改动：** `RAGConfig` 加 `Strategy`；`StrategyConfig` 定义；`applyDefaults` 设默认（query=multi、fusion=rrf、其余 off）。

### store（T2）
**职责：** strategy 列迁移与 CRUD。
**改动：** schema.go 加 ALTER；kb.go SQL 更新；store.go `KnowledgeBase` 加字段。

### rag strategy.go（T3）
**职责：** 合并与校验。
**改动：** `ResolveStrategy`、`ValidateStrategy`（非法组合：query=single+fusion=rrf；routing=auto+decomposition≠off 等）。

### engine 接入（T4）
**职责：** 用生效策略替代散开关。
**改动：** RAGEngine 加 `strategy` 字段 + `resolve(ctx, kbStrategy, reqStrategy)`；Ask/StreamAsk 的 `e.cfg.MultiQueryOn()` 等改从 eff 取。为控制范围：保留现有 `e.cfg.*On()` 语义，新增「策略覆盖」仅在提供 kb/req strategy 时生效（未提供 → 全局默认 = 现状）。

### API（T5）
**职责：** 请求级 + 知识库级字段。
**改动：** chatRequest/kb 请求加 strategy；handler 校验合并后传入 engine。

### 前端（T6）
**职责：** 设置 UI + 请求传参。

## 模块交互

```
config.yaml rag.strategy (全局) ─┐
knowledge_bases.strategy (kb级) ─┼→ ResolveStrategy(global, kb, req) → EffectiveStrategy
API 请求 body strategy (请求级) ─┘        │ 校验非法组合
                                          ▼
                                    engine Ask/StreamAsk
                                    eff.Query → multi? → SearchMulti
                                    eff.Decomposition → ? → tryDecompose
                                    eff.Routing → auto? → routeQuery
```

## 文件组织

```
internal/config/
├── config.go            — 修改：StrategyConfig + RAGConfig.Strategy + 默认值
configs/config.yaml      — 修改：rag.strategy 段注释示例
internal/store/
├── schema.go            — 修改：ALTER TABLE 迁移
├── kb.go                — 修改：SQL 含 strategy
├── store.go             — 修改：KnowledgeBase 加 Strategy
├── kb_test.go           — 修改：适配（如有）
internal/rag/
├── strategy.go          — 新建：ResolveStrategy / ValidateStrategy
├── strategy_test.go     — 新建：合并/校验测试
├── engine.go            — 修改：strategy 字段 + 生效策略接入
internal/api/
├── handler_chat.go      — 修改：chatRequest.strategy
├── handler_kb.go        — 修改：kb 请求 strategy
├── api_test.go          — 修改：请求级覆盖用例
internal/app/
├── app.go               — 修改：NewEngine 传全局 strategy
frontend/src/
├── api/types.ts         — 修改：StrategyConfig
├── api/kb.ts            — 修改：create/update 支持 strategy
├── api/chat.ts          — 修改：请求体 strategy
├── views/KbDetailView.vue — 修改：策略设置区块
├── views/ChatView.vue   — 修改：高级策略（可选）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 策略结构 | StrategyConfig（六字段，JSON） | 与 DB JSON 列、API 请求体同构，序列化简单 |
| 存储 | knowledge_bases.strategy TEXT（JSON） | 轻量迁移；JSON 灵活（字段可选） |
| 合并 | 请求 > 知识库 > 全局，空字段继承 | spec F5；实现简单（字段级覆盖） |
| 校验 | ResolveStrategy 内校验 | 单一入口，配置加载与请求都过它 |
| 引擎接入 | 保留 `e.cfg.*On()` 语义，新增覆盖 | 未提供 kb/req 策略时行为=现状（N1 兼容） |
| 默认值 | query=multi、fusion=rrf、其余 off | 与阶段一~三默认一致（保守） |
| 前端 | KbDetailView 开关组 + ChatView 高级设置 | 用户全选前端 UI；复用 Element Plus 表单 |
| 迁移 | ALTER TABLE ADD COLUMN IF NOT EXISTS | 幂等，兼容已有库 |
