<script setup lang="ts">
// 评测中心 · 任务列表：状态筛选 / 知识库过滤 / 关键词搜索 / 批量勾选对比 / 删除
// 运行中任务由 store 做 10s 低频轮询（仅在有活动任务且页面可见时）
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { DataAnalysis, Plus } from '@element-plus/icons-vue'
import { useEvalStore } from '../../stores/eval'
import { useKbStore } from '../../stores/kb'
import type { EvalTask } from '../../api/types'
import EvalTaskCard from '../../components/eval/EvalTaskCard.vue'

const router = useRouter()
const evalStore = useEvalStore()
const kbStore = useKbStore()

// 批量对比勾选（防呆：仅 completed 可选，上限 4 个）
const checkedIds = ref<string[]>([])

const COMPARE_MAX = 4

function checkDisabledReason(t: EvalTask): string {
  if (t.status !== 'completed') return '仅已完成任务可对比'
  if (!checkedIds.value.includes(t.id) && checkedIds.value.length >= COMPARE_MAX) {
    return `最多对比 ${COMPARE_MAX} 个任务`
  }
  return ''
}

function toggleCheck(t: EvalTask, checked: boolean) {
  if (checked) {
    if (checkedIds.value.length >= COMPARE_MAX) {
      ElMessage.warning(`最多对比 ${COMPARE_MAX} 个任务`)
      return
    }
    checkedIds.value = [...checkedIds.value, t.id]
  } else {
    checkedIds.value = checkedIds.value.filter((id) => id !== t.id)
  }
}

const canCompare = computed(() => checkedIds.value.length >= 2 && checkedIds.value.length <= COMPARE_MAX)

const compareTip = computed(() =>
  checkedIds.value.length < 2 ? '至少勾选 2 个已完成任务' : '',
)

function goCompare() {
  if (!canCompare.value) return
  router.push({ path: '/eval/compare', query: { ids: checkedIds.value.join(',') } })
}

const kbName = (id?: string) => (id ? (kbStore.kbs.find((k) => k.ID === id)?.Name ?? '') : '')

async function remove(t: EvalTask) {
  try {
    await ElMessageBox.confirm(
      `确定删除评测任务「${t.name}」吗？其报告将一并删除。`,
      '删除确认',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }
  try {
    await evalStore.deleteTask(t.id)
    checkedIds.value = checkedIds.value.filter((id) => id !== t.id)
    ElMessage.success('已删除')
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

// 有运行中任务时启动列表低频轮询；全部终态自动停止（store 内判定）
watch(
  () => evalStore.hasActiveTasks,
  (has) => {
    if (has) evalStore.startListPolling()
  },
)

onMounted(async () => {
  evalStore.loadTasks().catch((e) => ElMessage.error('加载评测任务失败：' + (e as Error).message))
  evalStore.loadDatasets().catch(() => {})
  kbStore.load().catch(() => {})
  // 首屏若已有活动任务则立即开启轮询
  watch(
    () => evalStore.tasksLoading,
    (loading) => {
      if (!loading && evalStore.hasActiveTasks) evalStore.startListPolling()
    },
    { once: true },
  )
})

onBeforeUnmount(() => {
  evalStore.stopListPolling()
})
</script>

<template>
  <div class="eval-page">
    <div class="page-head">
      <h2 class="page-title">评测中心</h2>
      <el-button type="primary" :icon="Plus" @click="router.push('/eval/new')">发起评测</el-button>
    </div>

    <div class="filter-bar">
      <el-radio-group v-model="evalStore.filter.status">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="active">进行中</el-radio-button>
        <el-radio-button value="completed">已完成</el-radio-button>
        <el-radio-button value="failed">失败</el-radio-button>
        <el-radio-button value="canceled">已取消</el-radio-button>
      </el-radio-group>
      <el-select
        v-model="evalStore.filter.kbId"
        placeholder="全部知识库"
        clearable
        class="filter-kb"
      >
        <el-option v-for="kb in kbStore.kbs" :key="kb.ID" :label="kb.Name" :value="kb.ID" />
      </el-select>
      <el-input
        v-model="evalStore.filter.keyword"
        placeholder="搜索任务名"
        clearable
        class="filter-keyword"
      />
    </div>

    <el-empty
      v-if="!evalStore.tasksLoading && evalStore.tasks.length === 0"
      description="还没有评测任务，点击右上角发起第一次评测"
    >
      <template #image>
        <el-icon :size="56" class="empty-icon"><DataAnalysis /></el-icon>
      </template>
    </el-empty>
    <el-empty
      v-else-if="!evalStore.tasksLoading && evalStore.filteredTasks.length === 0"
      description="没有符合筛选条件的任务"
    />

    <el-row :gutter="16" v-loading="evalStore.tasksLoading && evalStore.tasks.length === 0">
      <el-col v-for="t in evalStore.filteredTasks" :key="t.id" :xs="24" :sm="12" :lg="8" :xl="6">
        <EvalTaskCard
          :task="t"
          :kb-name="kbName(t.kb_id)"
          :dataset-name="evalStore.datasetName(t.dataset_id)"
          :checked="checkedIds.includes(t.id)"
          :check-disabled-reason="checkDisabledReason(t)"
          @enter="router.push(`/eval/${t.id}`)"
          @remove="remove(t)"
          @toggle-check="(c: boolean) => toggleCheck(t, c)"
        />
      </el-col>
    </el-row>

    <!-- 批量对比浮动操作条 -->
    <transition name="el-fade-in">
      <div v-if="checkedIds.length" class="compare-bar">
        <span class="compare-bar-text">已选 {{ checkedIds.length }} 项</span>
        <el-tooltip :content="compareTip" :disabled="!compareTip" placement="top">
          <span>
            <el-button type="primary" :disabled="!canCompare" @click="goCompare">
              对比所选（{{ checkedIds.length }}）
            </el-button>
          </span>
        </el-tooltip>
        <el-button text @click="checkedIds = []">清空</el-button>
      </div>
    </transition>
  </div>
</template>

<style scoped>
.eval-page {
  padding: 24px 28px 96px; /* 底部留出浮动操作条空间 */
}

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.filter-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 20px;
}

.filter-kb {
  width: 200px;
}

.filter-keyword {
  width: 220px;
}

.empty-icon {
  color: var(--br-text-tertiary);
}

.compare-bar {
  position: fixed;
  left: 50%;
  bottom: 28px;
  transform: translateX(calc(-50% + 108px)); /* 补偿侧边栏宽度，相对内容区居中 */
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 18px;
  border-radius: var(--br-radius-pill);
  background: var(--br-bg-elevated);
  border: 1px solid var(--br-border-strong);
  box-shadow: var(--br-shadow-lg);
  z-index: 20;
}

.compare-bar-text {
  font-size: 13px;
  color: var(--br-text-secondary);
  font-variant-numeric: tabular-nums;
}
</style>
