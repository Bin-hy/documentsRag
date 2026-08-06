# RAG 评估与优化 Tasks

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/eval/dataset.go` | 数据集模型 + 加载/校验 |
| 新建 | `internal/eval/report.go` | 报告聚合与输出 |
| 新建 | `internal/eval/judge.go` | LLM-as-Judge（准确性/忠实度） |
| 新建 | `internal/eval/evaluator.go` | 评估编排（三阶段） |
| 新建 | `internal/eval/dataset_test.go` | 数据集加载校验测试 |
| 新建 | `internal/eval/evaluator_test.go` | 评估编排测试（fake 组件） |
| 修改 | `internal/app/app.go` | 抽出非 HTTP 装配复用函数 |
| 新建 | `cmd/eval/main.go` | CLI 入口 |

## T1: eval 包骨架 + 数据集模块

**文件：** `internal/eval/dataset.go`、`internal/eval/dataset_test.go`
**依赖：** 无
**步骤：**
1. 定义 `EvalSample{Question, Answer, ExpectedIDs, KBID}` 与 `Dataset{Name, Samples}`（字段与 plan.md 一致）
2. 实现 `LoadDataset(path string) (*Dataset, error)`：按扩展名分发——`.json` 走 JSON 解析、`.jsonl` 走逐行解析；JSONL 每行一条 sample
3. 实现 `Validate(d *Dataset) error`：samples 非空、每条 question 非空、expected_ids 非 nil（可空数组）
4. 测试：合法 JSON 数据集加载成功；合法 JSONL 加载成功；question 为空报错；samples 为空报错；非法 JSON 报错

**验证：** `go test ./internal/eval/ -run TestDataset -v` 通过

## T2: 报告模块

**文件：** `internal/eval/report.go`
**依赖：** T1（引用 EvalResult/EvalSample）
**步骤：**
1. 定义 `Report` 结构：`DatasetName`、`Mode`、`TotalSamples`、`ErrorCount`、`RecallByK map[int]float64`、`AvgAccuracy *float64`、`FaithfulRatio *float64`、`Results []EvalResult`
2. 实现 `ComputeMetrics(results []EvalResult, kValues []int) Report`：Recall@K = 命中样本数/有效样本数；AvgAccuracy = 非 nil 评分均值；FaithfulRatio = true 占比
3. 实现 `WriteReport(r Report, w io.Writer, format string)`：`text` 输出人类可读表格（含逐样本明细：问题/回答/分数/判定/错误）；`json` 输出完整 JSON

**验证：** `go build ./internal/eval/...` 编译通过；后续在 evaluator_test 中验证指标计算

## T3: LLM-as-Judge 模块

**文件：** `internal/eval/judge.go`
**依赖：** T1（无直接依赖，可并行）
**步骤：**
1. 定义 judge 提示词常量：`accuracyPrompt`（要求输出 `{"score": 0-10}`）、`faithfulnessPrompt`（要求输出 `{"faithful": true/false}`），提示词中固化评分标准
2. 实现 `JudgeAccuracy(ctx, client llm.LLM, question, answer, standardAnswer, model string) (float64, error)`：构造 system+user 消息 → `client.Generate`（Temperature 0.0、MaxTokens 合理值）→ 解析 JSON 取 score
3. 实现 `JudgeFaithfulness(ctx, client llm.LLM, question, answer string, sources []rag.Source, model string) (bool, error)`：sources 拼接进提示词 → 解析 JSON 取 faithful
4. 解析失败返回 error（由 evaluator 记录为单样本错误）

**验证：** `go build ./internal/eval/...` 编译通过；judge 逻辑通过 evaluator_test 的 fake llm 验证

## T4: 评估编排模块

**文件：** `internal/eval/evaluator.go`
**依赖：** T1、T2、T3
**步骤：**
1. 定义 `EvalConfig{DatasetPath, KValues, Mode, JudgeModel, Concurrency, OutputPath}`，KValues 默认 `[1,3,5]`、Mode 默认 `full`、Concurrency 默认 2
2. 实现 `Run(ctx, cfg EvalConfig, rt retriever.Retriever, engine rag.Engine, judgeLLM llm.LLM) (*Report, error)`：
   - 加载数据集 + Validate
   - 按 Mode 分派：`retrieve` 只跑检索；`qa` 跑问答；`full` 检索+问答+LLM 指标
   - 样本级并发：errgroup + Concurrency 上限
   - 单样本错误捕获进 `EvalResult.Error`，不中断整体
3. 实现内部 `evalOneRetrieve(ctx, rt, sample, kValues) EvalResult`（Recall 判定：期望 ID ∈ 前 K 结果 ID）与 `evalOneQA(ctx, engine, sample) EvalResult`
4. 实现 `retrieveMode` / `qaMode` / `fullMode` 三个子流程函数

**验证：** `go build ./internal/eval/...` 编译通过；行为由 T7 的 fake 组件测试验证

## T5: app 装配复用

**文件：** `internal/app/app.go`
**依赖：** 无（与 T1-T4 并行）
**步骤：**
1. 新增 `AssembleEvalDeps(cfg *config.Config) (EvalDeps, error)`，其中 `EvalDeps{Retriever retriever.Retriever; Engine rag.Engine; LLM llm.LLM; Closer func() error}`
2. 复用现有装配逻辑：连接 Postgres（可选，仅需要时）→ embedder → vectorstore → bm25 → pipeline 不启动 worker → retriever → engine → llm
3. 注意：评测不需要 worker 与 HTTP，不启动 task worker、不构建 router；Closer 关闭 vs/store
4. 保持 `New` 函数路径不变（内部共享装配细节，避免大改）

**验证：** `go build ./...` 编译通过；`go test ./internal/app/...`（如有）通过

## T6: cmd/eval CLI

**文件：** `cmd/eval/main.go`
**依赖：** T1-T5
**步骤：**
1. 解析 flag：`-c`（配置文件，默认 configs/config.yaml）、`-d`（数据集路径，必填）、`-m`（模式 retrieve/qa/full）、`-k`（K 值逗号分隔）、`-o`（输出路径，默认 stdout）、`-j`（评审模型覆盖）
2. `config.LoadConfig` → `app.AssembleEvalDeps`（defer Closer）→ 构建 EvalConfig → `eval.Run` → `report.WriteReport`
3. 输出模式由 `-o` 扩展名决定：`.json` 输出 JSON，否则文本

**验证：** `go build ./cmd/eval/...` 编译通过；`go run ./cmd/eval -h` 显示参数说明

## T7: 评估编排测试

**文件：** `internal/eval/evaluator_test.go`
**依赖：** T4
**步骤：**
1. fake retriever：`Search` 返回固定结果（含 sample.ExpectedIDs 中一个、及干扰项），可配置错误
2. fake engine：`Ask` 返回固定回答与来源，可配置错误；`StreamAsk` 返回固定事件流
3. fake llm：`Generate` 返回可配置的 `{"score": N}` / `{"faithful": true/false}` 或错误
4. 用例：retrieve 模式 Recall 计算正确（命中/未命中）；qa 模式收集回答与来源；full 模式 Accuracy/Faithful 填充；单样本错误不中断（某样本 fake 报错，Report.ErrorCount 正确）；并发上限生效（Concurrency=1 时顺序执行）

**验证：** `go test ./internal/eval/ -v` 全部通过

## T8: 全量验证

**文件：** 无新增
**依赖：** T1-T7
**步骤：**
1. `go build ./...` 全项目编译
2. `go test ./...` 全部测试（含新 eval 包）
3. `go vet ./...` 无告警
4. 手工冒烟：构造一个 2 条样本的示例数据集，`go run ./cmd/eval -d <dataset> -m retrieve` 输出含 Recall@1/3/5

**验证：** 上述命令全部通过；冒烟输出包含三个 K 值

## 执行顺序

```
T1 → T2 ─┐
T3 ──────┼→ T4 → T7 → T8
T5（并行）┘
T6（依赖 T1-T5，可与 T4 并行写骨架）
```

T1/T3/T5 可并行（不同文件互不依赖）；T2 依赖 T1；T4 依赖 T1-T3；T7 依赖 T4；T6 依赖全部组件；T8 收尾。
