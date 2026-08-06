// 文档与任务状态：列表加载、上传、删除、失败详情与自动轮询
// 任务状态随文档列表返回（Document.Status），失败详情按需查任务接口。
import { defineStore } from 'pinia'
import * as docApi from '../api/doc'
import * as taskApi from '../api/task'
import type { Document, Task } from '../api/types'

const POLL_INTERVAL_MS = 3000

export const useDocStore = defineStore('doc', {
  state: () => ({
    documents: [] as Document[],
    tasks: [] as Task[], // 失败文档的错误详情缓存
    loading: false,
    kbId: '',
    pollTimer: 0 as ReturnType<typeof setInterval> | 0,
    uploading: false,
  }),

  getters: {
    hasActiveTasks: (state) =>
      state.documents.some((d) => d.Status === 'pending' || d.Status === 'processing'),
  },

  actions: {
    async load(kbId: string) {
      this.kbId = kbId
      this.loading = true
      try {
        this.documents = await docApi.listDocuments(kbId)
      } finally {
        this.loading = false
      }
    },

    /** 上传多个文件，返回每个文件的文件名（失败时抛错由调用方提示） */
    async upload(files: File[]) {
      this.uploading = true
      try {
        for (const file of files) {
          await docApi.uploadDocument(this.kbId, file)
        }
      } finally {
        this.uploading = false
      }
      await this.load(this.kbId)
      this.startPolling()
    },

    async remove(docId: string) {
      await docApi.deleteDocument(docId)
      await this.load(this.kbId)
    },

    /** 获取失败任务详情（错误原因） */
    async fetchTask(taskId: string): Promise<Task | undefined> {
      try {
        const t = await taskApi.getTask(taskId)
        this.tasks = [...this.tasks.filter((x) => x.ID !== t.ID), t]
        return t
      } catch {
        return undefined
      }
    },

    async retry(taskId: string) {
      await taskApi.retryTask(taskId)
      await this.load(this.kbId)
      this.startPolling()
    },

    /** 启动轮询：有活动任务时每 3s 刷新文档列表，全部完成自动停止 */
    startPolling() {
      if (this.pollTimer) return
      this.pollTimer = setInterval(async () => {
        if (this.kbId) {
          await this.load(this.kbId)
        }
        if (!this.hasActiveTasks && this.pollTimer) {
          clearInterval(this.pollTimer)
          this.pollTimer = 0
        }
      }, POLL_INTERVAL_MS)
    },

    stopPolling() {
      if (this.pollTimer) {
        clearInterval(this.pollTimer)
        this.pollTimer = 0
      }
    },
  },
})
