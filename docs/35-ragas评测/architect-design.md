# RAGAS 评测系统 —— 后端架构设计

> 文档版本：v1.0
> 关联文档：`docs/35-ragas评测/prompt-design.md`（四指标中文化与评审模型策略）、`docs/35-ragas评测/frontend-design.md`（前端页面与 API 层）、`docs/13-rag评估/spec.md`（现有 internal/eval）
> 关联代码：`internal/api/router.go`、`internal/api/middleware.go`（Auth）、`internal/api/handler_chat.go`、`internal/rag/engine.go`（RAGResult）、`internal/eval/`、`frontend/src/api/client.ts`
> 范围：纯架构设计，不含实现代码。

---

## 0. 设计目标与约束

| 项 | 内容 |
|---|---|
| 目标 | 引入独立 Python 微服务封装 RAGAS，提供生成侧四指标（faithfulness / answer_relevancy / context_precision / context_recall）的异步评测能力，前端可提交任务、轮询进度、查看报告与多次对比 |
| 互补关系 | `internal/eval`（Go，CLI）保留 Recall@K 检索指标与数据集格式（`EvalSample`），RAGAS 服务负责生成侧指标。两侧共用同一数据集 JSON/JSONL 格式，不互相依赖 |
| 硬约束 1 | 前端凭据、统一响应包装 `{code,message,data}`、401 跳登录等既有约定必须延续（`frontend/src/api/client.ts`） |
| 硬约束 2 | RAGAS 需要上下文正文，而 `rag.Source` 无 `content` 字段 → 采用 prompt-design §4.2 方案 A：问答接口加 `include_contexts` 评测专用参数 |
| 硬约束 3 | 评测为长任务（数十样本 × 每样本多次 LLM 调用），必须异步化：提交 → 轮询 → 报告 |

---

## 1. 整体架构

### 1.1 组件图与数据流

```
┌────────────────────────────────────────────────────────────────────┐
│ 前端（Vue 3 + Element Plus，frontend/）                              │
│  EvalListView / EvalNewView / EvalDetailView / EvalCompareView      │
│  api/eval.ts ── request<T>()（Bearer JWT/API Key，{code,msg,data}） │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ HTTPS（同源，/api/v1/eval/*）
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ Go 后端（Gin，binrag-server，:8085）                                 │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ Auth 中间件（现有：JWT 本地验签 / API Key SHA-256 查库）        │  │
│  │ Eval 反向代理组 /api/v1/eval/* ──────────────────────┐        │  │
│  │ （鉴权 + 注入 X-Eval-Internal-Token + 透传，不改包装）  │        │  │
│  └──────────────────────────────────────────────────────┼───────┘  │
│  ▲                                                       │          │
│  │ ③ 回调：POST /api/v1/chat?include_contexts=true       │          │
│  │    （评测专用 API Key，RAGResult.Sources 附 content）   │          │
└──┼───────────────────────────────────────────────────────┼──────────┘
   │                                                       │ HTTP（内网）
   │                                                       ▼
   │              ┌─────────────────────────────────────────────────┐
   │              │ ragas-eval 评测微服务（Python / uv / FastAPI）     │
   │              │  :8090（不暴露宿主端口，仅 compose 内网可达）       │
   │              │                                                  │
   │              │  ┌────────────┐  ┌──────────────┐  ┌──────────┐ │
   │              │  │ API 层      │→ │ 任务队列       │→ │ 数据采集器│─┘
   │              │  │ (routers)  │  │ (asyncio     │  │ (BinRag  │  ③
   │              │  │            │  │  worker pool)│  │  Client) │──┘
   │              │  └────────────┘  └──────┬───────┘  └──────────┘
   │              │       │                 ▼
   │              │       │          ┌──────────────┐   ④ Judge LLM
   │              │       │          │ RAGAS 执行器   │──────────────▶ OpenAI 兼容端点
   │              │       │          │ (中文 Prompt, │   (temperature=0)
   │              │       │          │  四指标分派)   │──────────────▶ Embedding 端点
   │              │       ▼          └──────────────┘   （复用项目配置）
   │              │  ┌────────────┐
   │              │  │ 报告存储     │  SQLite（WAL）+ 样本明细 JSON
   │              │  │ (SQLite)   │  卷挂载持久化
   │              │  └────────────┘
   │              └─────────────────────────────────────────────────┘
   │
   │ 并行存在（不动）：internal/eval CLI ── 直接走 retriever/rag.Engine 进程内调用
   │                   产出 Recall@K 检索指标（数据集格式与 RAGAS 侧共用）
```

**四条核心数据流**：

1. **控制流（①）**：前端 → Go（鉴权）→ Python（任务 CRUD / 进度 / 报告）。Go 是纯反向代理，不解包不重组响应。
2. **采集流（③）**：评测任务进入 worker 后，数据采集器携带**评测专用 API Key** 回调 Go `POST /api/v1/chat?include_contexts=true`，逐样本获得 `answer + sources(含 content)`。
3. **评测流（④）**：RAGAS 执行器将样本映射为 `SingleTurnSample`（映射表见 prompt-design §4.1），调用 Judge LLM / Embedding（OpenAI 兼容，复用项目配置）计算指标。
4. **持久化流**：任务状态、样本采集结果、指标分数、汇总报告全部落 SQLite；前端轮询与报告读取只打存储，不打执行器。

### 1.2 关键决策：前端经 Go 后端代理，不直连 Python 服务

**结论：前端只与 Go 后端通信，Go 以反向代理转发 `/api/v1/eval/*` 到 ragas-eval。**（与 frontend-design §3.1 的既定端点形态一致，本文将其固化为架构决策。）

| 维度 | 经 Go 代理（选） | 前端直连 Python（弃） |
|---|---|---|
| 鉴权 | 复用现有 Auth 中间件（JWT + API Key 双通道），零新增认证面 | Python 侧需重新实现 JWT 验签（依赖 Go 的签名密钥）或 API Key 查库（直连 Postgres），双份鉴权逻辑必漂移 |
| 前端一致性 | 同源、同 `client.ts` 实例、同 `{code,message,data}` 包装、401 统一跳登录 | 需第二套 axios 实例、第二套凭据与错误处理、CORS 配置 |
| 网络暴露面 | Python 服务不发布宿主端口，仅 compose 内网可达 | 必须对公网/宿主暴露 8090，攻击面扩大 |
| 部署 | 前端只认一个入口（8085），与现状一致 | 前端需感知第二个 baseURL，环境差异化配置变复杂 |
| 代价 | Go 侧加一个轻量反向代理 handler（`httputil.ReverseProxy`，~80 行）；代理层成为评测流量的单点（可接受：控制流流量极小，重的采集流不经代理） | — |

代理路径规则：`/api/v1/eval/*` 原样转发（路径不变、查询串不变、Body 流式透传），Python 服务与 Go 暴露**完全相同的路径与响应包装**，代理因此无需任何报文改写。仅做两件事：① 前置 Auth 中间件；② 注入 `X-Eval-Internal-Token` 头供 Python 校验（见 §2.5）。

---

## 2. HTTP 接口契约（Python 服务 ↔ Go 代理 ↔ 前端）

### 2.1 通用约定

| 约定 | 内容 |
|---|---|
| 路径前缀 | `/api/v1/eval`，与 Go 侧代理规则一致，Python 内部不做二次前缀 |
| 命名风格 | 端点与 JSON 字段全部 **snake_case**（Python 侧惯例；frontend-design §0 已认可此先例，types.ts 中需标注） |
| 响应包装 | 与 BinRag 完全一致：`{"code": 0, "message": "ok", "data": ...}`，成功 code=0；失败 code 取 HTTP 状态语义码（400/401/404/409/422/429/500/503），HTTP 状态码与 code 保持一致 |
| 分页 | `page`（1 起）/ `page_size`（默认 20，上限 100）；列表响应 `data: {items: [...], total: n, page, page_size}` |
| ID | 全部使用 UUID v4 字符串 |
| 时间 | RFC3339 / ISO8601 带时区（`2026-02-01T08:00:00+08:00`） |
| 幂等 | `POST /tasks` 接受可选 `Idempotency-Key` 头；24h 内同 key 返回同一任务（防前端双击/重试重复提交，评测任务 LLM 成本高，必须防重） |
| 关联 ID | 所有响应回显 `X-Request-ID`；任务全生命周期日志携带 `task_id` |

### 2.2 端点总表

| # | 方法 | 路径 | 说明 |
|---|---|---|---|
| 1 | GET | `/api/v1/eval/health` | 健康检查（Go 代理豁免鉴权或走 bootstrap key，见 2.5） |
| 2 | GET | `/api/v1/eval/datasets` | 数据集列表 |
| 3 | POST | `/api/v1/eval/datasets` | 上传数据集（multipart） |
| 4 | GET | `/api/v1/eval/datasets/{id}/preview` | 预览 + 校验结果 |
| 5 | DELETE | `/api/v1/eval/datasets/{id}` | 删除数据集（被任务引用时 409） |
| 6 | GET | `/api/v1/eval/judge-models` | 可用评审模型清单（服务端配置驱动） |
| 7 | POST | `/api/v1/eval/tasks` | 提交评测任务 |
| 8 | GET | `/api/v1/eval/tasks` | 任务列表（`?status=&kb_id=&page=&page_size=`） |
| 9 | GET | `/api/v1/eval/tasks/{id}` | 任务详情/进度（轮询打这个） |
| 10 | POST | `/api/v1/eval/tasks/{id}/cancel` | 取消任务 |
| 11 | DELETE | `/api/v1/eval/tasks/{id}` | 删除任务及报告 |
| 12 | GET | `/api/v1/eval/tasks/{id}/report` | 汇总报告 + 明细分页 |
| 13 | GET | `/api/v1/eval/tasks/{id}/report/samples/{sample_id}` | 单样本完整内容（下钻） |
| 14 | GET | `/api/v1/eval/reports?task_ids=a,b,c` | 多任务汇总批量查询（对比视图） |

### 2.3 各端点契约

#### （1）健康检查 `GET /api/v1/eval/health`

响应 `data`：

```json
{
  "status": "ok",                       // ok | degraded（依赖不可用）
  "version": "0.1.0",
  "checks": {
    "db": "ok",                         // SQLite 可写
    "binrag_api": "ok",                 // 回调 Go /api/v1/knowledge-bases 探活（带评测 Key）
    "judge_llm": "ok",                  // 评审模型端点连通性（缓存 60s，不打满）
    "queue": { "running": 1, "queued": 3 }
  }
}
```

- `degraded` 时 HTTP 200 + `status:"degraded"`（探活语义留给 compose healthcheck 的 `/healthz` 裸端点：db 挂才 503）。

#### （2）数据集列表 `GET /api/v1/eval/datasets`

响应 `data.items[]`（DatasetView）：

```json
{
  "id": "uuid",
  "name": "客服知识库评测集-v3",
  "sample_count": 56,
  "with_reference_count": 40,           // 含标准答案的样本数（决定 context_recall 覆盖率）
  "source_format": "jsonl",             // json | jsonl
  "content_hash": "sha256:...",         // 数据集内容哈希（报告可复现/对比用）
  "created_at": "2026-02-01T08:00:00+08:00"
}
```

#### （3）上传数据集 `POST /api/v1/eval/datasets`

- 请求：`multipart/form-data`，字段 `file`（.json/.jsonl，≤ 10MB）、`name`（可选，默认文件名）。
- 数据集格式复用 `internal/eval` 的 `EvalSample`：`{name, samples: [{question, answer?, expected_ids, kb_id?}]}` 或逐行 JSONL；校验规则与 `dataset.go Validate` 对齐（question 非空、expected_ids 非 nil）。
- 响应：DatasetView + `validation: {warnings: [...]}`（如"12 条样本缺 reference，context_recall 将跳过"）。
- 错误：`422` 格式非法（`message` 含首条错误行号，对齐 Go 侧报错风格）。

#### （4）数据集预览 `GET /api/v1/eval/datasets/{id}/preview?limit=5`

```json
{
  "dataset": { "...DatasetView" },
  "field_stats": { "with_reference": 40, "with_expected_ids": 56, "with_kb_id": 30 },
  "samples": [ { "question": "...", "answer": "...", "expected_ids": ["..."], "kb_id": "..." } ]
}
```

#### （6）评审模型清单 `GET /api/v1/eval/judge-models`

```json
{ "items": [ { "id": "qwen-plus", "label": "Qwen Plus（推荐）", "is_default": true, "supports_structured_output": true } ] }
```

清单来自服务端配置（环境变量/配置文件），不在前端硬编码；服务端启动时校验 default 模型连通性。

#### （7）提交评测任务 `POST /api/v1/eval/tasks`

请求体：

```json
{
  "name": "v3 数据集基线评测",            // 可选，默认自动生成
  "dataset_id": "uuid",                  // 必填
  "kb_id": "uuid",                       // 可选；为空时逐样本用其自带 kb_id，仍为空则不限定（对齐 Go resolveKBScope 语义）
  "judge_model": "qwen-plus",            // 可选，默认服务端 default
  "metrics": ["faithfulness", "answer_relevancy", "context_precision", "context_recall"],  // 可选，默认全量
  "sample_concurrency": 4,               // 可选，样本级并发上限（默认 4，上限见 §4.3）
  "strategy": null                       // 可选，透传 Go chat 的 strategy 覆盖（评测特定检索策略时用）
}
```

响应 `201` + TaskView（任务骨架，`status: "pending"`）。幂等：带相同 `Idempotency-Key` 的重复请求返回首个任务（HTTP 200 而非 201）。

#### （9）任务详情/进度 `GET /api/v1/eval/tasks/{id}`

响应 `data`（TaskDetail，轮询主接口，必须轻量——不含样本明细）：

```json
{
  "id": "uuid",
  "name": "v3 数据集基线评测",
  "status": "evaluating",                // pending|collecting|evaluating|completed|failed|canceled
  "stage": "evaluating",                 // 展示用当前阶段（与 status 同义，预留子阶段）
  "dataset_id": "uuid", "kb_id": "uuid", "judge_model": "qwen-plus", "metrics": ["..."],
  "progress": {
    "total": 56,
    "collected": 56,                     // 采集完成数
    "evaluated": 31,                     // 评测完成数
    "failed": 1                          // 失败样本数（不阻断整体）
  },
  "error_message": "",                   // 任务级失败原因
  "created_at": "...", "started_at": "...", "finished_at": null,
  "report_ready": false
}
```

轮询约定：前端 3s 起、运行中指数退避至 10s 上限（frontend-design §4.2）；该接口目标 p95 < 50ms（纯 SQLite 读）。

#### （10）取消 `POST /api/v1/eval/tasks/{id}/cancel`

- 仅 `pending/collecting/evaluating` 可取消，其余 `409`。
- 协作式取消：在样本边界生效（进行中的单次 LLM 调用允许跑完或随 httpx 超时中断，见 §4.4）。响应 TaskView（`status: "canceled"`，`finished_at` 落时间）。

#### （12）报告 `GET /api/v1/eval/tasks/{id}/report`

查询参数：`page` / `page_size` / `metric_lt=faithfulness:0.6`（低分样本过滤）/ `sort=faithfulness:asc`。
任务未完成返回 `409`（`message: "任务尚未完成"`）。响应 `data`：

```json
{
  "task": { "...TaskDetail" },
  "summary": {
    "faithfulness":       { "mean": 0.87, "coverage": 1.0,  "valid_samples": 56 },
    "answer_relevancy":   { "mean": 0.91, "coverage": 1.0,  "valid_samples": 56 },
    "context_precision":  { "mean": 0.74, "coverage": 1.0,  "valid_samples": 56, "degraded": false },
    "context_recall":     { "mean": 0.68, "coverage": 0.71, "valid_samples": 40, "note": "16 条样本缺 reference，记 N/A" }
  },
  "config_snapshot": {                    // 可复现与公平对比的前提
    "dataset_hash": "sha256:...", "judge_model": "qwen-plus",
    "binrag_config_hash": "sha256:...",   // 提交任务时抓取的 Go /api/v1/config 快照哈希
    "ragas_version": "0.2.x", "prompt_version": "zh-v1"
  },
  "samples": {                            // 明细分页（不含 contexts 正文，下钻接口才给）
    "items": [ { "sample_id": "uuid", "question": "...", "answer_excerpt": "...",
                 "scores": { "faithfulness": 0.83, "answer_relevancy": 0.9, "context_precision": 0.5, "context_recall": null },
                 "status": "ok", "error": "" } ],
    "total": 56, "page": 1, "page_size": 20
  }
}
```

`context_precision` 在 reference 缺失样本上使用无参考变体时，逐样本 `scores` 不变但在 `summary` 标注降级口径（prompt-design §4.3）。

#### （13）单样本下钻 `GET .../report/samples/{sample_id}`

```json
{
  "sample_id": "uuid", "question": "...", "reference": "...",
  "answer": "完整回答",
  "contexts": [ { "id": "chunk-id", "filename": "...", "heading": "...", "score": 0.83, "content": "片段正文" } ],
  "scores": { "...": "含 judge 原始理由 rationale（如 ragas 提供）" },
  "status": "ok", "error": ""
}
```

#### （14）批量汇总 `GET /api/v1/eval/reports?task_ids=a,b,c`（≤ 8 个）

```json
{ "items": [ { "task_id": "uuid", "name": "...", "finished_at": "...",
               "summary": { "...同上单任务 summary" },
               "config_snapshot": { "..." } } ] }
```

对比视图前端拿此接口渲染雷达图/表格；是否可对比的提示（数据集 hash 不一致、judge 模型不一致）由前端根据 `config_snapshot` 给出警告条（frontend-design §5.3）。

### 2.4 错误码约定

复用 BinRag 语义，code 与 HTTP 状态一致：

| code | 场景 |
|---|---|
| 400 | 参数缺失/非法（如 metrics 含未知指标名） |
| 401 | 缺少或无效的 `X-Eval-Internal-Token`（正常不应到达前端；Go 代理自身的 401 走既有逻辑） |
| 404 | 任务/数据集/样本不存在 |
| 409 | 状态冲突：取消已完成任务、删除被引用数据集、报告未就绪 |
| 422 | 数据集格式校验失败（message 带行号/字段） |
| 429 | 队列已满（`queued >= max_queue_size`）拒绝新任务 |
| 500 | 未捕获内部错误（日志带 task_id + request_id） |
| 503 | 依赖不可用：BinRag API 探活失败 / Judge LLM 不可达（提交任务前预检失败时返回） |

单样本级失败**不产生** HTTP 错误：计入 `progress.failed` 与样本 `error` 字段（对齐 internal/eval N3 容错语义）。

### 2.5 鉴权方案（三层，全部复用既有设施）

```
前端 ──Bearer JWT/API Key──▶ Go Auth 中间件（现有，零改动）
Go 代理 ──X-Eval-Internal-Token──▶ Python 校验中间件（共享密钥，env 双侧配置）
Python 采集器 ──Bearer 评测专用 API Key──▶ Go /api/v1/chat（走既有 API Key 查库流程）
```

1. **用户面**：完全复用 Go `Auth` 中间件（JWT 本地验签 / API Key SHA-256 查库，`internal/api/middleware.go`）。评测端点挂在代理组上即自动获得同等保护；`eval/health` 单独豁免（或仅 bootstrap key 可见详细 checks）。
2. **服务面（Go→Python）**：共享内部令牌。Python 侧 FastAPI 中间件校验 `X-Eval-Internal-Token`，缺失/错误一律 401。配合网络层隔离（compose 内网不发布端口），构成纵深防御——即使令牌泄露，公网也不可达。
3. **回调面（Python→Go）**：在 BinRag 中创建一个名为 `ragas-eval` 的**专用 API Key**（系统级，非 bootstrap），经环境变量注入 Python 服务。好处：采集流量在 Go 侧审计日志中可独立识别、可独立吊销、限流桶独立；不借用用户凭据，任务生命周期与用户会话解耦。
4. **数据权限语义**：评测任务的 kb 访问权在**提交时**校验一次（代理层把用户身份透传为 `X-Eval-User-Id`？——不需要：采集统一用评测专用系统 Key，其语义等价"系统级不限定"，与 API Key 未指定 kb_id 的现状一致）。多租户隔离的收口：提交任务时 Go 代理对 `kb_id` 调用 `ensureKBAccess` 做越权校验（越权 404），通过后才转发；Python 信任已校验的请求。**这是代理层除转发外唯一的业务逻辑。**

---

## 3. Python 服务内部模块划分

```
ragas-eval/
├── pyproject.toml                  # uv 管理（见 §5）
├── src/ragas_eval/
│   ├── main.py                     # FastAPI 装配：中间件、路由、启动/关停钩子
│   ├── config.py                   # pydantic-settings：env/文件配置（端口、Go 地址、Key、并发、LLM）
│   ├── api/
│   │   ├── deps.py                 # 依赖注入（repo / queue / collector 单例）
│   │   ├── middleware.py           # 内部令牌校验、request_id、异常→{code,message} 统一包装
│   │   ├── routes_datasets.py      # 数据集 CRUD + 预览
│   │   ├── routes_tasks.py         # 任务提交/查询/取消/删除
│   │   └── routes_reports.py       # 报告/样本下钻/批量对比
│   ├── core/
│   │   ├── collector.py            # 数据采集器：BinRagClient（httpx.AsyncClient），
│   │   │                           #   调 /api/v1/chat?include_contexts=true，
│   │   │                           #   超时/重试（tenacity 指数退避，仅对网络错误与 5xx）、样本级容错
│   │   ├── runner.py               # RAGAS 执行器：SingleTurnSample 映射、指标分派
│   │   │                           #   （reference 缺失降级表，prompt-design §4.3）、
│   │   │                           #   ragas.evaluate 批量调用与逐样本分数落库
│   │   ├── queue.py                # 任务队列：asyncio.Queue + worker pool（见 §4）
│   │   ├── lifecycle.py            # 状态机：pending→collecting→evaluating→completed/failed/canceled
│   │   └── prompts.py              # 中文 Prompt 适配（落地 prompt-design §1 的四套提示词，prompt_version 常量）
│   ├── store/
│   │   ├── db.py                   # aiosqlite 连接管理、WAL、迁移（启动时执行 schema 版本化 DDL）
│   │   ├── models.py               # Dataset/Task/Sample/Report dataclass
│   │   └── repo.py                 # 仓储层：全部 SQL 收口于此，API/worker 不写裸 SQL
│   └── llm/
│       ├── factory.py              # ragas LLM/Embedding 适配（OpenAI 兼容，temperature=0）
│       └── ratelimit.py            # Judge LLM RPM 限流（令牌桶，防打爆供应商配额）
└── tests/                          # pytest + pytest-asyncio + respx（mock Go API 与 LLM）
```

模块职责要点：

- **collector** 是唯一与 Go 后端通信的模块；**llm/factory** 是唯一与模型供应商通信的模块。两者都禁止被 API 层直接调用，只经 worker 使用——保证所有外部调用都受任务生命周期（取消/超时）管辖。
- **runner** 对 RAGAS 的使用收敛为「构造 EvaluationDataset → 选指标列表 → evaluate → 逐样本分数回写」，RAGAS 版本升级只动这一个文件。
- **lifecycle** 状态机集中定义合法迁移（如 `evaluating→canceled` 合法、`completed→canceled` 非法），worker 与 API 层共用，避免状态散落。

---

## 4. 评测任务生命周期与并发控制

### 4.1 状态机

```
              ┌──────────────────────────────────────────────┐
              ▼                                              │
pending ──▶ collecting ──▶ evaluating ──▶ completed          │
   │            │              │                             │
   │            ▼              ▼                             │
   │         canceled ◀──（样本边界协作取消）◀─────────────────┘
   ▼            ▲
canceled ───────┘
任意运行态 ──▶ failed（任务级错误：Go API 持续不可达 / DB 写失败 / 未捕获异常）
```

- `collecting`：逐样本回调 Go chat。单样本失败（超时/5xx 重试耗尽）记样本 `error`，**不迁移任务状态**。
- `evaluating`：按批次（默认 8 样本/批）喂给 ragas.evaluate；批内失败降级为逐样本重试一次，再失败记样本错误。
- 完成判定：`collected + failed == total` 且评测侧同理 → `completed`（允许部分样本失败的任务整体成功，报告含 coverage）。

### 4.2 进度与恢复

- 每个样本的状态迁移即时落库（SQLite 单行更新），进度查询零计算。
- **重启恢复**：服务启动时把 `collecting/evaluating` 状态的任务重置为 `pending` 重排队（样本级幂等：已 `ok` 的样本跳过，从断点续跑）；不追求进程崩溃时的调用级续跑（LLM 调用不可重放，样本粒度是最小原子）。

### 4.3 并发模型（三层限额）

| 层 | 机制 | 默认值 | 理由 |
|---|---|---|---|
| 任务级 | 全局最多 `max_running_tasks` 个任务同时运行，超出排队（`429` 仅在队列满时） | 2 | 评测是重 LLM 流量的后台任务，不能挤压线上问答的模型配额 |
| 样本级（采集） | 每任务 `sample_concurrency` 信号量，httpx 并发回调 Go | 4（上限 8） | Go 侧 chat 本身有限流（RateLimit 中间件），采集不能成为洪峰；与 internal/eval N4 语义对齐 |
| 样本级（评测） | ragas evaluate 的批大小 + `llm/ratelimit.py` 对 Judge LLM 的 RPM 令牌桶 | 批 8，RPM 按供应商配额配置 | RAGAS 单样本 faithfulness 就要 1+N 次 LLM 调用，限流必须独立于采集侧 |

### 4.4 超时与取消

| 粒度 | 策略 |
|---|---|
| 单次 chat 回调 | httpx timeout：connect 5s / read 120s（长回答生成）；网络错误与 5xx 指数退避重试 2 次（1s/4s）；4xx 不重试直接记样本失败 |
| 单次 LLM 评审 | 由 ragas/llm 客户端 timeout 控制（60s）；失败按 §4.1 降级重试 |
| 样本级 | 采集+评测合计软超时 300s，超时记样本失败继续 |
| 任务级 | 硬截止 `task_deadline`（默认 2h，可按样本数线性放宽）；到期迁移 `failed`（`error_message` 注明） |
| 取消 | 协作式：worker 在每样本边界检查取消标志；进行中的 httpx 请求通过 `asyncio.CancelledError` 传播中断；LLM 调用不强行中断（等其自然返回后丢弃结果），避免供应商侧计费歧义 |

---

## 5. uv 项目结构建议

### 5.1 仓库落位

```
docs-rag/
├── services/
│   └── ragas-eval/            # 本文 §3 的目录树
│       ├── pyproject.toml
│       ├── Dockerfile
│       └── src/ragas_eval/...
```

独立目录而非塞进 Go 模块根：语言工具链隔离，compose 构建上下文清晰。

### 5.2 pyproject.toml 要点（依赖清单）

```toml
[project]
name = "ragas-eval"
version = "0.1.0"
requires-python = ">=3.11,<3.13"        # ragas 生态对 3.13 支持滞后，钉 3.11/3.12
dependencies = [
    "fastapi>=0.115",                   # Web 框架
    "uvicorn[standard]>=0.30",          # ASGI 服务器
    "ragas>=0.2",                       # 评测框架（0.2+ 的 SingleTurnSample API）
    "langchain-openai>=0.2",            # ragas 的 OpenAI 兼容 LLM/Embedding 适配层
    "httpx>=0.27",                      # 异步 HTTP 客户端（回调 Go API）
    "pydantic>=2.7",
    "pydantic-settings>=2.4",           # env 配置
    "aiosqlite>=0.20",                  # 异步 SQLite
    "python-multipart>=0.0.9",          # 数据集上传（multipart）
    "tenacity>=8.4",                    # 重试/退避
]

[dependency-groups]
dev = [
    "pytest>=8", "pytest-asyncio>=0.24",
    "respx>=0.21",                      # httpx mock
    "ruff>=0.6",                        # lint + format
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

- **包管理**：`uv sync` 生成 `uv.lock` 并提交仓库（可复现构建）；Dockerfile 用 `uv sync --frozen --no-dev` 安装。
- **Python 版本**：`>=3.11,<3.13`。3.11 是为 asyncio 任务组/异常组与 ragas 依赖链（datasets、langchain）兼容性；上限 <3.13 规避 ragas 生态的发布滞后。
- Dockerfile：多阶段——`ghcr.io/astral-sh/uv:python3.11-bookworm` 构建阶段 `uv sync --frozen`，运行阶段 slim 镜像拷贝 `.venv`，非 root 用户运行。

---

## 6. 报告持久化方案与多次对比

### 6.1 选型对比

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 纯文件（JSON/Parquet 落盘） | 零依赖；报告天然可导出 | 无事务（进度与分数部分写入难恢复）；列表/筛选/分页要自己实现；并发写风险 | 弃 |
| 复用 BinRag Postgres | 单库运维；理论上可 join 知识库元数据 | 跨语言共享 schema，迁移权责不清；Go/Pg 升级牵连评测服务；评测服务崩溃风险传导主库 | 弃 |
| **SQLite（WAL 模式）+ 卷持久化（选）** | 嵌入式零运维；事务保证进度/分数一致；单写多读完全匹配负载（控制流读 QPS 低、写是 worker 单写者）；备份=拷一个文件 | 不支持多实例水平扩展（评测服务本就单实例，见 §7） | **选** |

补充设计：

- **样本明细与 contexts 正文**以 JSON 列存于 `samples` 表（SQLite JSON1），不拆关系表——明细是写一次读多次的文档型数据，关系化无收益。
- **汇总指标冗余落 `reports` 表**（任务完成时一次性算好 mean/coverage），报告读取不再扫样本表；样本表仅在明细分页/下钻时访问。
- 保留原始数据集内容哈希与 `config_snapshot`（§2.3-12），报告可复现性靠它而非靠重跑。

主要表结构（逻辑视图）：

```
datasets(id, name, source_format, content_hash, raw_blob, created_at)
tasks(id, name, dataset_id, kb_id, judge_model, metrics_json, status,
      progress_json, error_message, idempotency_key UNIQUE,
      config_snapshot_json, created_at, started_at, finished_at)
samples(id, task_id, idx, question, reference, kb_id,
        answer, contexts_json, scores_json, status, error, UNIQUE(task_id, idx))
reports(task_id PRIMARY KEY, summary_json, created_at)
```

索引：`tasks(status)`、`tasks(created_at DESC)`、`samples(task_id, idx)`、`datasets(content_hash)`。

### 6.2 多次评测对比能力

- **数据面**：`GET /api/v1/eval/reports?task_ids=` 批量返回各任务 `summary + config_snapshot`，一次往返支持对比视图（≤ 8 个任务）。
- **可比性判定**：对比有效性由 `config_snapshot` 三要素决定——`dataset_hash`（同一数据集版本）、`judge_model`（同一评审模型）、`prompt_version`（同一中文提示词版本）。任一不同，前端展示警告而非阻止（分数仍可参考，口径差异明示）。
- **留存策略**：任务与报告默认永久保留；提供 `DELETE /tasks/{id}` 手动清理；预留 `retention_days` 配置（默认 0=不清理），防止 SQLite 无限膨胀。

---

## 7. 部署方式（docker-compose）

### 7.1 新增服务定义（主 docker-compose.yml）

```yaml
  ragas-eval:
    build:
      context: ./services/ragas-eval
    environment:
      - EVAL_LISTEN_PORT=8090
      - EVAL_BINRAG_BASE_URL=http://binrag-server:8085   # 回调 Go 的内网地址
      - EVAL_BINRAG_API_KEY=${RAGAS_EVAL_API_KEY}         # 评测专用 API Key（.env 注入）
      - EVAL_INTERNAL_TOKEN=${EVAL_INTERNAL_TOKEN}        # 与 Go 代理共享的内部令牌
      - EVAL_JUDGE_BASE_URL=...                           # Judge LLM OpenAI 兼容端点（复用主配置同供应商）
      - EVAL_JUDGE_API_KEY=...
      - EVAL_EMBED_BASE_URL=... / EVAL_EMBED_API_KEY=...  # Embedding（必须与索引同源，prompt-design §3.3）
      - EVAL_DB_PATH=/data/eval.db
      - EVAL_MAX_RUNNING_TASKS=2
    volumes:
      - eval_data:/data
    depends_on:
      binrag-server:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "python -c \"import urllib.request,sys;sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8090/healthz').status==200 else 1)\""]
      interval: 30s
      timeout: 5s
      retries: 3
    restart: unless-stopped
    # 注意：不配置 ports —— 不暴露宿主端口，仅 compose 内网可达，入口统一走 Go 代理

volumes:
  eval_data:
```

Go 侧配套改动（属本设计范围内的最小集）：

1. 配置新增 `eval_service_url`（如 `http://ragas-eval:8090`）与 `eval_internal_token`；为空时代理组返回 503「评测服务未配置」。
2. 路由新增代理组：`v1.Any("/eval/*path", evalProxy)`（在 Auth 中间件之后）。
3. `chatRequest`/handler 支持 `include_contexts=true`（query 或 body 字段），为 `Source` 增加 `content,omitempty`，仅评测参数显式开启时填充（prompt-design §4.2 方案 A）。
4. 提交任务的 `kb_id` 越权校验在代理层做（§2.5-4）——建议实现为：代理对 `POST /api/v1/eval/tasks` 特判解析 body 中的 `kb_id`，其余端点纯透传。

### 7.2 环境差异

| 环境 | 形态 |
|---|---|
| 本地开发 | `docker-compose.dev.yml` 只起 pg/qdrant；Go `go run`、Python `uv run uvicorn --reload :8090` 直跑，Python 回调宿主机 Go；前端 vite dev 代理 `/api` 到 Go，无需感知 Python |
| 一键部署 | 主 `docker-compose.yml` 增加 ragas-eval（如上） |
| 生产 | 同 compose 形态；如需独立扩容，评测服务可拆出独立 compose project——SQLite 单写约束决定了它**永远单副本**，扩容方向是垂直加 CPU/内存而非多副本 |

### 7.3 资源与 SLO 建议

- ragas-eval 容器：0.5~1 CPU / 1GiB 起步（瓶颈在外部 LLM 调用，不在本地计算）。
- SLO：任务详情轮询接口 p95 < 50ms；健康检查 p95 < 200ms；任务吞吐由 LLM 配额决定，不设延迟 SLO，只设 `task_deadline` 兜底。

---

## 8. 关键技术决策表

| # | 决策点 | 选择 | 理由 |
|---|---|---|---|
| D1 | 前端接入方式 | **经 Go 反向代理**，不直连 Python | 复用 Auth 中间件与 `{code,message,data}` 契约；前端零新凭据零跨域；Python 不暴露公网端口（§1.2） |
| D2 | 代理形态 | **纯透传反向代理**（路径/包装一致），仅注入内部令牌 + 提交任务时校验 kb 越权 | 避免双层序列化/错误码翻译的漂移成本；业务逻辑留在一处（Python） |
| D3 | 服务间鉴权 | 三层：用户 Bearer（复用）/ Go→Python 共享内部令牌 / Python→Go 专用 API Key | 每层都用既有机制，无新认证体系；采集流量可独立审计与吊销（§2.5） |
| D4 | 上下文正文获取 | Go chat 接口加 `include_contexts=true` 参数（方案 A） | 保证「评测所见 = 生成所用」；比再调一次检索接口（方案 B）口径更真（prompt-design §4.2） |
| D5 | 任务执行器 | **进程内 asyncio worker pool**，不引入 Celery/Redis | 单实例部署下外部队列是纯增运维负担；SQLite 落库 + 启动重排队已满足断点续跑；后续若拆多副本再迁移 Dramatiq/arq |
| D6 | 并发控制 | 三层限额：任务数 2 / 采集样本并发 4 / Judge LLM RPM 令牌桶 | 评测是重 LLM 流量，必须与线上问答隔离配额（§4.3） |
| D7 | 取消语义 | 样本边界协作式取消，进行中的 LLM 调用不硬断 | 避免供应商计费歧义与半成品状态；样本是最小原子单位（§4.4） |
| D8 | 报告存储 | **SQLite（WAL）+ 卷**，不复用 Postgres、不落纯文件 | 单写多读负载匹配；零运维；事务保证进度一致性；跨语言共享主库 schema 的耦合代价过高（§6.1） |
| D9 | 与 internal/eval 关系 | **并存互补**：Recall@K 留 Go CLI，生成侧四指标归 RAGAS 服务；共用 EvalSample 数据集格式 | 检索指标需要进程内访问 retriever（Recall@K 走 HTTP 拿不到 expected_ids 判定所需的完整候选集），生成指标天然黑盒可走 HTTP |
| D10 | 数据集格式 | 复用 `EvalSample`（question/answer/expected_ids/kb_id），Python 侧等价校验 | 一份数据集两侧通用；`expected_ids`/`kb_id` 在 RAGAS 侧降级为 metadata，不浪费人工标注 |
| D11 | 评审模型与 Embedding | 复用项目 OpenAI 兼容配置，temperature=0；Embedding 必须与索引进源 | 不引入新供应商；embedding 不同源会使 answer_relevancy 的向量口径失真（prompt-design §3.3） |
| D12 | 防重复提交 | `Idempotency-Key`（24h 窗口，tasks 表唯一约束） | 评测任务 LLM 成本高，前端双击/网络重试必须幂等 |
| D13 | 多次对比 | 批量汇总接口 + `config_snapshot`（dataset_hash/judge_model/prompt_version）三要素判定可比性 | 对比的有效性取决于口径一致，快照是唯一可靠的判定依据（§6.2） |
| D14 | Python 版本 | 3.11–3.12（`>=3.11,<3.13`），uv + `uv.lock` 提交 | ragas 依赖链对 3.13 支持滞后；锁文件保证镜像可复现 |

---

## 9. 风险与开放问题

| 风险 | 影响 | 缓解 |
|---|---|---|
| RAGAS 版本升级导致分数口径漂移 | 历史报告不可比 | `config_snapshot.ragas_version` 显式记录；升级时发版说明标注「分数不可跨版本对比」 |
| Go `include_contexts` 填充正文增大响应体 | 采集流量变大（内网，可接受） | 仅评测参数开启时填充；正常问答路径零影响 |
| Judge LLM 配额耗尽 | 任务大面积样本失败 | RPM 限流 + 任务级 deadline + 样本级容错；health 接口暴露 judge_llm 状态 |
| SQLite 长期膨胀 | 磁盘占用 | `retention_days` 预留；单文件备份/清理操作简单 |
| 多租户下评测专用 Key 的"系统级不限定"语义 | 低权用户或可见他人库评测分数 | 提交时代理层 kb 越权校验收口（§2.5-4）；任务/报告查询接口暂不携带知识库内容正文，下钻正文按 kb 权限二次校验（Go 代理 `/chunks/{id}` 已有同款语义可参考）——**此项为开放问题，实现前需与权限模型（docs/25）对齐细化** |
