# Decomposition 问题分解 + Step-Back 回退 Plan

## 架构概览

两个策略都在 `rag.Engine` 的问答编排层实现，作为 **prepare 之前的外层分支**：

1. **Decomposition（问题分解）**：`Ask`/`StreamAsk` 入口先判定复杂度 → 复杂时走「分解 → 逐子问题检索/生成 → 综合」独立流程；简单时走常规 prepare。综合阶段**复用现有生成**（messages 由各子问题上下文拼接 + 综合指令）。
2. **Step-Back（回退查询）**：判定需要回退时，先生成回退问题单独检索，再把「回退上下文 + 原问题检索上下文」合并注入生成。

两者互斥：Decomposition 判定优先（都启用时只走 Decomposition）。

### 核心设计选择：子问题「检索 → 生成」还是「只检索 → 综合」？

- **方案 A（只检索 → 一次综合生成）**：每个子问题只检索（不单独生成答案），把各子问题的**检索上下文**全部拼进最终生成消息。优点：少 N 次 LLM 生成、延迟低、引用来源完整；缺点：子问题间的信息综合靠最终 LLM。
- **方案 B（检索 + 子答案 → 综合）**：每个子问题检索后先生成子答案，再把子答案拼给最终生成。优点：子问题深度理解；缺点：N+1 次 LLM 调用，成本/延迟高。

**推荐方案 A**（只检索 → 一次综合）：符合 spec「综合回答基于所有子问题答案」且成本可控（仅判定 + 分解 + 综合共 2 次额外 LLM 调用）；子问题检索上下文已足够 LLM 综合。Sequential 模式用**前序子问题的检索上下文**作为后续参考（不生成子答案）。

## 核心数据结构

### RAGConfig 扩展（internal/config/config.go）
```go
type RAGConfig struct {
    // ... 现有（含阶段一 multi query）
    DecompositionEnabled *bool  `yaml:"decomposition_enabled"`  // nil 视为关闭
    DecompositionMode    string `yaml:"decomposition_mode"`     // parallel / sequential，默认 parallel
    DecompositionMaxSub  int    `yaml:"decomposition_max_sub"`  // 子问题上限，默认 5
    StepBackEnabled      *bool  `yaml:"step_back_enabled"`      // nil 视为关闭
    DecompositionTemplatePath string `yaml:"decomposition_template_path"`
    StepBackTemplatePath      string `yaml:"step_back_template_path"`
}
func (c RAGConfig) DecompositionOn() bool { return c.DecompositionEnabled != nil && *c.DecompositionEnabled }
func (c RAGConfig) StepBackOn() bool      { return c.StepBackEnabled != nil && *c.StepBackEnabled }
```

### promptTemplates 扩展（internal/rag/prompt.go）
```go
type promptTemplates struct {
    system, context, rewrite, multiQuery string
    decomposeJudge  string // 判定是否分解（输出 JSON {decompose: bool, reason}）
    decomposeList   string // 生成子问题列表（输出 JSON 数组）
    stepBackJudge   string // 判定是否回退（输出 JSON {step_back: bool, question}）
}
```

### 内部结构（internal/rag/engine.go 或新文件 decompose.go）
```go
// decomposeResult 分解判定与子问题
type decomposeResult struct {
    ShouldDecompose bool
    SubQuestions    []string
}

// stepBackResult 回退判定与回退问题
type stepBackResult struct {
    ShouldStepBack bool
    StepBackQuery  string
}
```

## 模块设计

### rag.Engine 编排（internal/rag/engine.go + decompose.go）
**职责：** Ask/StreamAsk 入口分支 + 分解/回退流程。
**改动：**
- `Ask` / `StreamAsk` 开头：
  ```go
  if e.cfg.DecompositionOn() && !o.StepBackOnly { // 互斥：Decomposition 优先
      if res, ok, err := e.tryDecompose(ctx, sessionID, question, o); err == nil && ok {
          return res, nil
      } // 判定为不分解或失败 → 落回常规
  } else if e.cfg.StepBackOn() {
      if res, ok, err := e.tryStepBack(...); err == nil && ok {
          return res, nil
      }
  }
  // 常规 prepare 路径（现状）
  ```
- `tryDecompose(ctx, sessionID, question, o) (*RAGResult, bool, error)`：
  1. `judgeDecompose`：LLM 判定（`decomposeJudge` 模板）→ `{decompose: bool}`；false → 返回 (nil, false, nil) 落常规
  2. `listSubQuestions`：LLM 生成子问题（`decomposeList` 模板，上限 MaxSub）→ `[]string`
  3. 逐子问题检索：parallel（并发）/ sequential（前序检索上下文拼入后续 prompt 作参考）——每子问题 `retriever.Search`（Multi-Query 若启用则 `SearchMulti`）
  4. 综合：把各子问题检索上下文 + 原问题拼进 user 消息，`llm.Generate` 一次生成最终回答；Sources = 所有子问题检索结果
  5. 落历史（question + answer），返回 `RAGResult{Answer, Sources}`
- `tryStepBack(ctx, sessionID, question, o) (*RAGResult, bool, error)`：
  1. `judgeStepBack`：LLM 判定（`stepBackJudge`）→ `{step_back: bool, question: 回退问题}`
  2. 回退问题检索（`retriever.Search` 或 `SearchMulti`）
  3. 原问题检索（常规）
  4. 合并上下文注入生成；Sources = 回退 + 原问题检索结果

**失败语义：** 判定/解析/检索失败 → 返回 (nil, false, err)，外层静默降级常规路径（`err == nil && ok` 才走策略）。

### StreamAsk 适配
`StreamAsk` 需支持分解/回退的**流式**输出：分解/回退路径的最终回答仍可流式（综合生成的 `StreamGenerate`）；子问题阶段无流式（内部完成），最终综合才流式。为控制复杂度：**StreamAsk 走分解/回退时，综合阶段用 `StreamGenerate` 流式输出**，前置判定/检索阶段同步完成。

### config（internal/config/config.go、configs/config.yaml）
字段 + `applyDefaults`（Mode 默认 parallel、MaxSub 默认 5）+ yaml 注释示例（默认关闭）。

### 测试
- `internal/rag/engine_test.go`：
  - `TestAsk_DecompositionParallel`：判定复杂 → 子问题并发检索 → 综合生成，Sources 来自各子问题
  - `TestAsk_DecompositionSequential`：前序子问题上下文拼入后续（断言检索次数与顺序）
  - `TestAsk_DecompositionSkip`：判定不分解 → 走常规（检索数与常规一致）
  - `TestAsk_StepBack`：判定回退 → 回退 + 原问题双检索 → 回答
  - `TestAsk_StrategiesMutualExclusion`：两策略都启用 → 仅 Decomposition
  - `TestAsk_DecompositionFallback`：判定失败 → 常规路径，回答正常
- fakeLLM 需支持按 prompt 特征返回不同 JSON（复用现有 genFunc 模式）

## 模块交互

```
Ask/StreamAsk
  ├─ DecompositionOn?
  │   ├─ 是：tryDecompose
  │   │     ├─ judgeDecompose → 简单 → (nil,false) → 常规 prepare
  │   │     ├─ listSubQuestions → 子问题列表
  │   │     ├─ 逐子问题检索（parallel 并发 / sequential 前序拼入）
  │   │     └─ 综合生成（含各子问题上下文 + 原问题）→ RAGResult
  │   └─ 否（StepBackOn）：
  │       ├─ tryStepBack
  │       │   ├─ judgeStepBack → 否 → 常规
  │       │   ├─ 回退问题检索 + 原问题检索
  │       │   └─ 合并上下文生成 → RAGResult
  └─ 常规 prepare（现状：改写/多查询 → 检索 → 组装 → 生成）
```

## 文件组织

```
internal/config/
├── config.go            — 修改：5 个新字段 + DecompositionOn/StepBackOn + applyDefaults
configs/config.yaml      — 修改：rag 段加 decomposition/step_back 注释示例
internal/rag/
├── engine.go            — 修改：Ask/StreamAsk 入口分支
├── decompose.go         — 新建：tryDecompose / tryStepBack / judge/listSubQuestions / 检索与综合
├── prompt.go            — 修改：4 个新模板 + promptTemplates 字段 + render 函数
├── engine_test.go       — 修改：6 个新用例 + fakeLLM 扩展
internal/api/docs/       — 如接口无变更则不动（策略默认关）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 子问题处理 | 方案 A：只检索 → 一次综合生成 | 成本可控（2 次额外 LLM 调用）；子问题上下文足够综合；引用完整 |
| 互斥 | Decomposition 优先 | 用户确认；避免叠加复杂化与成本失控 |
| 启用 | 独立开关 + LLM 判定 | 用户确认；简单问题零成本跳过 |
| 判定失败 | 静默降级常规路径 | 与阶段一 Multi-Query 降级语义一致 |
| Sequential 参考 | 前序子问题检索上下文拼入后续 prompt | 不生成子答案（成本）；检索上下文提供足够线索 |
| StreamAsk | 判定/检索同步，综合阶段流式 | 保持 SSE 体验，前端无需改动 |
| 子问题上限 | MaxSub 默认 5 | 防止分解爆炸，控制成本 |
| 日志 | 记录判定/子问题/回退问题/耗时 | N4 可观测；供 eval 对比 |
