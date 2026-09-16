# 评测数据集示例（EvalSample 格式，与 internal/eval 共用）

RAGAS 评测服务复用 Go 侧 `internal/eval` 的数据集格式，支持 `.json` 与 `.jsonl` 两种。

## 字段说明

| 字段 | 必填 | 说明 |
|------|------|------|
| question | 是 | 评测问题 |
| answer | 否 | 标准答案（reference）。缺失时 context_recall 记 N/A、context_precision 使用无参考变体 |
| expected_ids | 是 | 期望检索片段 ID 列表（RAGAS 侧降级为 metadata，Recall@K 由 internal/eval 使用） |
| kb_id | 否 | 知识库范围；为空时由任务级 kb_id 兜底，均为空则不限定 |

## JSON 格式（dataset.sample.json）

```json
{
  "name": "示例评测集",
  "samples": [
    {
      "question": "BinRag 支持哪些文档格式？",
      "answer": "支持 PDF、Word、Markdown 等格式。",
      "expected_ids": ["chunk-id-1"]
    },
    {
      "question": "如何配置向量数据库？",
      "expected_ids": ["chunk-id-2"],
      "kb_id": "kb-uuid"
    }
  ]
}
```

## JSONL 格式（dataset.sample.jsonl）

逐行一条样本，首行可为 `{"name": "..."}` 元信息行（与 Go 侧 parseJSONL 行为对齐）。

## 端到端联调数据集要求（checklist 场景 1）

- ≥5 条真实知识库问题（question/answer/expected_ids 按实际入库内容填写）
- 含 1 条无 answer 的样本（验证降级路径）
- 含 1 条指向不存在知识库的样本（验证样本级容错，progress.failed=1）
