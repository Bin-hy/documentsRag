<script setup lang="ts">
// 发起评测：独立页表单（三区块：评测对象 / 评测指标 / 评审模型与执行参数）+ 吸底操作条
// 表单草稿（除上传文件外）存 store，提交失败/中途离开再回来可恢复
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules, type UploadFile } from 'element-plus'
import { Collection, Upload } from '@element-plus/icons-vue'
import { useEvalStore } from '../../stores/eval'
import { useKbStore } from '../../stores/kb'
import { previewEvalDataset, uploadEvalDataset } from '../../api/eval'
import type { EvalDataset, EvalDatasetPreview, EvalMetric } from '../../api/types'
import { METRIC_META, METRIC_ORDER } from '../../utils/evalScore'

const router = useRouter()
const evalStore = useEvalStore()
const kbStore = useKbStore()

const formRef = ref<FormInstance>()
const form = reactive({
  name: '',
  kb_id: '',
  dataset_id: '',
  metrics: [...METRIC_ORDER] as EvalMetric[],
  judge_model: '',
  sample_concurrency: 4,
})
const nameTouched = ref(false) // 名称被手动改过则不再自动生成
const submitting = ref(false)
const uploading = ref(false)
const preview = ref<EvalDatasetPreview | null>(null)
const previewLoading = ref(false)
// 幂等键：表单会话内稳定（防双击/网络重试重复提交），提交成功后重新生成
const idempotencyKey = ref(crypto.randomUUID())

const rules: FormRules = {
  dataset_id: [{ required: true, message: '请选择评测数据集', trigger: 'change' }],
  metrics: [
    {
      type: 'array',
      required: true,
      min: 1,
      message: '至少选择一项评测指标',
      trigger: 'change',
    },
  ],
}

const selectedDataset = computed<EvalDataset | undefined>(() =>
  evalStore.datasets.find((d) => d.id === form.dataset_id),
)

// 无标准答案（reference/ground_truth）的数据集禁用 context_recall
const recallDisabled = computed(() => (selectedDataset.value?.with_reference_count ?? 0) === 0)

watch(recallDisabled, (disabled) => {
  if (disabled) {
    form.metrics = form.metrics.filter((m) => m !== 'context_recall')
  }
})

// 任务名自动生成：{知识库名}-{日期}（手动改过后不再覆盖）
watch(
  () => form.kb_id,
  (kbId) => {
    if (nameTouched.value) return
    const kb = kbStore.kbs.find((k) => k.ID === kbId)
    const d = new Date()
    const date = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
    form.name = kb ? `${kb.Name}-${date}` : ''
  },
)

// 选择数据集 → 拉预览（样本数、字段校验、前 2 条）
watch(
  () => form.dataset_id,
  async (id) => {
    preview.value = null
    if (!id) return
    previewLoading.value = true
    try {
      preview.value = await previewEvalDataset(id, 2)
    } catch (e) {
      ElMessage.error('加载数据集预览失败：' + (e as Error).message)
    } finally {
      previewLoading.value = false
    }
  },
)

// 表单草稿：变化即存 store（上传文件除外），提交成功后 store 清空
watch(
  form,
  () => {
    evalStore.saveDraft({
      name: form.name || undefined,
      kb_id: form.kb_id || undefined,
      dataset_id: form.dataset_id || undefined,
      metrics: [...form.metrics],
      judge_model: form.judge_model || undefined,
      sample_concurrency: form.sample_concurrency,
    })
  },
  { deep: true },
)

const DATASET_MAX_MB = 10

/** 上传新数据集：前端预检（大小 + 首条结构），通过即上传并选中 */
async function handleUpload(file: UploadFile) {
  const raw = file.raw
  if (!raw) return
  const name = raw.name.toLowerCase()
  if (!name.endsWith('.json') && !name.endsWith('.jsonl')) {
    ElMessage.error('仅支持 .json / .jsonl 数据集文件')
    return
  }
  if (raw.size > DATASET_MAX_MB * 1024 * 1024) {
    ElMessage.error(`数据集文件不能超过 ${DATASET_MAX_MB}MB`)
    return
  }
  // 首条结构预校验：jsonl 取首行，json 取 samples[0]
  try {
    const text = await raw.text()
    let first: unknown
    if (name.endsWith('.jsonl')) {
      const line = text.split('\n').find((l) => l.trim())
      first = line ? JSON.parse(line) : null
    } else {
      const parsed = JSON.parse(text)
      first = Array.isArray(parsed?.samples) ? parsed.samples[0] : null
    }
    if (!first || typeof (first as { question?: unknown }).question !== 'string') {
      ElMessage.error('数据集结构不合法：每条样本需包含 question 字段')
      return
    }
  } catch {
    ElMessage.error('数据集文件解析失败，请检查 JSON/JSONL 格式')
    return
  }
  uploading.value = true
  try {
    const ds = await uploadEvalDataset(raw)
    ElMessage.success(`数据集「${ds.name}」上传成功`)
    for (const w of ds.validation?.warnings ?? []) {
      ElMessage.warning(w)
    }
    await evalStore.loadDatasets()
    form.dataset_id = ds.id
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    uploading.value = false
  }
}

async function submit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    const task = await evalStore.createTask(
      {
        name: form.name || undefined,
        dataset_id: form.dataset_id,
        kb_id: form.kb_id || undefined,
        judge_model: form.judge_model || undefined,
        metrics: [...form.metrics],
        sample_concurrency: form.sample_concurrency,
      },
      idempotencyKey.value,
    )
    idempotencyKey.value = crypto.randomUUID() // 成功一次后换新键
    ElMessage.success('评测任务已提交')
    router.push(`/eval/${task.id}`)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  kbStore.load().catch(() => {})
  evalStore.loadDatasets().catch((e) => ElMessage.error('加载数据集失败：' + (e as Error).message))
  try {
    await evalStore.loadJudgeModels()
    const def = evalStore.judgeModels.find((m) => m.is_default)
    if (def && !form.judge_model) form.judge_model = def.id
  } catch {
    /* 评审模型清单加载失败：保持空，由服务端默认 */
  }
  // 恢复草稿（提交失败/离开返回/「重新发起」预填）
  const d = evalStore.draft
  if (d) {
    if (d.name) {
      form.name = d.name
      nameTouched.value = true
    }
    if (d.kb_id) form.kb_id = d.kb_id
    if (d.dataset_id) form.dataset_id = d.dataset_id
    if (d.metrics?.length) form.metrics = [...d.metrics]
    if (d.judge_model) form.judge_model = d.judge_model
    if (d.sample_concurrency) form.sample_concurrency = d.sample_concurrency
  }
})
</script>

<template>
  <div class="eval-new-page">
    <div class="page-head">
      <h2 class="page-title">发起评测</h2>
    </div>

    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="eval-form">
      <!-- 区块一：评测对象 -->
      <div class="form-card">
        <div class="form-card-title">评测对象</div>
        <el-form-item label="任务名称">
          <el-input
            v-model="form.name"
            placeholder="留空则由后端自动生成"
            maxlength="64"
            @input="nameTouched = true"
          />
        </el-form-item>
        <el-form-item label="知识库">
          <el-select v-model="form.kb_id" placeholder="选择知识库（留空 = 逐样本自带范围）" clearable class="full-w">
            <el-option v-for="kb in kbStore.kbs" :key="kb.ID" :label="kb.Name" :value="kb.ID" />
          </el-select>
        </el-form-item>
        <el-form-item label="评测数据集" prop="dataset_id">
          <div class="dataset-row">
            <el-select
              v-model="form.dataset_id"
              placeholder="选择已注册数据集"
              class="full-w"
              :loading="evalStore.datasetsLoading"
            >
              <el-option v-for="d in evalStore.datasets" :key="d.id" :value="d.id">
                <span>{{ d.name }}</span>
                <span class="br-muted dataset-opt-meta">{{ d.sample_count }} 条样本</span>
              </el-option>
            </el-select>
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              accept=".json,.jsonl"
              :on-change="handleUpload"
              :disabled="uploading"
            >
              <el-button :icon="Upload" :loading="uploading">上传新数据集</el-button>
            </el-upload>
          </div>
          <!-- 数据集预览卡：样本数 / 字段校验 / 前 2 条样本折叠预览 -->
          <div v-if="form.dataset_id" v-loading="previewLoading" class="dataset-preview">
            <template v-if="preview">
              <div class="preview-stats">
                <span class="preview-stat">
                  <span class="br-muted">样本数</span>
                  <b class="mono">{{ preview.dataset.sample_count }}</b>
                </span>
                <span class="preview-stat">
                  <span class="br-muted">含标准答案</span>
                  <b class="mono">{{ preview.field_stats.with_reference }}</b>
                </span>
                <span class="preview-stat">
                  <span class="br-muted">含期望文档</span>
                  <b class="mono">{{ preview.field_stats.with_expected_ids }}</b>
                </span>
                <el-tag
                  :type="preview.field_stats.with_reference > 0 ? 'success' : 'warning'"
                  size="small"
                  effect="light"
                >
                  {{ preview.field_stats.with_reference > 0 ? '字段校验通过' : '缺标准答案，召回率不可评' }}
                </el-tag>
              </div>
              <el-collapse v-if="preview.samples.length" class="preview-samples">
                <el-collapse-item title="样本预览（前 2 条）" name="samples">
                  <div v-for="(s, i) in preview.samples" :key="i" class="preview-sample">
                    <div class="preview-q">Q{{ i + 1 }}：{{ s.question }}</div>
                    <div v-if="s.answer" class="br-muted preview-a">A：{{ s.answer }}</div>
                  </div>
                </el-collapse-item>
              </el-collapse>
            </template>
          </div>
        </el-form-item>
      </div>

      <!-- 区块二：评测指标 -->
      <div class="form-card">
        <div class="form-card-title">评测指标</div>
        <el-form-item prop="metrics">
          <el-checkbox-group v-model="form.metrics">
            <div v-for="m in METRIC_ORDER" :key="m" class="metric-item">
              <el-checkbox :value="m" :disabled="m === 'context_recall' && recallDisabled">
                <span class="metric-item-name">
                  {{ METRIC_META[m].label }}
                  <span class="br-muted metric-item-en">{{ METRIC_META[m].en }}</span>
                </span>
              </el-checkbox>
              <div class="br-muted metric-item-desc">
                {{ METRIC_META[m].desc }}
                <span v-if="m === 'context_recall' && recallDisabled" class="recall-hint">
                  （当前数据集无标准答案，此项不可用）
                </span>
              </div>
            </div>
          </el-checkbox-group>
        </el-form-item>
      </div>

      <!-- 区块三：评审模型与执行参数 -->
      <div class="form-card">
        <div class="form-card-title">评审模型与执行参数</div>
        <el-form-item label="评审模型（Judge LLM）">
          <el-select v-model="form.judge_model" placeholder="服务端默认模型" clearable class="full-w">
            <el-option v-for="m in evalStore.judgeModels" :key="m.id" :label="m.label" :value="m.id">
              <span>{{ m.label }}</span>
              <el-tag v-if="m.is_default" size="small" type="success" effect="plain" class="judge-default">
                默认
              </el-tag>
            </el-option>
          </el-select>
        </el-form-item>
        <el-collapse class="advanced-collapse">
          <el-collapse-item title="高级参数" name="advanced">
            <!-- 注：采样上限/单样本超时/随机种子不在 architect-design §2.3-7 契约内，
                 高级参数仅暴露契约中的 sample_concurrency；strategy 覆盖暂由后端默认 -->
            <el-form-item label="样本并发数（采集回调并发上限，默认 4，上限 8）">
              <el-input-number v-model="form.sample_concurrency" :min="1" :max="8" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </div>

      <!-- 吸底操作条 -->
      <div class="form-actions">
        <el-button @click="router.back()">取消</el-button>
        <el-button type="primary" :icon="Collection" :loading="submitting" @click="submit">
          提交评测
        </el-button>
      </div>
    </el-form>
  </div>
</template>

<style scoped>
.eval-new-page {
  padding: 24px 28px;
}

.page-head {
  margin-bottom: 20px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.eval-form {
  max-width: 720px;
  margin: 0 auto;
}

.form-card {
  padding: 20px 22px;
  margin-bottom: 16px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
}

.form-card-title {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  margin-bottom: 14px;
}

.full-w {
  width: 100%;
}

.dataset-row {
  display: flex;
  gap: 10px;
  width: 100%;
}

.dataset-opt-meta {
  float: right;
  font-size: 12px;
}

.dataset-preview {
  width: 100%;
  margin-top: 10px;
  padding: 12px 14px;
  border: 1px solid var(--br-border);
  border-radius: var(--br-radius-md);
  background: var(--br-bg-inset);
}

.preview-stats {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 18px;
}

.preview-stat {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-size: 12.5px;
}

.mono {
  font-family: var(--br-font-mono);
  font-variant-numeric: tabular-nums;
}

.preview-samples {
  margin-top: 8px;
  border: none;
}

.preview-sample {
  margin-bottom: 8px;
}

.preview-q {
  font-size: 13px;
}

.preview-a {
  font-size: 12.5px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.metric-item {
  padding: 6px 0;
}

.metric-item-name {
  font-weight: 600;
  font-size: 13.5px;
}

.metric-item-en {
  font-size: 11.5px;
  font-weight: 400;
  margin-left: 6px;
}

.metric-item-desc {
  font-size: 12px;
  padding-left: 24px;
  line-height: 1.5;
}

.recall-hint {
  color: var(--el-color-warning);
}

.judge-default {
  float: right;
}

.advanced-collapse {
  border: none;
}

.form-actions {
  position: sticky;
  bottom: 0;
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 14px 0;
  background: color-mix(in srgb, var(--br-bg) 88%, transparent);
  backdrop-filter: blur(8px);
}
</style>
