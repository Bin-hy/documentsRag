# RAGAS 评测系统 Plan

> 基于已批准的 `spec.md`，汇总三份专项设计：`architect-design.md`（架构与接口契约）、`prompt-design.md`（四指标中文提示词与评审模型策略）、`frontend-design.md`（前端页面与 API 层）。细节以各专项文档为准，本文档定义整体架构、核心接口与文件组织。

## 架构概览

```
前端（Vue3 评测中心）
   │ 同源 /api/v1/eval/*（Bearer JWT/API Key，{code,message,data} 包装）
   ▼
Go 后端（Gin）
   ├─ Auth 中间件（现有，零改动）
   ├─ Eval 代理组：纯透传 + 注入 X-Eval-Internal-Token + 提交任务时校验 kb_id 越权
   └─ chat 接口新增 include_contexts 出口参数（Source 附 content,omitempty）
   ▲ ③ 采集回调（评测专用 API Key）        │ ① 控制流（HTTP 内网）
   │                                       ▼
   └───────────────  ragas-eval Python 微服务（uv + FastAPI，不暴露宿主端口）
                     ├─ API 层（routers：datasets / tasks / reports / health）
                     ├─ 任务队列（asyncio worker pool，进程内，不引入 Celery/Redis）
                     ├─ 数据采集器（httpx 回调 Go chat?include_contexts=true）
                     ├─ RAGAS 执行器（中文 Prompt，四指标，Judge LLM/Embedding 复用项目配置）
                     └─ SQLite（WAL）报告存储 + 卷持久化

并存不动：internal/eval CLI（Recall@K 检索指标，进程内调用 retriever）
```

四个组件职责：

1. **Go 代理层**：`/api/v1/eval/*` 纯透传反向代理（`httputil.ReverseProxy`），路径与响应包装与 Python 侧完全一致，不做报文改写。唯一业务逻辑：前置 Auth、注入内部令牌、对 `POST /tasks` 特判解析 body 中 `kb_id` 做越权校验。
2. **ragas-eval 微服务**：评测核心。采集器是唯一与 Go 通信的模块，llm/factory 是唯一与模型供应商通信的模块，两者只被 worker 调用（受任务生命周期管辖）。
3. **前端评测中心**：四个页面（列表/发起/详情/对比）+ `api/eval.ts` + `stores/eval.ts`，所有请求经 Go 代理。
4. **SQLite 存储**：任务状态、样本明细（JSON 列）、汇总报告（冗余落表，读取不扫样本表）。

## 核心数据结构

### HTTP 契约（Python 服务 ↔ Go 代理 ↔ 前端）

14 个端点，全部 `/api/v1/eval` 前缀，snake_case，`{code,message,data}` 包装（完整契约见 architect-design §2）：

| 分组 | 端点 |
|---|---|
| 健康 | `GET /health` |
| 数据集 | `GET/POST /datasets`、`GET /datasets/{id}/preview`、`DELETE /datasets/{id}` |
| 模型 | `GET /judge-models` |
| 任务 | `POST /tasks`（支持 Idempotency-Key）、`GET /tasks`、`GET /tasks/{id}`（轮询）、`POST /tasks/{id}/cancel`、`DELETE /tasks/{id}` |
| 报告 | `GET /tasks/{id}/report`（明细分页/排序/低分过滤）、`GET /tasks/{id}/report/samples/{sample_id}`（下钻）、`GET /reports?task_ids=`（对比，≤8） |

错误码：400/401/404/409/422/429/500/503，code 与 HTTP 状态一致；单样本失败不产生 HTTP 错误，计入 `progress.failed`。

### 鉴权三层模型

```
前端 ──Bearer JWT/API Key──▶ Go Auth 中间件（复用，零改动）
Go 代理 ──X-Eval-Internal-Token──▶ Python 校验中间件（共享密钥）
Python 采集器 ──Bearer 评测专用 API Key──▶ Go /api/v1/chat（既有查库流程）
```

### 任务状态机

```
pending ──▶ collecting ──▶ evaluating ──▶ completed
   │            │              │
   ▼            ▼              ▼
 canceled ◀──（样本边界协作取消，进行中的 LLM 调用不硬断）
任意运行态 ──▶ failed（任务级错误）
```

- 单样本失败记样本 error，不迁移任务状态；完成判定允许部分样本失败（报告含 coverage）
- 重启恢复：`collecting/evaluating` 重置为 `pending` 重排队，已 ok 样本跳过（样本级断点续跑）
- 三层并发限额：任务级 2 / 采集样本并发 4 / Judge LLM RPM 令牌桶

### Go 侧数据结构改动

- `chatRequest` 增加 `include_contexts`（query 参数），为 true 时 `Source` 填充 `content,omitempty`（默认路径零影响）
- 配置新增 `eval_service_url` 与 `eval_internal_token`（为空时代理组返回 503）

### 数据集映射（EvalSample → RAGAS SingleTurnSample）

| RAGAS 字段 | 来源 |
|---|---|
| user_input | EvalSample.Question |
| response | Go chat 响应 answer |
| retrieved_contexts | Go chat 响应 sources[].content（include_contexts=true） |
| reference | EvalSample.Answer（缺失时 context_recall 记 N/A、context_precision 降级无参考变体） |

### SQLite 表（逻辑视图）

```
datasets(id, name, source_format, content_hash, raw_blob, created_at)
tasks(id, name, dataset_id, kb_id, judge_model, metrics_json, status,
      progress_json, error_message, idempotency_key UNIQUE,
      config_snapshot_json, created_at, started_at, finished_at)
samples(id, task_id, idx, question, reference, kb_id,
        answer, contexts_json, scores_json, status, error, UNIQUE(task_id, idx))
reports(task_id PRIMARY KEY, summary_json, created_at)
```

## 模块设计

### Go 侧（最小改动集）

| 模块 | 职责 | 文件 |
|---|---|---|
| eval 代理 | 反向代理 + 内部令牌注入 + kb 越权校验 | `internal/api/proxy_eval.go` |
| chat 出口 | include_contexts 参数与 Source.content 填充 | `internal/api/handler_chat.go`、`internal/rag/`（Source 结构） |
| 配置 | eval_service_url / eval_internal_token | `internal/config/`、`configs/` |

### Python 微服务（services/ragas-eval/）

```
services/ragas-eval/
├── pyproject.toml                  # uv 管理，Python >=3.11,<3.13，uv.lock 提交
├── Dockerfile                      # 多阶段：uv sync --frozen --no-dev，非 root 运行
├── src/ragas_eval/
│   ├── main.py                     # FastAPI 装配
│   ├── config.py                   # pydantic-settings
│   ├── api/
│   │   ├── deps.py / middleware.py # DI / 内部令牌校验 / 统一异常包装
│   │   ├── routes_datasets.py      # 数据集 CRUD + 预览
│   │   ├── routes_tasks.py         # 任务提交/查询/取消/删除
│   │   └── routes_reports.py       # 报告/下钻/批量对比
│   ├── core/
│   │   ├── collector.py            # BinRagClient（httpx，超时/重试/样本容错）
│   │   ├── runner.py               # RAGAS 执行器（SingleTurnSample 映射、指标分派、降级表）
│   │   ├── queue.py                # asyncio.Queue + worker pool
│   │   ├── lifecycle.py            # 状态机（合法迁移集中定义）
│   │   └── prompts.py              # 四套中文 Prompt（prompt_version 常量，见 prompt-design §1）
│   ├── store/
│   │   ├── db.py / models.py / repo.py  # aiosqlite WAL、迁移、仓储收口
│   └── llm/
│       ├── factory.py              # ragas LLM/Embedding 适配（OpenAI 兼容，temp=0）
│       └── ratelimit.py            # Judge LLM RPM 令牌桶
└── tests/                          # pytest + pytest-asyncio + respx
```

### 前端（frontend/src/）

| 新增 | 职责 |
|---|---|
| `api/eval.ts` + `types.ts` 追加 | 12 个方法（snake_case 模块，注释标注），复用 `request<T>()` |
| `stores/eval.ts` | 任务列表/详情状态、链式退避轮询（2s→5s→15s）、visibilitychange 停/恢复、终态清理 |
| `views/eval/EvalListView.vue` | 任务列表、状态筛选、批量勾选对比 |
| `views/eval/EvalNewView.vue` | 发起表单（独立页）：知识库/数据集（预览校验）/指标/评审模型/高级参数 |
| `views/eval/EvalDetailView.vue` | 运行中=进度面板；完成=报告（指标卡+雷达图+分布图+明细表+Drawer 下钻） |
| `views/eval/EvalCompareView.vue` | 并排汇总表 + 叠加雷达图 + 可比性警告（依 config_snapshot 三要素） |
| `components/eval/EvalChart.vue` | vue-echarts 薄封装，从 `--br-*` 变量生成 option，跟随亮暗主题 |

改动：`router/index.ts`（/eval 路由组，/eval/new 先于 /eval/:id 注册）、`AppLayout.vue`（侧边栏加「评测中心」+ default-active 子路径高亮修正）、`package.json`（echarts + vue-echarts，路由内懒加载）。

## 模块交互

1. **发起评测**：前端 `POST /api/v1/eval/tasks` → Go Auth → 代理解析 kb_id 越权校验 → 注入内部令牌透传 → Python 校验令牌 → 落库 pending → 201 TaskView（幂等键防重）
2. **任务执行**：worker 领取 → collecting：采集器并发（信号量 4）回调 `POST /api/v1/chat?include_contexts=true`（评测专用 Key）→ 逐样本落库 → evaluating：按批 8 样本喂 ragas.evaluate（中文 Prompt，Judge temp=0，RPM 限流）→ 逐样本分数落库 → completed：汇总算 mean/coverage 落 reports 表
3. **进度轮询**：前端 `GET /tasks/{id}`（纯 SQLite 读，p95<50ms），退避 2s→15s，隐藏暂停，终态清理
4. **报告查看**：`GET /tasks/{id}/report`（summary + 明细分页，不含 contexts 正文）→ 下钻 `GET .../samples/{id}`（含 contexts 正文与判定理由）
5. **对比**：`GET /reports?task_ids=` → 前端依 config_snapshot（dataset_hash/judge_model/prompt_version）判定可比性并警告

## 文件组织

```
docs-rag/
├── services/ragas-eval/            # Python 微服务（如上目录树，uv 管理）
├── internal/api/proxy_eval.go      # Go 代理（新增）
├── internal/api/handler_chat.go    # include_contexts（修改）
├── internal/config/ + configs/     # 两项配置（修改）
├── frontend/src/
│   ├── api/eval.ts                 # 新增
│   ├── api/types.ts                # 追加评测类型
│   ├── stores/eval.ts              # 新增
│   ├── views/eval/*.vue            # 新增 4 页
│   ├── components/eval/EvalChart.vue  # 新增
│   ├── router/index.ts             # 修改
│   └── layouts/AppLayout.vue       # 修改
├── docker-compose.yml              # 新增 ragas-eval 服务 + eval_data 卷
└── docs/35-ragas评测/              # spec/plan/task/checklist + 三份专项设计
```

## 技术决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 前端接入方式 | 经 Go 反向代理，不直连 Python | 复用 Auth 与响应包装；前端零新凭据零跨域；Python 不暴露公网端口 |
| 代理形态 | 纯透传 + 注入内部令牌 + kb 越权校验 | 避免双层序列化漂移；业务逻辑收敛 Python 一侧 |
| 服务间鉴权 | 三层：用户 Bearer / 共享内部令牌 / 评测专用 API Key | 全部复用既有机制；采集流量可独立审计吊销 |
| 上下文正文获取 | chat 加 include_contexts=true | 评测所见 = 生成所用，口径最真 |
| 任务执行器 | 进程内 asyncio worker pool | 单实例部署下外部队列是纯运维负担 |
| 并发控制 | 三层限额（任务 2 / 采集 4 / LLM RPM 令牌桶） | 评测重 LLM 流量须与线上问答隔离配额 |
| 取消语义 | 样本边界协作式 | 避免供应商计费歧义与半成品状态 |
| 报告存储 | SQLite（WAL）+ 卷 | 单写多读匹配；零运维；跨语言共享主库 schema 耦合代价高 |
| 与 internal/eval 关系 | 并存互补，共用 EvalSample 数据集格式 | 检索指标需进程内访问；生成指标天然黑盒走 HTTP |
| 评审提示词 | 整段覆写 RAGAS Prompt 实例 + 中文 few-shot，不用 adapt_prompts 自动翻译 | 自动翻译示例分布仍英文、质量不可控；沿用 judge.go 已验证经验 |
| 评审/Embedding 模型 | 复用项目 OpenAI 兼容配置，temp=0；Embedding 必须与索引同源 | 不引入新供应商；embedding 不同源使 relevancy 向量口径失真 |
| 防重复提交 | Idempotency-Key（24h，唯一约束） | 评测 LLM 成本高，双击/重试必须幂等 |
| 多次对比 | 批量汇总接口 + config_snapshot 三要素判定可比性 | 口径一致性靠快照判定，前端警告而非阻止 |
| Python 版本 | >=3.11,<3.13，uv.lock 提交 | ragas 生态对 3.13 支持滞后；锁文件保证可复现 |
| 图表库 | ECharts + vue-echarts 按需注册、路由内懒加载、对接 --br-* 主题变量 | 雷达/分布图开箱即用；保持 Soft Structuralism 视觉一致 |

## 开放问题

- 多租户下报告下钻正文的权限收口需与 docs/25 权限模型对齐细化（本期：提交时 kb 越权校验 + 下钻正文按 kb 权限二次校验，参考 `/chunks/{id}` 同款语义）
