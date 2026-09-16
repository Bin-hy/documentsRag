// 评测中心状态：任务列表/详情/报告/表单草稿 + 链式退避轮询
// 轮询策略（frontend-design §4.2，在 stores/doc.ts 模式上增强）：
//  1. setTimeout 链式退避：前 30s 每 2s → 5min 内每 5s → 之后每 15s；上一轮响应回来后再排下一轮，避免慢请求堆叠
//  2. visibilitychange：页面隐藏清除定时器，恢复可见时立即拉一次再按当前档位恢复
//  3. 清理时机：任务终态自动停止（completed 顺带拉报告）、详情页 unmount 必停
//  4. 错误韧性：单次失败不停，连续失败 3 次暂停并提示，提供手动重试
// 列表页另有 10s 低频轮询，仅在有运行中任务且页面可见时启用。
import { defineStore } from 'pinia'
import { ElMessage } from 'element-plus'
import * as evalApi from '../api/eval'
import type {
  CreateEvalTaskPayload,
  EvalDataset,
  EvalReport,
  EvalReportQuery,
  EvalTask,
  EvalTaskStatus,
  JudgeModel,
} from '../api/types'
import { isActiveStatus } from '../utils/evalScore'

// 详情页退避档位（按任务轮询年龄选择）
const BACKOFF_FAST_MS = 2_000 // 前 30s：刚提交用户盯着看
const BACKOFF_MID_MS = 5_000 // 30s ~ 5min
const BACKOFF_SLOW_MS = 15_000 // 5min 后
const BACKOFF_FAST_WINDOW_MS = 30_000
const BACKOFF_MID_WINDOW_MS = 5 * 60_000
const MAX_POLL_FAILURES = 3 // 连续失败 3 次暂停
const LIST_POLL_MS = 10_000 // 列表页低频轮询
const TASKS_PAGE_SIZE = 100 // 契约分页上限；评测任务量级下一次性拉全量做客户端分组筛选

// visibilitychange 监听模块级单例（store 为单例语义；详情/列表两路轮询共享一个监听器）
let visHandler: (() => void) | null = null

export const useEvalStore = defineStore('eval', {
  state: () => ({
    tasks: [] as EvalTask[],
    tasksTotal: 0,
    tasksLoading: false,
    // 列表筛选：'active' 为前端分组（pending+collecting+evaluating），其余直传后端契约状态
    filter: { status: '' as '' | 'active' | EvalTaskStatus, kbId: '', keyword: '' },
    datasets: [] as EvalDataset[],
    datasetsLoading: false,
    judgeModels: [] as JudgeModel[],
    currentTask: null as EvalTask | null,
    currentReport: null as EvalReport | null,
    reportQuery: { page: 1, page_size: 50 } as EvalReportQuery,
    reportLoading: false,
    draft: null as Partial<CreateEvalTaskPayload> | null, // 发起表单草稿（提交失败/离开返回可恢复）
    // 详情页轮询
    pollTimer: 0 as ReturnType<typeof setTimeout> | 0,
    pollTaskId: '',
    pollStartedAt: 0,
    pollFailCount: 0,
    pollPaused: false,
    // 列表页低频轮询
    listPollTimer: 0 as ReturnType<typeof setTimeout> | 0,
    listPolling: false,
  }),

  getters: {
    hasActiveTasks: (state) => state.tasks.some((t) => isActiveStatus(t.status)),
    /** 客户端筛选后的任务列表（状态分组 + 知识库 + 关键词） */
    filteredTasks: (state) => {
      const { status, kbId, keyword } = state.filter
      const kw = keyword.trim().toLowerCase()
      return state.tasks.filter((t) => {
        if (status === 'active' && !isActiveStatus(t.status)) return false
        if (status && status !== 'active' && t.status !== status) return false
        if (kbId && t.kb_id !== kbId) return false
        if (kw && !t.name.toLowerCase().includes(kw)) return false
        return true
      })
    },
    datasetName: (state) => (id: string) =>
      state.datasets.find((d) => d.id === id)?.name ?? '',
  },

  actions: {
    // —— 列表与基础数据 ——

    async loadTasks() {
      this.tasksLoading = true
      try {
        const res = await evalApi.listEvalTasks({ page: 1, page_size: TASKS_PAGE_SIZE })
        this.tasks = res?.items ?? []
        this.tasksTotal = res?.total ?? 0
      } finally {
        this.tasksLoading = false
      }
    },

    async loadDatasets() {
      this.datasetsLoading = true
      try {
        const res = await evalApi.listEvalDatasets()
        this.datasets = res?.items ?? []
      } finally {
        this.datasetsLoading = false
      }
    },

    async loadJudgeModels() {
      const res = await evalApi.listJudgeModels()
      this.judgeModels = res?.items ?? []
    },

    // —— 任务操作（反馈消息由调用方统一 ElMessage）——

    async createTask(payload: CreateEvalTaskPayload, idempotencyKey?: string): Promise<EvalTask> {
      const task = await evalApi.createEvalTask(payload, idempotencyKey)
      this.draft = null // 提交成功清草稿
      return task
    },

    async cancelTask(id: string) {
      const t = await evalApi.cancelEvalTask(id)
      if (this.currentTask?.id === id) this.currentTask = t
      await this.loadTasks()
    },

    async deleteTask(id: string) {
      await evalApi.deleteEvalTask(id)
      this.tasks = this.tasks.filter((t) => t.id !== id)
      this.tasksTotal = Math.max(0, this.tasksTotal - 1)
    },

    /** 拉取任务详情（详情页首屏与轮询共用） */
    async fetchTask(id: string): Promise<EvalTask> {
      const t = await evalApi.getEvalTask(id)
      this.currentTask = t
      return t
    },

    // —— 报告 ——

    async loadReport(taskId: string, query?: EvalReportQuery) {
      this.reportLoading = true
      try {
        this.reportQuery = { ...this.reportQuery, ...query }
        this.currentReport = await evalApi.getEvalReport(taskId, this.reportQuery)
      } finally {
        this.reportLoading = false
      }
    },

    // —— 表单草稿 ——

    saveDraft(d: Partial<CreateEvalTaskPayload>) {
      this.draft = { ...d }
    },

    /** 从失败/已取消任务带原参数回填草稿（「重新发起」） */
    draftFromTask(t: EvalTask) {
      this.draft = {
        name: t.name,
        dataset_id: t.dataset_id,
        kb_id: t.kb_id,
        judge_model: t.judge_model,
        metrics: [...t.metrics],
      }
    },

    // —— 详情页轮询（链式退避 + 可见性管理）——

    /** 启动轮询：立即拉一次，之后按退避档位 setTimeout 链式调度 */
    startPolling(taskId: string) {
      this.stopPolling()
      this.pollTaskId = taskId
      this.pollStartedAt = Date.now()
      this.pollFailCount = 0
      this.pollPaused = false
      this.ensureVisibilityListener()
      void this.pollTick()
    },

    /** 手动重试刷新（连续失败暂停后由 UI 触发） */
    resumePolling() {
      if (!this.pollTaskId) return
      this.pollPaused = false
      this.pollFailCount = 0
      this.ensureVisibilityListener()
      void this.pollTick()
    },

    stopPolling() {
      if (this.pollTimer) {
        clearTimeout(this.pollTimer)
        this.pollTimer = 0
      }
      this.pollTaskId = ''
      this.pollFailCount = 0
      this.pollPaused = false
      this.maybeRemoveVisibilityListener()
    },

    pollDelayMs(): number {
      const elapsed = Date.now() - this.pollStartedAt
      if (elapsed < BACKOFF_FAST_WINDOW_MS) return BACKOFF_FAST_MS
      if (elapsed < BACKOFF_MID_WINDOW_MS) return BACKOFF_MID_MS
      return BACKOFF_SLOW_MS
    },

    async pollTick() {
      const id = this.pollTaskId
      if (!id || this.pollPaused || document.hidden) return
      try {
        const t = await evalApi.getEvalTask(id)
        if (this.pollTaskId !== id) return // 期间已切换/停止
        this.currentTask = t
        this.pollFailCount = 0
        if (!isActiveStatus(t.status)) {
          // 终态：自动停止；有报告则顺带拉一次
          this.stopPolling()
          if (t.status === 'completed' || t.report_ready) {
            this.loadReport(id, { page: 1 }).catch(() => {
              /* 报告拉取失败不阻断状态展示，用户可手动刷新 */
            })
          }
          return
        }
      } catch {
        // 网络抖动不停止轮询；连续失败达上限才暂停
        if (this.pollTaskId !== id) return
        this.pollFailCount++
        if (this.pollFailCount >= MAX_POLL_FAILURES) {
          this.pollPaused = true
          if (this.pollTimer) {
            clearTimeout(this.pollTimer)
            this.pollTimer = 0
          }
          ElMessage.warning('进度刷新失败，已暂停自动刷新')
          return
        }
      }
      this.scheduleNextPoll()
    },

    scheduleNextPoll() {
      if (!this.pollTaskId || this.pollPaused) return
      this.pollTimer = setTimeout(() => {
        this.pollTimer = 0
        void this.pollTick()
      }, this.pollDelayMs())
    },

    // —— 列表页 10s 低频轮询 ——

    startListPolling() {
      if (this.listPolling) return
      this.listPolling = true
      this.ensureVisibilityListener()
      void this.listTick()
    },

    stopListPolling() {
      this.listPolling = false
      if (this.listPollTimer) {
        clearTimeout(this.listPollTimer)
        this.listPollTimer = 0
      }
      this.maybeRemoveVisibilityListener()
    },

    async listTick() {
      if (!this.listPolling || document.hidden) return
      try {
        await this.loadTasks()
      } catch {
        /* 列表低频轮询失败静默，下一轮重试 */
      }
      if (!this.listPolling) return
      if (!this.hasActiveTasks) {
        this.stopListPolling() // 无活动任务自动停止
        return
      }
      this.listPollTimer = setTimeout(() => {
        this.listPollTimer = 0
        void this.listTick()
      }, LIST_POLL_MS)
    },

    // —— 页面可见性：hidden 清定时器；visible 立即拉一次再恢复 ——

    ensureVisibilityListener() {
      if (visHandler) return
      visHandler = () => {
        const store = useEvalStore()
        if (document.hidden) {
          if (store.pollTimer) {
            clearTimeout(store.pollTimer)
            store.pollTimer = 0
          }
          if (store.listPollTimer) {
            clearTimeout(store.listPollTimer)
            store.listPollTimer = 0
          }
        } else {
          if (store.pollTaskId && !store.pollPaused) void store.pollTick()
          if (store.listPolling) void store.listTick()
        }
      }
      document.addEventListener('visibilitychange', visHandler)
    },

    maybeRemoveVisibilityListener() {
      if (visHandler && !this.pollTaskId && !this.listPolling) {
        document.removeEventListener('visibilitychange', visHandler)
        visHandler = null
      }
    },
  },
})
