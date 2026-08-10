<script setup lang="ts">
// 知识库列表：创建 / 编辑 / 删除 / 进入详情
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Folder, Edit, Delete } from '@element-plus/icons-vue'
import { useKbStore } from '../stores/kb'
import type { Kb } from '../api/types'

const router = useRouter()
const kbStore = useKbStore()

const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const editingId = ref('')
const formRef = ref<FormInstance>()
const form = ref({ name: '', description: '' })

const rules: FormRules = {
  name: [{ required: true, message: '请输入知识库名称', trigger: 'blur' }],
}

function openCreate() {
  dialogMode.value = 'create'
  editingId.value = ''
  form.value = { name: '', description: '' }
  dialogVisible.value = true
}

function openEdit(kb: Kb) {
  dialogMode.value = 'edit'
  editingId.value = kb.ID
  form.value = { name: kb.Name, description: kb.Description }
  dialogVisible.value = true
}

async function submit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  try {
    if (dialogMode.value === 'create') {
      await kbStore.create(form.value.name, form.value.description)
      ElMessage.success('知识库创建成功')
    } else {
      await kbStore.update(editingId.value, form.value.name, form.value.description)
      ElMessage.success('知识库已更新')
    }
    dialogVisible.value = false
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

async function remove(kb: Kb) {
  try {
    await ElMessageBox.confirm(
      `确定删除知识库「${kb.Name}」吗？其下文档与向量数据将一并删除。`,
      '删除确认',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }
  try {
    await kbStore.remove(kb.ID)
    ElMessage.success('已删除')
  } catch (err) {
    ElMessage.error((err as Error).message)
  }
}

function enter(kb: Kb) {
  router.push(`/kb/${kb.ID}`)
}

onMounted(() => {
  kbStore.load()
})
</script>

<template>
  <div class="kb-page">
    <div class="page-head">
      <h2 class="page-title">知识库</h2>
      <el-button type="primary" :icon="Plus" @click="openCreate">新建知识库</el-button>
    </div>

    <el-empty v-if="!kbStore.loading && kbStore.kbs.length === 0" description="还没有知识库，点击右上角创建第一个知识库" />

    <el-row :gutter="16" v-loading="kbStore.loading">
      <el-col v-for="kb in kbStore.kbs" :key="kb.ID" :xs="24" :sm="12" :md="8" :lg="6">
        <div class="kb-card" @click="enter(kb)">
          <div class="kb-card-head">
            <span class="kb-icon">
              <el-icon :size="18"><Folder /></el-icon>
            </span>
            <span class="kb-name br-text-ellipsis">{{ kb.Name }}</span>
          </div>
          <p class="kb-desc">{{ kb.Description || '暂无描述' }}</p>
          <div class="kb-card-foot">
            <span class="br-muted kb-date">{{ new Date(kb.CreatedAt).toLocaleDateString() }}</span>
            <div class="kb-actions" @click.stop>
              <el-button size="small" text :icon="Edit" @click="openEdit(kb)">编辑</el-button>
              <el-button size="small" text type="danger" :icon="Delete" @click="remove(kb)">删除</el-button>
            </div>
          </div>
        </div>
      </el-col>
    </el-row>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'create' ? '新建知识库' : '编辑知识库'"
      width="420px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="70px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="知识库名称" maxlength="64" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="简要描述（可选）" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.kb-page {
  padding: 24px 28px;
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

/* 知识库卡片：双贝泽尔结构（外层壳 + 内芯），去默认边框+阴影通用卡 */
.kb-card {
  margin-bottom: 16px;
  padding: 16px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
  cursor: pointer;
  transition:
    transform var(--br-transition-base),
    box-shadow var(--br-transition-base),
    border-color var(--br-transition-base);
}

.kb-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--br-shadow-md);
  border-color: var(--br-primary-soft-2);
}

.kb-card:active {
  transform: translateY(-1px) scale(0.995);
}

.kb-card-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.kb-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border-radius: 10px;
  background: var(--br-primary-soft);
  color: var(--br-primary);
  transition: background-color var(--br-transition-fast);
}

.kb-card:hover .kb-icon {
  background: var(--br-primary);
  color: #fff;
}

.kb-name {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.kb-desc {
  margin: 0 0 14px;
  min-height: 20px;
  color: var(--br-text-secondary);
  font-size: 13px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.kb-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
  border-top: 1px solid var(--br-border);
}

.kb-date {
  font-size: 12px;
}
</style>
