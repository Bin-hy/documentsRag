<script setup lang="ts">
// 知识库详情：信息头 + 上传面板 + 文档/任务列表
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useDocStore } from '../stores/doc'
import UploadPanel from '../components/UploadPanel.vue'
import TaskList from '../components/TaskList.vue'

const route = useRoute()
const router = useRouter()
const docStore = useDocStore()

const kbId = route.params.id as string
const kbName = ref('')
const kbDescription = ref('')

onMounted(async () => {
  // 知识库信息：优先取 store（列表页已加载）；直接刷新时兜底加载
  const { useKbStore } = await import('../stores/kb')
  const kbStore = useKbStore()
  if (kbStore.kbs.length === 0) {
    await kbStore.load()
  }
  const kb = kbStore.kbs.find((k) => k.ID === kbId)
  if (!kb) {
    router.replace('/kb')
    return
  }
  kbName.value = kb.Name
  kbDescription.value = kb.Description
  docStore.load(kbId)
  docStore.startPolling()
})

onUnmounted(() => {
  docStore.stopPolling()
})
</script>

<template>
  <div class="kb-detail">
    <div class="detail-head">
      <el-button text :icon="ArrowLeft" @click="router.push('/kb')">返回</el-button>
      <div class="detail-info">
        <h2 class="detail-name">{{ kbName }}</h2>
        <p v-if="kbDescription" class="detail-desc">{{ kbDescription }}</p>
      </div>
    </div>

    <div class="detail-body">
      <UploadPanel :kb-id="kbId" />

      <div class="doc-section">
        <div class="doc-head">
          <h3 class="doc-title">文档与任务（{{ docStore.documents.length }}）</h3>
          <span v-if="docStore.hasActiveTasks" class="br-muted">任务处理中，列表自动刷新…</span>
        </div>
        <TaskList :documents="docStore.documents" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.kb-detail {
  padding: 16px 24px;
}

.detail-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 16px;
}

.detail-info {
  flex: 1;
}

.detail-name {
  margin: 0;
  font-size: 20px;
}

.detail-desc {
  margin: 4px 0 0;
  color: var(--br-text-secondary);
  font-size: 13px;
}

.doc-section {
  margin-top: 20px;
}

.doc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.doc-title {
  margin: 0;
  font-size: 15px;
}
</style>
