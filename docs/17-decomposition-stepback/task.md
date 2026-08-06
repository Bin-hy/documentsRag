# Decomposition 问题分解 + Step-Back 回退 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/config/config.go` | 5 新字段 + DecompositionOn/StepBackOn + applyDefaults |
| 修改 | `configs/config.yaml` | rag 段加 decomposition/step_back 注释示例 |
| 修改 | `internal/rag/prompt.go` | 4 个新模板 + promptTemplates 字段 + render 函数 |
| 新建 | `internal/rag/decompose.go` | tryDecompose / tryStepBack / 判定 / 检索 / 综合 |
| 修改 | `internal/rag/engine.go` | Ask/StreamAsk 入口分支 |
| 修改 | `internal/rag/engine_test.go` | 6 个新用例 + fakeLLM 扩展 |

## T1: config 配置项

**文件：** `internal/config/config.go`、`configs/config.yaml`
**依赖：** 无
**步骤：**
1. `RAGConfig` 增加字段：
   - `DecompositionEnabled *bool \`yaml:"decomposition_enabled"\``
   - `DecompositionMode string \`yaml:"decomposition_mode"\``（parallel/sequential）
   - `DecompositionMaxSub int \`yaml:"decomposition_max_sub"\``
   - `StepBackEnabled *bool \`yaml:"step_back_enabled"\``
   - `DecompositionTemplatePath`、`StepBackTemplatePath`（string）
2. 新增方法 `DecompositionOn()`、`StepBackOn()`（nil 视为关闭）
3. `applyDefaults`：Mode 默认 "parallel"、MaxSub 默认 5
4. config.yaml rag 段加注释示例（decomposition_enabled / step_back_enabled 默认 false）

**验证：** `go build ./internal/config/...` 通过

## T2: prompt 模板

**文件：** `internal/rag/prompt.go`
**依赖：** T1
**步骤：**
1. 新增 4 个模板常量：
   - `defaultDecomposeJudgeTemplate`：判定是否分解，输出 JSON `{"decompose": true/false, "reason": "..."}`
   - `defaultDecomposeListTemplate`：生成子问题列表，输出 JSON 数组
   - `defaultStepBackJudgeTemplate`：判定是否回退，输出 JSON `{"step_back": true/false, "question": "回退问题"}`
2. `promptTemplates` 增加 `decomposeJudge`、`decomposeList`、`stepBackJudge` 字段；`loadPromptTemplates` 加载
3. 新增 render 函数：`renderDecomposeJudge(question)`、`renderDecomposeList(question, maxSub)`、`renderStepBackJudge(question)`

**验证：** `go build ./internal/rag/...` 通过

## T3: 判定与子问题生成（decompose.go 前半）

**文件：** `internal/rag/decompose.go`
**依赖：** T2
**步骤：**
1. 定义 `decomposeResult{ShouldDecompose bool; SubQuestions []string}`、`stepBackResult{ShouldStepBack bool; StepBackQuery string}`
2. `judgeDecompose(ctx, question) (bool, error)`：调 LLM → 解析 `{decompose}` JSON → 返回
3. `listSubQuestions(ctx, question) ([]string, error)`：调 LLM → 解析 JSON 数组 → 去重/限长（MaxSub）
4. `judgeStepBack(ctx, question) (stepBackResult, error)`：调 LLM → 解析 `{step_back, question}` JSON
5. JSON 解析失败返回 error（外层降级）

**验证：** `go build ./internal/rag/...` 通过

## T4: 检索与综合（decompose.go 后半）

**文件：** `internal/rag/decompose.go`
**依赖：** T3
**步骤：**
1. `tryDecompose(ctx, sessionID, question, o) (*RAGResult, bool, error)`：
   - `judgeDecompose` false → 返回 (nil, false, nil)
   - `listSubQuestions` → 子问题列表
   - 逐子问题检索（parallel：errgroup 并发；sequential：前序子问题检索上下文拼入后续 prompt 作参考）——每子问题调 `retriever.Search` 或 `SearchMulti`（若 Multi-Query 启用）
   - 综合：各子问题上下文 + 原问题拼 user 消息 → `llm.Generate` 一次生成
   - 落历史，返回 `RAGResult{Answer, Sources=所有子问题检索结果}`
2. `tryStepBack(ctx, sessionID, question, o) (*RAGResult, bool, error)`：
   - `judgeStepBack` → `{step_back: false}` → (nil, false, nil)
   - 回退问题检索（`Search` 或 `SearchMulti`）+ 原问题检索
   - 合并上下文注入生成
   - 落历史，返回 `RAGResult{Answer, Sources=回退+原问题结果}`
3. 任一环节错误 → 返回 (nil, false, err)（外层静默降级）
4. 日志：判定结果、子问题列表/回退问题、各阶段耗时

**验证：** `go build ./internal/rag/...` 通过

## T5: engine 入口分支

**文件：** `internal/rag/engine.go`
**依赖：** T4
**步骤：**
1. `Ask` 开头（prepare 之前）：
   ```go
   if e.cfg.DecompositionOn() {
       if res, ok, err := e.tryDecompose(ctx, sessionID, question, opts...); err == nil && ok {
           return res, nil
       } // 失败/不适用 → 落回常规
   } else if e.cfg.StepBackOn() {
       if res, ok, err := e.tryStepBack(ctx, sessionID, question, opts...); err == nil && ok {
           return res, nil
       }
   }
   ```
2. `StreamAsk` 同理：分解/回退综合阶段用 `StreamGenerate` 流式输出；前置判定/检索同步
   - 简化：`StreamAsk` 中若 `tryDecompose`/`tryStepBack` 返回 `ok=true`，则把综合生成改为流式（判定/检索已完成，仅综合消息流式）
3. 注意互斥：`DecompositionOn()` 分支 else if `StepBackOn()`（Decomposition 优先）

**验证：** `go build ./internal/rag/...` 通过

## T6: 测试

**文件：** `internal/rag/engine_test.go`
**依赖：** T5
**步骤：**
1. 扩展 fakeLLM：按 user 消息内容特征返回不同 JSON（含「是否分解」「子问题」「回退」提示特征）
2. 新增用例：
   - `TestAsk_DecompositionParallel`：判定复杂 → 子问题并发检索 → 综合生成，Sources 来自各子问题
   - `TestAsk_DecompositionSequential`：前序子问题上下文拼入后续（断言检索顺序）
   - `TestAsk_DecompositionSkip`：判定不分解 → 常规路径（检索数与常规一致）
   - `TestAsk_StepBack`：判定回退 → 回退 + 原问题双检索 → 回答
   - `TestAsk_StrategiesMutualExclusion`：两策略都启用 → 仅 Decomposition（StepBack 判定未调用）
   - `TestAsk_DecompositionFallback`：判定失败 → 常规路径，回答正常
3. 现有测试不回归（默认策略关，行为不变）

**验证：** `go test ./internal/rag/ -v` 全部通过

## T7: 全量验证 + 冒烟

**文件：** 无新增
**依赖：** T1-T6
**步骤：**
1. `go build ./...` + `go vet ./...` + `go test ./...` 全绿
2. 临时配置开启 `decomposition_enabled: true`，启动服务，用复合问题问答观察日志（判定/子问题/综合）
3. 冒烟后恢复默认配置

**验证：** 上述命令全绿；冒烟日志符合预期

## 执行顺序

```
T1 → T2 → T3 → T4 → T5 → T6 → T7
```

T1-T5 串行（config → 模板 → 判定 → 检索综合 → 入口）；T6 依赖 T5；T7 收尾。
