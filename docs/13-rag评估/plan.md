# RAG 评估与优化 Plan

## 架构概览

新增 **`internal/eval` 包**（评估核心）+ **`cmd/eval` 命令行入口**，完全独立于 server/desktop，复用现有组件：

```
cmd/eval/main.go        CLI：解析参数 → 加载配置 → 装配 → 运行 → 报告
internal/eval/
├── dataset.go          数据集模型 + JSON/JSONL 加载与校验
├── evaluator.go        评估编排（检索/问答/LLM 指标 三阶段）
├── judge.go            LLM-as-Judge：准确性 + 忠实度评审提示词与调用
├── report.go           报告聚合与输出（文本 + JSON）
└── dataset_test.go     dataset 加载校验测试
internal/app/
└── app.go              抽出 retriever/engine 装配供 eval 复用（不依赖 HTTP）
```

评估链路完全复用现有组件：`retriever.Retriever`（检索）、`rag.Engine`（问答）、`llm.LLM`（评审），不修改其行为。

## 核心数据结构

### EvalSample（数据集单条）
```go
type EvalSample struct {
    Question   string   `json:"question"`             // 问题
    Answer     string   `json:"answer,omitempty"`     // 标准答案（可选，仅问答模式用）
    ExpectedIDs []string `json:"expected_ids"`        // 期望检索片段 ID（Recall@K 判定）
    KBID       string   `json:"kb_id,omitempty"`      // 知识库范围（可选，空=不限定）
}
```

### Dataset（数据集）
```go
type Dataset struct {
    Name    string      `json:"name"`
    Samples []EvalSample `json:"samples"`
}
```
加载：JSON（`{name, samples[]}`）或 JSONL（每行一条 sample）；校验：question 非空、expected_ids 存在（可空数组）、字段类型正确。

### EvalConfig（评测配置）
```go
type EvalConfig struct {
    DatasetPath   string   // 数据集文件路径
    KValues       []int    // Recall@K 的 K 列表，默认 [1,3,5]
    Mode          string   // retrieve / qa / full
    JudgeModel    string   // LLM-as-Judge 使用的模型（默认用配置 llm.model）
    Concurrency   int      // 样本并发数，默认 2
    Temperature   float32  // 评审温度，默认 0.0（确定性优先）
    OutputPath    string   // 报告输出路径（默认 stdout）
}
```

### EvalResult（单样本结果）
```go
type EvalResult struct {
    Sample     EvalSample
    Retrieved  []retriever.RetrieveResult // 检索结果（含 ID/Content）
    Answer     string                     // 问答回答
    Sources    []rag.Source               // 引用来源
    Recall     map[int]bool               // K → 是否命中期望片段
    Accuracy   *float64                   // LLM 准确性评分 0-10（未评= nil）
    Faithful   *bool                      // LLM 忠实判定（未评= nil）
    Error      string                     // 单样本错误（非空=失败）
}
```

## 模块设计

### cmd/eval/main.go
**职责：** CLI 入口，`-c` 指定 config.yaml（与 server 同款），`-d` 数据集路径，`-m` 模式（retrieve/qa/full），`-o` 报告输出。
**流程：** 加载配置 → 调用 app 装配（复用 embedder/vectorstore/bm25/retriever/engine/llm）→ 构建 EvalConfig → 运行 Evaluator → 输出报告。

### internal/eval/dataset.go
**职责：** 数据集加载与校验（F1）。
**对外接口：** `LoadDataset(path string) (*Dataset, error)`、`LoadDatasetJSONL(r io.Reader) (*Dataset, error)`、`Validate(d *Dataset) error`。
**依赖：** 无（纯解析）。

### internal/eval/evaluator.go
**职责：** 评估编排，三阶段（F2/F3/F7）：
- `EvaluateRetrieval(ctx, retriever, samples, kValues, kbFilterFn) []EvalResult` — 逐样本检索，计算 Recall@K
- `EvaluateQA(ctx, engine, samples) []EvalResult` — 逐样本问答，收集回答与来源
- `Run(ctx, cfg, retriever, engine, judgeLLM) (*Report, error)` — 按 Mode 调度：retrieve 只跑检索；qa 只跑问答+来源；full 全链路+LLM 指标
- 并发：`errgroup` 限制 Concurrency；单样本 panic/error 捕获进 EvalResult.Error（F3 容错）

**对外接口：** `Run(ctx, EvalConfig, retriever.Retriever, rag.Engine, llm.LLM) (*Report, error)`
**依赖：** dataset、judge、retriever、rag、llm。

### internal/eval/judge.go
**职责：** LLM-as-Judge 两个指标（F4/F5）：
- `JudgeAccuracy(ctx, llmClient, question, answer, standardAnswer, model) (float64, error)` — 提示词要求按 0-10 打分，解析 `{"score": n}` JSON
- `JudgeFaithfulness(ctx, llmClient, question, answer, sources, model) (bool, error)` — 提示词要求判断回答是否基于引用来源，解析 `{"faithful": true/false}`
- 提示词常量固化（确定性优先：Temperature 0.0）
**依赖：** llm。

### internal/eval/report.go
**职责：** 报告聚合与输出（F6）：
- `ComputeMetrics(results []EvalResult, kValues []int) Report` — 汇总 Recall@K 均值、平均准确性、忠实比例、错误样本数
- `WriteReport(r Report, w io.Writer, format string)` — 文本表格 或 JSON
**依赖：** 无。

### internal/app/app.go 改动
**职责：** 抽出非 HTTP 装配复用。新增 `AssembleEvalDeps(cfg) (retriever, engine, llm, closer, error)`（或导出轻量装配函数），cmd/eval 使用；server 路径不变。

## 模块交互

```
cmd/eval
  │ LoadConfig(-c)
  ▼
app.AssembleEvalDeps(cfg) → retriever / engine / llm / closer
  │
  ▼
eval.Run(EvalConfig, retriever, engine, llm)
  ├── Mode=retrieve: 逐样本 retriever.Search → Recall@K
  ├── Mode=qa:       逐样本 engine.Ask → answer + sources
  └── Mode=full:     上述 + judge.JudgeAccuracy / JudgeFaithfulness
  ▼
report.WriteReport（文本/JSON）
```

## 文件组织

```
cmd/eval/main.go            — CLI 入口
internal/eval/
├── dataset.go              — 数据集加载/校验
├── evaluator.go            — 评估编排
├── judge.go                — LLM-as-Judge
├── report.go               — 报告
├── dataset_test.go         — 数据集测试
└── evaluator_test.go       — 评估编排测试（fake retriever/engine/llm）
internal/app/app.go         — 抽出装配复用函数
configs/                    — 复用 config.yaml（评测用同一配置文件）
```

## 技术决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 评测入口 | cmd/eval 独立 CLI | 与 server 分离，CI 可跑、无需 HTTP；评测非日常操作 |
| 装配复用 | app 包导出轻量装配（不依赖 router/webui） | 避免 cmd/eval 重复连接 Qdrant/Postgres/构造组件；server 路径不变 |
| 数据集格式 | JSON + JSONL 双支持 | JSON 易读、JSONL 适合大批量；字段对齐 spec |
| Recall 判定 | 期望片段 ID ∈ 前 K 检索结果 ID 集合 | 简单可观测；RetrieveResult.ID 与入库 chunk_id 一致 |
| LLM-as-Judge | 复用现有 llm.LLM，低温度（0.0）+ JSON 输出解析 | 评审模型与生成可同源也可独立配置（JudgeModel 覆盖）；确定性优先 |
| 并发 | errgroup + 上限（默认 2） | 样本级并行提速，防打爆 embedding/LLM API |
| 容错 | 单样本错误写入 EvalResult.Error，不中断 | 满足 N3；报告标注错误样本 |
| 报告 | 文本表格（默认）+ JSON（-o json） | 人读与机器消费兼顾 |
| BM25 数据源 | 复用内存 BM25 索引 | 评测依赖已入库数据（Qdrant + 内存 BM25 需同进程重建，沿用 app 装配即可） |
