# RAG 评估与优化 Checklist

> 每一项通过运行代码或观察行为来验证，聚焦系统行为。

## 实现完整性
- [ ] 数据集模块已实现且可加载校验（验证：`go test ./internal/eval/ -run TestDataset -v` 通过）
- [ ] 报告模块已实现（验证：`go build ./internal/eval/...` 编译通过）
- [ ] LLM-as-Judge 已实现（验证：`go build ./internal/eval/...` 编译通过）
- [ ] 评估编排已实现（验证：`go test ./internal/eval/ -v` 通过）
- [ ] cmd/eval CLI 可运行（验证：`go run ./cmd/eval -h` 显示参数）
- [ ] app 装配复用函数存在（验证：`go build ./...` 编译通过）

## 数据集行为
- [ ] 合法 JSON 数据集加载成功（验证：dataset_test 用例）
- [ ] 合法 JSONL 数据集加载成功（验证：dataset_test 用例）
- [ ] 非法数据集（空 question / 空 samples / 非法 JSON）报错（验证：dataset_test 用例）

## 评估行为（fake 组件测试）
- [ ] retrieve 模式输出 Recall@K（命中/未命中判定正确）（验证：evaluator_test 用例 1）
- [ ] qa 模式收集回答与引用来源（验证：evaluator_test 用例 2）
- [ ] full 模式 Accuracy 评分正确填充（验证：evaluator_test 用例 3）
- [ ] full 模式 Faithful 判定正确填充（验证：evaluator_test 用例 3）
- [ ] 单样本失败不中断整体，ErrorCount 正确（验证：evaluator_test 用例 4）
- [ ] 并发上限生效（Concurrency=1 顺序执行）（验证：evaluator_test 用例 5）

## 集成
- [ ] 评测复用现有组件（retriever/engine/llm），无重复实现（验证：代码走 app 装配，无独立实现）
- [ ] server 装配路径不受影响（验证：`go test ./internal/api/...` 通过）

## 编译与测试
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 全部通过（含新 eval 包）
- [ ] `go vet ./...` 无告警

## 端到端场景
- [ ] 场景 1（检索冒烟）：2 条样本示例数据集 → `go run ./cmd/eval -d <ds> -m retrieve` → 输出含 Recall@1/3/5 三值（验证：命令输出）
- [ ] 场景 2（全量冒烟，需真实环境）：数据集含标准答案 → `-m full` → 输出回答明细 + 平均准确性 + 忠实比例（验证：命令输出，需 Qdrant/Postgres/LLM 环境）
- [ ] 场景 3（容错端到端）：数据集含一条期望片段不存在的样本 → 评测完成且报告标记错误样本（验证：命令输出 ErrorCount ≥ 1）

## Spec 验收标准映射
- [ ] AC1（数据集定义与加载/拒绝）→ 数据集行为
- [ ] AC2（检索评估 Recall@1/3/5）→ 评估行为第 1 项 + 端到端场景 1
- [ ] AC3（问答评估输出回答与来源）→ 评估行为第 2 项 + 端到端场景 2
- [ ] AC4（准确性指标）→ 评估行为第 3 项
- [ ] AC5（忠实度指标）→ 评估行为第 4 项
- [ ] AC6（分项运行不调 LLM）→ retrieve 模式无 LLM 调用（evaluator_test 断言 + 端到端场景 1）
- [ ] AC7（容错）→ 评估行为第 5 项 + 端到端场景 3
- [ ] AC8（build/test/vet）→ 编译与测试
