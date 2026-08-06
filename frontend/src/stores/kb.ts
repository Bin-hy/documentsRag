// 知识库状态：列表加载与 CRUD
import { defineStore } from 'pinia'
import * as kbApi from '../api/kb'
import type { Kb } from '../api/types'

export const useKbStore = defineStore('kb', {
  state: () => ({
    kbs: [] as Kb[],
    loading: false,
  }),

  actions: {
    async load() {
      this.loading = true
      try {
        this.kbs = (await kbApi.listKbs()) ?? []
      } finally {
        this.loading = false
      }
    },

    async create(name: string, description: string) {
      await kbApi.createKb(name, description)
      await this.load()
    },

    async update(id: string, name: string, description: string) {
      await kbApi.updateKb(id, name, description)
      await this.load()
    },

    async remove(id: string) {
      await kbApi.deleteKb(id)
      await this.load()
    },
  },
})
