# RAGAS 评测系统 Tasks

> 依据已批准的 spec.md + plan.md。执行顺序：Python 微服务骨架 → Go 侧改动 → 前端 → 集成部署。P0 主链路优先，P1/P2 增强随后。

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `services/ragas-eval/pyproject.toml` | uv 项目定义与依赖锁定 |
| 新建 | `services/ragas-eval/src/ragas_eval/config.py` | pydantic-settings 配置 |
| 新建 | `services/ragas-eval/src/ragas_eval/store/{db,models,repo}.py` | SQLite WAL、表结构、仓储层 |
| 新建 | `services/ragas-eval/src/ragas_eval/api/{middleware,deps,routes_datasets,routes_tasks,routes_reports}.py` | HTTP 接口层 |
| 新建 | `services/ragas-eval/src/ragas_eval/core/{collector,runner,queue,lifecycle,prompts}.py` | 采集器、RAGAS 执行器、任务队列、状态机、中文提示词 |
| 新建 | `services/ragas-eval/src/ragas_eval/llm/{factory,ratelimit}.py` | LLM/Embedding 适配、RPM 限流 |
| 新建 | `services/ragas-eval/src/ragas_eval/main.py` | FastAPI 装配 |
| 新建 | `services/ragas-eval/tests/` | pytest + respx 测试 |
| 新建 | `services/ragas-eval/Dockerfile` | 多阶段镜像 |
| 修改 | `internal/rag/`（Source 结构） | Source 增加 content,omitempty |
| 修改 | `internal/api/handler_chat.go` | include_contexts 出口参数 |
| 新建 | `internal/api/proxy_eval.go` | eval 反向代理 + 内部令牌 + kb 越权校验 |
| 修改 | `internal/api/router.go`、`internal/config/`、`configs/` | 代理路由挂载、两项配置 |
| 新建 | `frontend/src/api/eval.ts`、`frontend/src/stores/eval.ts`、`frontend/src/utils/evalScore.ts` | API 模块、store、分数色阶纯函数 |
| 修改 | `frontend/src/api/types.ts` | 追加评测类型（snake_case 标注） |
| 新建 | `frontend/src/views/eval/*.vue`（4 个）、`frontend/src/components/eval/*.vue`（5 个） | 评测中心页面与组件 |
| 修改 | `frontend/src/router/index.ts`、`frontend/src/components/AppLayout.vue`、`frontend/package.json` | 路由、菜单、echarts 依赖 |
| 修改 | `docker-compose.yml` | ragas-eval 服务 + eval_data 卷 |

---

## 阶段 A：Python 微服务骨架与存储

### T1: uv 项目初始化

**文件：** `services/ragas-eval/pyproject.toml`
**依赖：** 无
**步骤：**
1. `uv init` 建立项目，Python 钉 `>=3.11,<3.13`
2. 按 architect-design §5.2 添加依赖（fastapi/uvicorn/ragas/langchain-openai/httpx/pydantic-settings/aiosqlite/python-multipart/tenacity）与 dev 组（pytest/pytest-asyncio/respx/ruff）
3. `uv sync` 生成 `uv.lock` 并提交

**验证：** `cd services/ragas-eval && uv sync` 成功，`uv.lock` 生成

### T2: 配置模块

**文件：** `src/ragas_eval/config.py`
**依赖：** T1
**步骤：**
1. 用 pydantic-settings 定义 Settings：listen_port、binrag_base_url、binrag_api_key、internal_token、judge_base_url/api_key/model、embed_base_url/api_key/model、db_path、max_running_tasks、sample_concurrency、judge_rpm、task_deadline
2. 全部支持 `EVAL_` 前缀环境变量

**验证：** `uv run python -c "from ragas_eval.config import Settings; print(Settings(...))"` 可实例化（测试注入必填项）

### T3: SQLite 存储层

**文件：** `src/ragas_eval/store/db.py`、`models.py`、`repo.py`
**依赖：** T2
**步骤：**
1. db.py：aiosqlite 连接管理、WAL 模式、启动时执行版本化 DDL（datasets/tasks/samples/reports 四表 + 索引，见 plan 核心数据结构）
2. models.py：Dataset/Task/Sample/Report dataclass
3. repo.py：全部 SQL 收口（任务 CRUD、样本批量写入、进度更新、报告落库与查询、幂等键查重）

**验证：** pytest：建库→插入任务/样本→查询→进度更新→汇总落表，断言字段正确

### T4: 任务状态机

**文件：** `src/ragas_eval/core/lifecycle.py`
**依赖：** T3
**步骤：**
1. 定义状态枚举与合法迁移表（pending→collecting→evaluating→completed；运行态→canceled/failed）
2. 非法迁移抛错；完成判定允许部分样本失败

**验证：** pytest 遍历合法/非法迁移断言

## 阶段 B：Python 微服务核心链路

### T5: 中文评审提示词模块

**文件：** `src/ragas_eval/core/prompts.py`
**依赖：** T1
**步骤：**
1. 按 prompt-design §1 落地四套中文 Prompt（faithfulness 两步、answer_relevancy、context_precision、context_recall），整段覆写 RAGAS Prompt 实例，配中文 few-shot
2. 定义 `PROMPT_VERSION = "zh-v1"` 常量
3. 每个提示词配 ≥3 个回归测试样本

**验证：** pytest：提示词实例化 + 回归样本输出格式解析（verdict 数字 0/1、JSON 可解析）

### T6: LLM/Embedding 适配与限流

**文件：** `src/ragas_eval/llm/factory.py`、`ratelimit.py`
**依赖：** T2
**步骤：**
1. factory：langchain-openai ChatOpenAI/OpenAIEmbeddings 指向配置的 OpenAI 兼容端点，temperature=0，适配为 ragas LLM/Embedding 接口
2. ratelimit：Judge LLM RPM 令牌桶（asyncio 实现）
3. JSON 解析三层降级：response_format → `{}` 提取容错 → json-repair 兜底；429/5xx 指数退避（1s/2s/4s），解析失败 ≤2 次后记 NaN 并计入 parse_failure_rate

**验证：** pytest + respx mock LLM 端点：正常评分、429 重试、坏 JSON 容错、限流生效

### T7: 数据采集器

**文件：** `src/ragas_eval/core/collector.py`
**依赖：** T2
**步骤：**
1. BinRagClient（httpx.AsyncClient）：`POST {binrag_base_url}/api/v1/chat?include_contexts=true`，Bearer 评测专用 Key
2. 超时 connect 5s/read 120s；网络错误与 5xx 指数退避 2 次；4xx 不重试记样本失败
3. 样本级信号量并发（默认 4，上限 8）；返回 answer + contexts 正文

**验证：** pytest + respx mock Go chat：正常采集、超时重试、4xx 直接失败、并发上限生效

### T8: RAGAS 执行器

**文件：** `src/ragas_eval/core/runner.py`
**依赖：** T5、T6
**步骤：**
1. EvalSample → SingleTurnSample 映射（plan 映射表），reference 缺失降级表（context_recall 记 N/A、context_precision 无参考变体）
2. 按批 8 样本调 ragas.evaluate，中文 Prompt + Judge LLM/Embedding 注入
3. 批内失败降级逐样本重试一次，再失败记样本错误；逐样本分数与判定理由回写 repo

**验证：** pytest（mock ragas.evaluate 或用测试 double）：映射正确、降级路径、部分失败回写

### T9: 任务队列与 worker

**文件：** `src/ragas_eval/core/queue.py`
**依赖：** T4、T7、T8
**步骤：**
1. asyncio.Queue + worker pool，全局 max_running_tasks（默认 2），超出排队，队列满 429
2. worker 流程：collecting（采集器）→ evaluating（执行器）→ 汇总落 reports → completed
3. 样本边界协作式取消（每样本检查取消标志）；任务级 deadline（默认 2h）
4. 启动恢复：collecting/evaluating 任务重置 pending 重排队，已 ok 样本跳过

**验证：** pytest：提交任务→跑完→状态流转与进度正确；取消生效；模拟重启断点续跑

## 阶段 C：Python HTTP 接口层

### T10: 中间件与统一响应

**文件：** `src/ragas_eval/api/middleware.py`、`deps.py`
**依赖：** T2
**步骤：**
1. 内部令牌校验中间件（X-Eval-Internal-Token，缺失/错误 401，health 豁免）
2. request_id 生成与回显；异常→`{code,message}` 统一包装（错误码表见 architect-design §2.4）

**验证：** pytest + httpx ASGI 测试客户端：无令牌 401、异常包装格式正确

### T11: 数据集与任务路由

**文件：** `src/ragas_eval/api/routes_datasets.py`、`routes_tasks.py`
**依赖：** T3、T9、T10
**步骤：**
1. 数据集：列表/上传（multipart，≤10MB，EvalSample 校验对齐 Go dataset.go Validate，422 带行号）/预览/删除（被引用 409）
2. 任务：提交（Idempotency-Key 24h 幂等）/列表/详情（轻量轮询）/取消（非运行态 409）/删除
3. 提交前预检依赖（Go API 与 Judge LLM 连通性，失败 503）

**验证：** pytest 全覆盖：合法/非法数据集、幂等重提交返回同任务、取消状态冲突 409

### T12: 报告路由与服务装配

**文件：** `src/ragas_eval/api/routes_reports.py`、`src/ragas_eval/main.py`
**依赖：** T11
**步骤：**
1. 报告：汇总+明细分页（page/page_size/metric_lt/sort）、单样本下钻（含 contexts 与理由）、批量对比（≤8 任务）；任务未完成 409
2. health：status/checks（db/binrag_api/judge_llm/queue）+ 裸 `/healthz`
3. main.py 装配：中间件、路由、启动建库与任务恢复、关停钩子

**验证：** `uv run uvicorn ragas_eval.main:app` 启动，curl health 返回 ok；pytest 覆盖报告分页/过滤/下钻/409

## 阶段 D：Go 侧改动

### T13: chat include_contexts 出口

**文件：** `internal/rag/`（Source 结构）、`internal/api/handler_chat.go`
**依赖：** 无（可与阶段 A-C 并行）
**步骤：**
1. `Source` 增加 `Content string \`json:"content,omitempty"\``
2. chat handler 支持 `include_contexts=true`（query），为 true 时从检索结果填充各 Source.Content
3. 默认路径不填充（零影响）；swagger 注释同步

**验证：** `go build ./...` 通过；新增/更新单测：include_contexts=true 响应含 content、默认不含

### T14: eval 反向代理

**文件：** `internal/api/proxy_eval.go`、`internal/api/router.go`、`internal/config/`、`configs/`
**依赖：** T13
**步骤：**
1. 配置新增 `eval_service_url`、`eval_internal_token`（为空时代理组 503「评测服务未配置」）
2. `httputil.ReverseProxy` 纯透传 `/api/v1/eval/*`，注入 X-Eval-Internal-Token
3. `POST /api/v1/eval/tasks` 特判：解析 body 中 kb_id，调 ensureKBAccess 越权校验（越权 404）
4. 代理组挂在 Auth 中间件之后；health 端点豁免或 bootstrap key

**验证：** `go build/test/vet ./...` 通过；单测：令牌注入头、kb 越权 404、未配置 503

## 阶段 E：前端

### T15: API 层与类型

**文件：** `frontend/src/api/types.ts`、`api/eval.ts`、`utils/evalScore.ts`
**依赖：** 无（依赖契约，可 mock 开发）
**步骤：**
1. types.ts 追加评测类型（frontend-design §3.3，头部注释标注 snake_case 模块）
2. api/eval.ts 12 个方法，复用 `request<T>()`；上传走 FormData（对齐 api/doc.ts）
3. evalScore.ts：score→色阶/文案纯函数

**验证：** `pnpm tsc --noEmit`（或项目 lint/typecheck 脚本）通过；evalScore 单测

### T16: store 与路由菜单

**文件：** `frontend/src/stores/eval.ts`、`router/index.ts`、`components/AppLayout.vue`
**依赖：** T15
**步骤：**
1. eval store（选项式）：任务列表/筛选/currentTask/currentReport/draft/轮询定时器
2. 轮询：setTimeout 链式退避（2s→5s→15s）、visibilitychange 停/恢复、终态与 unmount 清理、连续失败 3 次暂停提示
3. 路由注册 /eval 组（/eval/new 先于 /eval/:id）；AppLayout 侧边栏加「评测中心」（DataAnalysis 图标）+ default-active 子路径高亮修正

**验证：** `pnpm build` 通过；手动验证菜单高亮与路由跳转

### T17: P0 页面——列表与发起

**文件：** `views/eval/EvalListView.vue`、`EvalNewView.vue`、`components/eval/EvalTaskCard.vue`
**依赖：** T16
**步骤：**
1. ListView：page-head + 状态筛选 + 任务卡片（状态 tag/进度条/四指标迷你值）+ 批量勾选对比 + 空态；running 任务 10s 低频轮询
2. NewView：三区块表单（评测对象/指标/评审模型与高级参数）、数据集预览与校验、无 reference 禁用 context_recall、提交后跳详情、草稿存 store

**验证：** 手动走查：列表渲染、筛选、表单校验、提交跳转

### T18: P0 页面——详情与报告

**文件：** `views/eval/EvalDetailView.vue`、`components/eval/{MetricSummaryCards,EvalSampleTable,EvalSampleDrawer}.vue`
**依赖：** T17
**步骤：**
1. 运行中形态：状态卡 + el-steps 阶段条 + 进度条 + 取消按钮；失败形态：错误卡 + 重新发起（预填）
2. 报告形态：四指标汇总卡（色阶）+ 明细表（先 el-table 后端分页，排序/低分过滤 chip）+ Drawer 下钻（MarkdownRenderer 渲染回答、仿 SourceCard 展示 contexts、上一条/下一条）

**验证：** 手动走查完整流程：提交→轮询→报告→下钻

### T19: P1 可视化

**文件：** `package.json`、`components/eval/EvalChart.vue`、`EvalDetailView.vue`（报告区接入）
**依赖：** T18
**步骤：**
1. 装 echarts + vue-echarts，按需注册，EvalChart 薄封装（init/resize/dispose）
2. option 工厂从 --br-* CSS 变量取色，监听 html.dark 重建
3. 报告区雷达图 + 逐指标分布图，defineAsyncComponent 懒加载

**验证：** 亮/暗主题下图表配色一致；构建主包不含 echarts（路由级分包）

### T20: P2 对比视图

**文件：** `views/eval/EvalCompareView.vue`
**依赖：** T19
**步骤：**
1. 并排汇总表（最优标记、差异>0.05 着色）+ 叠加雷达图（≤4 任务）
2. config_snapshot 三要素不一致时显示可比性警告条
3. 勾选防呆（仅 completed、≤4）

**验证：** 两个真实任务对比渲染正确、警告条触发

## 阶段 F：集成与部署

### T21: Dockerfile 与 compose

**文件：** `services/ragas-eval/Dockerfile`、`docker-compose.yml`
**依赖：** T12
**步骤：**
1. 多阶段 Dockerfile（uv:python3.11-bookworm 构建，`uv sync --frozen --no-dev`，slim 运行，非 root）
2. compose 加 ragas-eval 服务（不暴露端口、depends_on 健康、eval_data 卷、EVAL_* 环境变量）

**验证：** `docker compose build ragas-eval` 成功；`docker compose up` 后 health 通过、Go 代理链路打通

### T22: 端到端联调

**依赖：** T14、T20、T21
**步骤：**
1. 准备 ≥5 条样本的测试数据集（含 1 条无 reference、1 条指向不存在 KB 的样本）
2. 完整走查 AC1–AC10：上传→发起→进度→报告（四指标+理由）→降级标注→样本级容错→取消→前端全流程→直连 Python 401
3. 双侧测试：`go build/test/vet ./...` + `uv run pytest` + `uv run ruff check` + `pnpm build`

**验证：** 端到端场景全部通过，证据留档（命令输出/截图）

## 执行顺序

```
T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9 → T10 → T11 → T12 ─┐
T13 → T14（可与 A-C 并行）─────────────────────────────────────┤
T15 → T16 → T17 → T18 → T19 → T20（依赖契约，可与 A-D 并行）────┤
                                                               ▼
                                            T21 → T22（端到端联调）
```
