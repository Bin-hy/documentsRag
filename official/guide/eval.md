---
title: RAG 评估
description: BinRag RAG 评估 — 数据集驱动的 Recall@K 检索评估与 LLM-as-Judge 问答质量评分。
---

# RAG 评估

`cmd/eval` 提供独立的 RAG 评估 CLI，以数据集驱动量化检索与问答质量，为分块 / Embedding / Prompt 调优提供依据。

## 用法

```bash
# 仅检索评估（Recall@K，不调 LLM）
go run ./cmd/eval -c configs/config.local.yaml -d dataset.json -m retrieve

# 全量评估（检索 + 问答 + LLM-as-Judge 准确性与忠实度）
go run ./cmd/eval -c configs/config.local.yaml -d dataset.json -m full -o report.json
```

模式（`-m`）：`retrieve`（仅检索）/ `qa`（检索 + 问答）/ `full`（全量含 LLM 指标），K 值默认 `1,3,5`（`-k` 覆盖）。

## 数据集格式

`.json` 或 `.jsonl`：

```json
{
  "name": "示例评估集",
  "samples": [
    { "question": "产品支持哪些文档格式？", "answer": "TXT/Markdown/PDF 等",
      "expected_ids": ["<chunk_id>"], "kb_id": "<kb_id,可选>" }
  ]
}
```

- `expected_ids`：期望检索命中的 chunk ID（入库时生成的 chunk_id，可从向量库 payload 或文档 `ChunkIDs` 获取），用于 Recall@K 判定
- `answer`：标准答案，`full` 模式下用于 LLM 准确性评分（可选）
