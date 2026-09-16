# RAGAS 评测中心 —— 前端设计方案

> 版本：v1.0（设计稿，不含实现代码）
> 前置背景：评测由独立 Python 微服务（FastAPI 封装 RAGAS）执行，通过 HTTP 调用 Go 后端 `/api/v1/chat` 收集检索上下文与回答，产出 RAGAS 四指标（faithfulness / answer_relevancy / context_precision / context_recall）逐样本分数与汇总报告。评测为长任务，交互模型为 **提交 → 异步执行 → 轮询进度 → 查看报告**。

## 0. 设计约束（来自现有代码库的观察结论）

实现前先固化对现有前端的观察，所有新页面必须遵守：

| 维度 | 现状 | 评测中心的遵循方式 |
| --- | --- | --- |
| 技术栈 | Vue 3 `<script setup lang="ts">` + Vite + Pinia（选项式 `defineStore`）+ Vue Router + Element Plus | 完全一致；store 沿用选项式写法（见 `stores/doc.ts`） |
| UI 组件 | Element Plus 2.14 + `@element-plus/icons-vue`，全局已在 main.ts 挂载 | 不引入第二个组件库；图标从 `@element-plus/icons-vue` 选（如 `DataAnalysis` / `Histogram`） |
| 设计语言 | Soft Structuralism：Indigo 品牌色（`--br-primary: #4f5bd5`），CSS 变量主题（`--br-*`），亮/暗双主题经 `html.dark` 切换 | 所有自定义样式只用 `--br-*` 变量，禁止硬编码颜色；图表库主题需对接亮/暗切换 |
| 卡片模式 | 双层结构卡片 + `hover: translateY(-3px)` 抬升（见 `KbListView.vue`） | 任务卡片、指标汇总卡沿用同一 hover/阴影/圆角规则 |
| API 层 | `api/client.ts` 统一 `request<T>()`：Bearer 凭据、`{code,message,data}` 解包、401 跳登录；每资源一个 `api/xxx.ts` 模块 | 新增 `api/eval.ts`，不新建 axios 实例 |
| 类型 | `api/types.ts` 集中定义；**Go 后端序列化为 PascalCase**（如 `Kb.ID`），但部分 handler（keyView 等）用 snake_case json tag | 评测微服务为 Python/FastAPI，**端点与字段命名统一用 snake_case**，并在 types.ts 注释中明确标注该模块是 snake_case（与 `ApiKeyView` 同样的先例） |
| 轮询先例 | `stores/doc.ts`：`setInterval` 3s、无活动任务自动停止、`stopPolling()` 手动停止 | 评测轮询在同一模式上做增强（退避 + 页面可见性） |
| 页面骨架 | `page-head`（标题 + 右上主按钮）+ 内容区，padding `24px 28px` | 三个评测页面沿用同一骨架 |
| 提示反馈 | `ElMessage.success/error` + `ElMessageBox.confirm` 二次确认 | 提交、取消、删除任务全部沿用 |

---

## 1. 页面信息架构

### 1.1 路由设计

在 `router/index.ts` 的 `AppLayout` children 中新增一组 `/eval` 路由（保持扁平、懒加载风格）：

| 路径 | name | 组件 | 说明 |
| --- | --- | --- | --- |
| `/eval` | `eval-list` | `views/eval/EvalListView.vue` | 评测任务列表（默认页） |
| `/eval/new` | `eval-new` | `views/eval/EvalNewView.vue` | 发起评测（独立页而非弹窗，见 1.3） |
| `/eval/:id` | `eval-detail` | `views/eval/EvalDetailView.vue` | 任务详情：进度态 / 报告态 |
| `/eval/compare?ids=a,b` | `eval-compare` | `views/eval/EvalCompareView.vue` | 多次评测结果对比（ids 经 query 传入，可分享链接） |

设计要点：

- `/eval/new` 放在 `/eval/:id` **之前**注册，避免 `new` 被 `:id` 吞掉。
- 列表页是入口，详情页通过 `router.push(\`/eval/\${task.id}\`)` 进入（与 `KbListView → KbDetailView` 的模式一致）。
- 对比页用 query 而非 path 参数承载 id 列表：id 数量可变（2~4 个），且便于复制 URL 分享对比结果。
- 登录守卫无需改动：`/eval/*` 不在 `meta.public` 内，自动受保护。

### 1.2 与布局的集成

`AppLayout.vue` 侧边栏 `el-menu` 在「知识库」之后插入一项：

```
/eval  →  图标 DataAnalysis（或 Histogram）→ 文案「评测中心」
```

菜单位置理由：评测是知识库质量的下游环节，紧跟知识库符合心智顺序。`el-menu` 的 `router` 模式 + `:default-active="route.path"` 已支持子路径高亮问题——`/eval/:id` 时 `route.path` 为 `/eval/xxx` 不会匹配 `/eval` 菜单项，需要把 `:default-active` 改为计算属性：`route.path.startsWith('/eval') ? '/eval' : route.path`（这是对 AppLayout 的唯一逻辑改动，应一并推广到 `/kb/:id` 现有的小缺陷）。

### 1.3 页面划分与职责

```
评测中心
├── EvalListView      任务列表：状态筛选、进度一览、进入详情、批量勾选发起对比、删除
├── EvalNewView       发起评测：表单（知识库/数据集/指标/评审模型）+ 预检 + 提交后跳转详情
├── EvalDetailView    任务详情：运行中=进度面板；完成=报告（汇总图表 + 明细表 + 单样本下钻）
└── EvalCompareView   对比视图：多任务汇总指标并排 + 雷达图叠加 + 差异明细
```

为什么「发起评测」用独立页而非 `el-dialog`：表单字段多（知识库、数据集、指标多选、评审模型、采样上限、并发等），且需要展示数据集预览（样本数、字段校验结果），对话框宽度不够；独立页还可被路由直达/刷新不丢状态（表单草稿存 store）。项目现有「新建知识库」用弹窗是因为只有 2 个字段，场景不同。

---

## 2. 各页面组件设计

### 2.1 EvalListView —— 评测任务列表

**布局**：`page-head`（标题「评测中心」+ 主按钮「发起评测」）→ 筛选栏 → 任务卡片列表（或表格）。

组件构成：

- **筛选栏**：`el-radio-group` 状态筛选（全部 / 运行中 / 已完成 / 失败 / 已取消）+ 知识库下拉过滤 + 关键词搜索（任务名）。
- **任务卡片**（沿用 kb-card 双层卡片语言）：任务名、知识库名、数据集名、状态 `el-tag`（pending=info / running=primary / completed=success / failed=danger / cancelled=warning）、进度条（运行中显示 `已评 N / 共 M 条`）、四项指标迷你数值（完成后）、创建时间、耗时。
- **批量对比**：卡片左上角 checkbox，勾选 2~4 个「已完成」任务后底部浮起操作条「对比所选（N）」→ 跳 `/eval/compare?ids=...`。不足 2 个时按钮禁用并 tooltip 说明。
- **运行中任务**：列表页对 running 任务低频轮询（10s，只刷新列表摘要，不进详情页），由 store 统一管理。
- 空态：`el-empty`「还没有评测任务，点击右上角发起第一次评测」（与知识库空态文案结构一致）。

### 2.2 EvalNewView —— 发起评测表单

**布局**：单列居中表单（max-width 720px），分三个卡片区块，底部吸底操作条。

**区块一：评测对象**
- 任务名称 `el-input`（默认自动生成：`{知识库名}-{日期}`，可改）
- 知识库 `el-select`（数据来自 `useKbStore`，必填）
- 评测数据集 `el-select` + 「上传新数据集」：数据集由评测微服务管理（预先注册的 JSON/JSONL，每条含 question / ground_truth / 可选 reference_contexts）。选择后下方展示数据集预览卡：样本数、字段校验结果、前 2 条样本折叠预览。上传走 `el-upload`（json/jsonl，前端做大小与首条结构预校验）。

**区块二：评测指标**
- `el-checkbox-group` 四指标多选，默认全选。每个指标附一行小字说明：
  - Faithfulness 忠实度：回答是否忠于检索内容
  - Answer Relevancy 答案相关性：回答是否切题
  - Context Precision 上下文精确率：检索结果中相关内容的排序质量
  - Context Recall 上下文召回率：检索是否覆盖标准答案所需信息（**需要 ground_truth**，数据集缺该字段时此项禁用并提示）

**区块三：评审模型与执行参数**
- 评审模型（Judge LLM）`el-select`：选项来自后端 `GET /api/v1/eval/judge-models`（微服务配置的可用模型清单），不在前端硬编码。
- 高级项 `el-collapse`：采样上限（样本数过多时截断，默认全量）、并发数、单样本超时、随机种子（可复现性，对应 docs/13 spec F8）。

**提交逻辑**：
1. `el-form` 校验 → `POST /api/v1/eval/tasks` → 拿到 `task_id`
2. `ElMessage.success('评测任务已提交')`，`router.push(\`/eval/\${task_id}\`)`
3. 表单草稿（除上传文件外）存入 store，提交失败/中途离开再回来可恢复。

### 2.3 EvalDetailView —— 任务详情与报告

页面按任务状态分两种形态，同一组件内切换。

**形态 A：运行中 / 排队中（进度面板）**
- 顶部状态卡：大状态标识 + 已运行时长 + 进度条（`已完成 N / M 样本`，百分比来自后端 `progress` 字段）。
- 阶段步骤条 `el-steps`：收集回答（调 /api/v1/chat）→ RAGAS 评分 → 生成报告。后端在任务详情里返回 `current_stage`，前端只做映射展示。
- 实时日志（可选，后端若提供 `logs` 尾部字段则展示最近 5 条；无则不放）。
- 操作：「取消任务」按钮（`ElMessageBox.confirm` 二次确认 → `POST .../cancel`）；轮询进行中（见 §4.2）。
- 失败态：红色错误卡（ErrorMessage + 失败阶段 + 可展开堆栈摘要）+「重新发起」按钮（带着原参数跳 `/eval/new` 并预填表单）。

**形态 B：已完成（报告）**

从上到下四个区域：

1. **汇总卡片区**：四个指标各一张数字卡（值 0~1，保留 3 位小数 + 达标色阶：≥0.8 绿 / 0.6~0.8 品牌色 / <0.6 橙）。卡片下方小字标注样本数与参与评分的指标子集。hover 沿用现有卡片抬升效果。
2. **可视化区**（详见 2.4 图表选型）：
   - 主视图：**雷达图**，四指标四维，一眼看短板；
   - 切换 Tab 或并排：**逐指标分布直方图/箱线**（四张小图或一张分组柱状图），展示逐样本分数分布而不只是均值——均值 0.8 可能是「全部 0.8」也可能是「一半 1.0 一半 0.6」，分布对调优更有用。
3. **逐样本明细表**：`el-table-v2`（虚拟滚动表格，见 §5.2）。列：#、question（省略 2 行）、answer（省略）、四指标分数（色阶着色单元格 + 排序）、操作「详情」。表头支持按任一指标排序、按分数区间过滤（如只看 faithfulness < 0.6 的样本——这是调优时最高频的操作，做成快捷筛选 chip）。
4. **单样本下钻**：点击行「详情」→ `el-drawer`（右侧抽屉，宽度 60%）展示完整内容：
   - Question 全文
   - Answer 全文（用现有 `MarkdownRenderer` 渲染）
   - Ground Truth（若数据集提供）
   - Contexts 列表：复用/仿照现有 `SourceCard` 的卡片样式逐条展示检索片段（内容 + 来源文件名 + 相关性得分）
   - 四指标分数条（每指标一条 0~1 进度条 + 数值）
   - 若后端返回 RAGAS 的逐条 reason/verdict 则折叠展示（调试用）

用 Drawer 而非新路由：下钻是报告的上下文内浏览，用户常在多个样本间连续查看（抽屉内提供「上一条/下一条」按钮），跳转路由会打断这个流。

### 2.4 图表库选型：ECharts（vue-echarts）

候选对比：

| 方案 | 结论 | 理由 |
| --- | --- | --- |
| **ECharts + vue-echarts** | ✅ 选用 | 雷达图/柱状图/箱线图开箱即用；按需注册可 tree-shake（RadarChart/BarChart/BoxplotChart + CanvasRenderer，增量约 300~400KB gzip 前，懒加载路由内引入不进主包）；原生支持暗色主题与 responsive resize；中文社区文档完善 |
| Chart.js + vue-chartjs | 备选 | 包体更小，但雷达图样式定制与暗色适配弱于 ECharts，箱线图需插件 |
| 自绘 SVG | 否 | 四指标雷达 + 分布图自绘成本高，且后续对比视图要叠加多条雷达，自绘不划算 |

引入约束（写进实现规范）：
- `vue-echarts` + `echarts/core` 按需 import，封装一个 `components/eval/EvalChart.vue` 薄封装（props: option / height，内部处理 init、resize observer、dispose）。
- **主题对接**：不加载 ECharts 内置 dark 主题，而是定义一套从 CSS 变量读取的 option 工厂（`getChartOption(isDark)`），颜色全部取自 `--br-primary` / `--br-text-secondary` / `--br-border`；监听 `html.dark` class 变化（MutationObserver 或主题切换时 emit）重建 option。这样图表与全站 Soft Structuralism 语言一致，而非"图表一块深色、页面一块深色"的割裂感。
- 雷达图配色：主任务 `--br-primary`；对比视图叠加时用品牌色 + 绿/橙/紫一组协调色板（定义在常量里，亮暗两套）。
- 图表区块懒加载：`EvalDetailView` 的报告区 `defineAsyncComponent` 引入，运行中形态不下载 ECharts。

### 2.5 EvalCompareView —— 多任务对比

- 进入方式：列表页勾选跳转（`?ids=a,b,c`），或详情页「与其他任务对比」。
- 布局：
  1. **并排汇总表**：行=四指标 + 样本数 + 耗时 + 评审模型 + 提交时间，列=各任务，单元格内最高分行首加「▲ 最优」标记；差异超过阈值（如 0.05）的单元格着色提示。
  2. **叠加雷达图**：一张雷达图上多任务多边形叠加（上限 4 个，图例可开关单个任务）。
  3. **分组柱状图**：X 轴=四指标，每组内多任务并列柱。
  4. **逐样本联查（v2 可延后）**：按 question 对齐，看同一问题在不同评测中的分数变化——用于验证"改了检索策略后这个问题修好了没有"。
- 数据：`GET /api/v1/eval/reports?task_ids=a,b,c` 批量接口，或前端并发 `getReport` × N（建议后端给批量接口，避免 N 次往返）。

---

## 3. API 层设计

### 3.1 端点形态提议（RESTful）

Go 后端作为网关转发到 Python 评测微服务（沿用现有 `/api/v1` 前缀与统一 `{code,message,data}` 包装，前端无感）。提议端点：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/eval/datasets` | 数据集列表（微服务端已注册的数据集） |
| POST | `/api/v1/eval/datasets` | 上传数据集（multipart，json/jsonl） |
| GET | `/api/v1/eval/datasets/:id/preview` | 数据集预览（样本数、字段校验、前 N 条） |
| GET | `/api/v1/eval/judge-models` | 可用评审模型清单 |
| POST | `/api/v1/eval/tasks` | 提交评测任务（异步，返回任务骨架） |
| GET | `/api/v1/eval/tasks` | 任务列表（支持 `?status=&kb_id=&page=&page_size=`） |
| GET | `/api/v1/eval/tasks/:id` | 任务详情（含 status / stage / progress，轮询打这个） |
| POST | `/api/v1/eval/tasks/:id/cancel` | 取消任务 |
| DELETE | `/api/v1/eval/tasks/:id` | 删除任务及其报告 |
| GET | `/api/v1/eval/tasks/:id/report` | 汇总报告 + 逐样本明细分页（`?page=&page_size=&metric_lt=faithfulness:0.6&sort=`） |
| GET | `/api/v1/eval/tasks/:id/report/samples/:sampleId` | 单样本完整内容（question/answer/contexts/分数，供下钻） |
| GET | `/api/v1/eval/reports?task_ids=a,b,c` | 多任务汇总批量查询（对比视图用） |

分页/过滤放 report 接口的理由：大报告逐样本可能上千条，明细不随报告一次性全量返回（见 §5.2）。

### 3.2 新增 `api/eval.ts` 方法签名草案

```ts
// api/eval.ts —— RAGAS 评测 REST API（对接评测微服务，经 Go 网关转发）
import { request } from './client'
import type {
  EvalDataset, EvalDatasetPreview, JudgeModel,
  EvalTask, EvalTaskDetail, EvalReport, EvalSampleDetail,
  CreateEvalTaskPayload, EvalTaskListQuery, EvalReportQuery,
} from './types'

// —— 数据集 ——
export function listEvalDatasets(): Promise<EvalDataset[]>
export function uploadEvalDataset(file: File, name?: string): Promise<EvalDataset>
export function previewEvalDataset(id: string): Promise<EvalDatasetPreview>

// —— 评审模型 ——
export function listJudgeModels(): Promise<JudgeModel[]>

// —— 任务 ——
export function createEvalTask(payload: CreateEvalTaskPayload): Promise<EvalTask>
export function listEvalTasks(query?: EvalTaskListQuery): Promise<EvalTask[]>
export function getEvalTask(id: string): Promise<EvalTaskDetail>   // 轮询用
export function cancelEvalTask(id: string): Promise<unknown>
export function deleteEvalTask(id: string): Promise<unknown>

// —— 报告 ——
export function getEvalReport(id: string, query?: EvalReportQuery): Promise<EvalReport>
export function getEvalSample(taskId: string, sampleId: string): Promise<EvalSampleDetail>
export function getEvalReportsBatch(taskIds: string[]): Promise<EvalReport[]>
```

上传数据集用 `client` 的 `post` + `FormData`（参照 `api/doc.ts` 的 uploadDocument 写法，绕过 `request<T>` 的 JSON 习惯或扩展它支持 FormData——实现时对齐现有上传代码）。

### 3.3 类型定义草案（追加到 `api/types.ts`）

> 命名约定：本模块与 Python/FastAPI 对齐，**全 snake_case**，文件头部注释说明（与 `ApiKeyView` 先例一致）。

```ts
// —— RAGAS 评测（评测微服务为 FastAPI，字段全 snake_case）——

export type EvalMetric =
  | 'faithfulness'
  | 'answer_relevancy'
  | 'context_precision'
  | 'context_recall'

export type EvalTaskStatus =
  | 'pending'      // 排队中
  | 'running'      // 执行中
  | 'completed'
  | 'failed'
  | 'cancelled'

export type EvalStage = 'collecting' | 'scoring' | 'reporting' // 收集回答→RAGAS评分→生成报告

// 数据集
export interface EvalDataset {
  id: string
  name: string
  sample_count: number
  has_ground_truth: boolean      // 无 ground_truth 时禁用 context_recall
  created_at: string
}

export interface EvalDatasetPreview {
  id: string
  sample_count: number
  validation: { ok: boolean; errors: string[] }  // 结构校验结果
  samples: Array<{ question: string; ground_truth?: string }> // 前 2~3 条
}

// 评审模型
export interface JudgeModel {
  id: string
  name: string        // 展示名，如 "qwen-plus (评审)"
  provider: string
  is_default: boolean
}

// 提交评测
export interface CreateEvalTaskPayload {
  name?: string                 // 空则由后端生成
  kb_id: string
  dataset_id: string
  metrics: EvalMetric[]         // 至少 1 项
  judge_model_id: string
  options?: {
    sample_limit?: number       // 采样上限，0/缺省 = 全量
    concurrency?: number
    timeout_s?: number
    seed?: number               // 可复现
  }
}

// 任务（列表视图）
export interface EvalTask {
  id: string
  name: string
  kb_id: string
  kb_name: string
  dataset_id: string
  dataset_name: string
  metrics: EvalMetric[]
  judge_model_id: string
  status: EvalTaskStatus
  progress: { total: number; finished: number }  // 样本粒度进度
  summary?: Partial<Record<EvalMetric, number>>  // 完成后有值：四指标均值
  error_message?: string
  created_at: string
  finished_at?: string
  duration_ms?: number
}

// 任务详情（轮询返回）
export interface EvalTaskDetail extends EvalTask {
  current_stage?: EvalStage
  logs?: string[]               // 最近若干条日志（可选）
}

// 汇总报告 + 明细分页
export interface EvalReport {
  task_id: string
  summary: Partial<Record<EvalMetric, number>>   // 各指标均值
  distribution?: Partial<Record<EvalMetric, number[]>>  // 各指标逐样本分数（供直方图；量大时可改为分桶）
  sample_total: number
  samples: EvalSampleRow[]      // 当前页
  page: number
  page_size: number
}

// 明细行（列表态，文本截断由前端做）
export interface EvalSampleRow {
  id: string
  index: number
  question: string
  answer: string
  scores: Partial<Record<EvalMetric, number>>
  error?: string                // 单样本失败不阻断整体（对齐 docs/13 spec N3）
}

// 单样本完整下钻
export interface EvalSampleDetail extends EvalSampleRow {
  ground_truth?: string
  contexts: Array<{
    content: string
    filename?: string
    score?: number
  }>
  metric_details?: Partial<Record<EvalMetric, { score: number; reason?: string }>>
}

// 查询参数
export interface EvalTaskListQuery {
  status?: EvalTaskStatus
  kb_id?: string
  page?: number
  page_size?: number
}

export interface EvalReportQuery {
  page?: number
  page_size?: number
  sort_by?: EvalMetric | 'index'
  sort_order?: 'asc' | 'desc'
  metric_lt?: string            // 形如 "faithfulness:0.6" 的低分过滤
}
```

---

## 4. 状态管理

### 4.1 新增 `stores/eval.ts`（选项式，对齐 `stores/doc.ts` 风格）

**职责边界**：

| 状态 | 放哪 | 理由 |
| --- | --- | --- |
| 任务列表 + 列表筛选条件 | eval store | 跨页面保留（详情返回列表不丢筛选） |
| 当前任务详情 + 报告 | eval store（`currentTask` / `currentReport`） | 详情页轮询写这里，组件只读 |
| 表单草稿 | eval store（`draft`） | 提交失败/离开返回可恢复 |
| 下钻抽屉当前样本 | 组件本地 `ref` | 纯 UI 态，不进 store |
| 对比任务的 id 集合 | URL query 为准，store 只缓存已拉取的报告 | URL 是单一事实源，可分享 |

State 草案：

```ts
state: () => ({
  tasks: [] as EvalTask[],
  tasksLoading: false,
  filter: { status: '' as EvalTaskStatus | '', kbId: '' },
  currentTask: null as EvalTaskDetail | null,
  currentReport: null as EvalReport | null,
  reportQuery: { page: 1, page_size: 50 } as EvalReportQuery,
  draft: null as Partial<CreateEvalTaskPayload> | null,
  pollTimer: 0 as ReturnType<typeof setTimeout> | 0,
})
```

### 4.2 轮询策略（在 doc store 模式上增强）

现有 `doc.ts` 是固定 3s `setInterval`。评测任务分钟级~小时级，固定高频轮询浪费请求，做三点增强：

1. **退避间隔（`setTimeout` 链而非 `setInterval`）**：
   - 前 30s：2s 一次（刚提交，用户盯着看）
   - 30s ~ 5min：5s 一次
   - 5min 后：15s 一次
   - 用 `setTimeout` 递归调度：上一轮响应回来后再排下一轮，天然避免请求重叠（`setInterval` 在慢请求下会堆叠）。
2. **页面可见性处理**：
   - 监听 `document.visibilitychange`：hidden 时清除定时器；visible 时**立即拉一次**再按当前档位恢复定时器。避免后台标签页持续打请求（浏览器也会节流 setTimeout，主动管理更可控）。
   - 监听器在 `startPolling` 时注册、`stopPolling` 时移除，防止重复绑定。
3. **清理时机**：
   - 任务到达终态（completed/failed/cancelled）→ 自动停止，并在 completed 时顺带拉一次报告。
   - `EvalDetailView` 的 `onBeforeUnmount` → `stopPolling()`（路由切走必停）。
   - 列表页的 10s 低频轮询只在有 running 任务且页面 visible 时启用，逻辑同上复用。

伪代码形态（仅示意调度结构）：

```ts
// startPolling：先立即拉一次；每轮结束后按任务年龄选间隔排下一轮
// visibilitychange: hidden → clearTimeout；visible → 立即拉 + 恢复
// 终态 / unmount / logout → stopPolling（清 timer + 移除 visibility 监听）
```

错误韧性：单次轮询请求失败不停止轮询（网络抖动常见），连续失败 3 次才暂停并 `ElMessage.warning('进度刷新失败，已暂停自动刷新')` + 提供手动「重试刷新」按钮。

---

## 5. 交互细节

### 5.1 长任务三态处理

- **Loading（运行中）**：不用全屏 `v-loading` 遮罩（会挡住取消按钮）；用进度面板 + 局部 `v-loading`（仅摘要数据区）。进度条用 `el-progress` 的动画条纹（`:indeterminate` 风格的 striped + active）表达"还在跑"。
- **失败**：
  - 任务级失败：红色错误卡 + `error_message` + 「重新发起」（携带原 payload 预填 `/eval/new` 表单，从 store draft 恢复）。
  - 样本级失败：不标红整个任务——报告页明细表该行分数列显示 `—`，行尾带 warning 图标，下钻可见 `error` 详情；汇总卡标注「有效样本 N/M」。对齐 docs/13 spec N3 的容错语义。
- **取消**：`ElMessageBox.confirm`（与删除知识库同一确认模式）→ cancel 接口 → 状态 tag 变 `cancelled`（warning 色），已产出的部分报告若后端保留则允许查看，UI 上标注「已取消，结果为部分数据」。

### 5.2 大数据集报告的分页与虚拟滚动

双轨策略：

- **明细表交互（排序/低分过滤/翻页）走后端分页**（§3.1 report 接口的 query 参数）：过滤与排序必须在全量数据上做，前端分页会漏数据。
- **当前页渲染用虚拟滚动**：`el-table-v2`（Element Plus 虚拟化表格），page_size 默认 50、可选 100/200。虚拟滚动解决单页 200 行 × 多列文本的渲染压力；后端分页解决全量数据的排序/过滤正确性。
- 不选纯前端全量 + `el-table-v2` 一次性渲染：上千条样本含 answer 长文本，单条 KB 级，全量 JSON 数 MB，首屏不可接受。
- 若实现期发现 `el-table-v2` 与现有样式融合成本高，降级方案：`el-table` + 后端分页（50/页），虚拟滚动作为性能优化项后补。

### 5.3 多次评测对比视图

见 §2.5。交互补充：

- 对比入口防呆：勾选的任务必须都是 `completed`，否则 checkbox 禁用并 tooltip「仅已完成任务可对比」。
- 雷达图叠加超过 4 个任务时禁止继续勾选（视觉上过密即失效）。
- 对比页提供「导出」按钮（v2）：将对比汇总表导出 CSV——评测结论常需贴进文档/周报。

### 5.4 其他交互约定

- 所有异步操作按钮带 `:loading`（防重复提交），模式同 `KbListView.submit`。
- 时间显示统一 `new Date(x).toLocaleString()`（与现有页面一致，不引入 dayjs）。
- 分数展示统一 `score.toFixed(3)`；色阶规则集中在 `utils/evalScore.ts`（score → 颜色/文案的纯函数，便于测试）。
- 路由离开 `/eval/new` 且有未提交表单时，可用 `onBeforeRouteLeave` 提示（v2，非首版必须）。

---

## 6. 视觉风格落地清单

1. **颜色**：全部走 `--br-*` 变量。指标色阶语义色允许引入 success/warning/danger，但优先复用 Element Plus 的 `--el-color-success/warning/danger`（已被主题覆盖接管，暗色下自动协调）。ECharts option 颜色从 CSS 变量读取（`getComputedStyle(document.documentElement).getPropertyValue('--br-primary')`），暗色切换时重算。
2. **卡片**：汇总卡、任务卡沿用 `KbListView` 的双层卡片规则：`--br-radius-lg`、`--br-shadow-sm`、hover `translateY(-3px)` + `--br-shadow-md` + 边框变 `--br-primary-soft-2`。
3. **页面骨架**：`page-head` + `page-title`（22px/700/-0.02em）+ 右上主按钮；内容 padding `24px 28px`。
4. **圆角与动效**：组件内自定义元素用 `--br-radius-md/pill`；过渡一律 `var(--br-transition-fast/base)` + `--br-ease`，禁止默认 ease。
5. **字体**：数值（分数、进度）用 `--br-font-mono` 等宽展示，表格内分数对齐更整齐；其余 `--br-font-sans`。
6. **图标**：`@element-plus/icons-vue`：侧边栏 `DataAnalysis`，进度 `Loading`，指标卡可用 `Aim/Finished/Search/Collection` 一组语义图标，失败 `WarningFilled`。
7. **暗色**：不新增任何 `html.dark` 覆盖之外的硬编码色；图表、进度条、分数色阶都需在暗色下走查一遍。

---

## 7. 文件落位与实施顺序建议

新增文件（实现阶段）：

```
frontend/src/
├── api/eval.ts                    # §3 API 模块
├── api/types.ts                   # 追加 §3.3 类型
├── stores/eval.ts                 # §4 store
├── utils/evalScore.ts             # 分数→色阶/文案纯函数
├── components/eval/
│   ├── EvalChart.vue              # vue-echarts 薄封装（主题对接）
│   ├── EvalTaskCard.vue           # 列表任务卡
│   ├── MetricSummaryCards.vue     # 四指标汇总卡
│   ├── EvalSampleTable.vue        # 明细表（el-table-v2）
│   └── EvalSampleDrawer.vue       # 单样本下钻抽屉
├── views/eval/
│   ├── EvalListView.vue
│   ├── EvalNewView.vue
│   ├── EvalDetailView.vue
│   └── EvalCompareView.vue
└── router/index.ts                # 注册 /eval 路由组
components/AppLayout.vue           # 侧边栏加「评测中心」+ default-active 修正
package.json                       # 新增依赖 echarts + vue-echarts
```

建议实施顺序：

1. **P0 主链路**：types + api/eval + store + 路由/菜单 + ListView + NewView + DetailView（进度 + 汇总卡 + 明细表 + 下钻，先 `el-table` 后端分页）
2. **P1 可视化**：ECharts 引入 + 雷达图/分布图 + 暗色对接
3. **P2 增强**：对比视图、`el-table-v2` 虚拟滚动、CSV 导出、表单离开提示

依赖后端契约确认项（实现前需与微服务对齐）：进度字段形态（样本数 vs 百分比）、`current_stage` 枚举、报告分页参数、单样本 reason 字段是否返回、judge-models 清单来源。
