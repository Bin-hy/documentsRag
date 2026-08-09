<script setup lang="ts">
// 对话页：会话侧栏 + 消息区 + 输入区（知识库选择 / 增强面板 / 流式停止）
import { nextTick, onMounted, ref, watch } from 'vue'
import { Promotion, VideoPause } from '@element-plus/icons-vue'
import { getStoredApiKey } from '../api/client'
import { useChatStore } from '../stores/chat'
import { useKbStore } from '../stores/kb'
import SessionList from '../components/SessionList.vue'
import ChatMessage from '../components/ChatMessage.vue'

const chatStore = useChatStore()
const kbStore = useKbStore()

const question = ref('')
const messageAreaRef = ref<HTMLDivElement>()

const selectedKbId = ref('')

// 增强能力可用性（来自 GET /api/v1/chat/enhancements）
const webSearchAvailable = ref(false)

async function loadEnhancements() {
  try {
    const resp = await fetch('/api/v1/chat/enhancements', {
      headers: { Authorization: `Bearer ${getStoredApiKey()}` },
    })
    if (!resp.ok) return
    const body = await resp.json()
    const list: Array<{ key: string; available: boolean }> = body?.data?.enhancements ?? []
    const web = list.find((e) => e.key === 'web_search')
    webSearchAvailable.value = web?.available ?? false
    if (!webSearchAvailable.value) {
      chatStore.enhanced = false // 不可用时强制关闭
    }
  } catch {
    /* 忽略：拉取失败按不可用处理 */
  }
}

onMounted(loadEnhancements)

async function scrollToBottom() {
  await nextTick()
  const el = messageAreaRef.value
  if (el) el.scrollTop = el.scrollHeight
}

// 流式期间持续滚动到底部
watch(
  () => chatStore.messages.map((m) => m.content).join(''),
  () => {
    if (chatStore.streaming) scrollToBottom()
  },
)

watch(
  () => chatStore.activeSessionId,
  () => scrollToBottom(),
)

async function handleSend() {
  const q = question.value.trim()
  if (!q) return
  question.value = ''
  chatStore.send(q)
  scrollToBottom()
}

function handleNewSession() {
  chatStore.newSession(selectedKbId.value)
}

async function handleSwitchSession(id: string) {
  await chatStore.switchSession(id)
}

function handleDeleteSession(id: string) {
  chatStore.deleteSession(id)
}

function handleStop() {
  chatStore.stop()
}

onMounted(async () => {
  // 知识库下拉数据源
  kbStore.load()
  // 恢复上次会话
  if (chatStore.sessions.length > 0 && !chatStore.activeSessionId) {
    await chatStore.switchSession(chatStore.sessions[0].id)
  } else if (chatStore.sessions.length === 0) {
    chatStore.newSession()
  }
})
</script>

<template>
  <div class="chat-page">
    <!-- 左：会话侧栏 -->
    <aside class="chat-side">
      <SessionList
        :sessions="chatStore.sessions"
        :active-id="chatStore.activeSessionId"
        @new="handleNewSession"
        @switch="handleSwitchSession"
        @delete="handleDeleteSession"
      />
    </aside>

    <!-- 右：消息区 + 输入区 -->
    <div class="chat-main">
      <div ref="messageAreaRef" class="message-area">
        <div v-if="chatStore.messages.length === 0" class="message-welcome">
          <div class="welcome-title">你好，我是 BinRag 知识库助手 👋</div>
          <p class="br-muted">选择知识库后提问，回答将基于知识库内容生成并标注引用来源</p>
        </div>

        <ChatMessage
          v-for="(m, i) in chatStore.messages"
          :key="i"
          :message="m"
          :streaming="chatStore.streaming && i === chatStore.messages.length - 1"
        />
      </div>

      <div class="input-area">
        <div class="input-bar">
          <el-select
            v-model="selectedKbId"
            class="kb-select"
            placeholder="知识库（不限）"
            clearable
            size="large"
            @change="handleNewSession"
          >
            <el-option
              v-for="kb in kbStore.kbs"
              :key="kb.ID"
              :label="kb.Name"
              :value="kb.ID"
            />
          </el-select>
          <el-input
            ref="inputRef"
            v-model="question"
            type="textarea"
            :rows="2"
            resize="none"
            placeholder="输入问题，Enter 发送，Shift+Enter 换行"
            :disabled="chatStore.streaming"
            @keydown.enter.exact.prevent="handleSend"
          />
          <el-button
            v-if="chatStore.streaming"
            type="danger"
            size="large"
            :icon="VideoPause"
            class="send-btn"
            @click="handleStop"
          >
            停止
          </el-button>
          <el-button
            v-else
            type="primary"
            size="large"
            :icon="Promotion"
            class="send-btn"
            :disabled="!question.trim()"
            @click="handleSend"
          >
            发送
          </el-button>
        </div>
        <div class="enhance-bar">
          <el-checkbox
            v-model="chatStore.enhanced"
            :disabled="chatStore.streaming || !webSearchAvailable"
            class="enhance-toggle"
          >
            增强：联网搜索
          </el-checkbox>
          <span class="enhance-tip br-muted">
            {{ webSearchAvailable ? '开启后回答可联网检索知识库外的最新信息' : '未配置联网搜索密钥（web_search.api_key），暂不可用' }}
          </span>
        </div>
        <p class="input-hint br-muted">内容由 AI 生成，请核实关键信息；引用来源可在回答下方查看</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chat-page {
  display: flex;
  height: 100%;
}

.chat-side {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid var(--br-border);
  background: var(--br-bg-card);
}

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.message-area {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}

.message-welcome {
  text-align: center;
  padding-top: 15vh;
}

.welcome-title {
  font-size: 22px;
  font-weight: 600;
  margin-bottom: 8px;
}

.input-area {
  padding: 12px 24px 16px;
  border-top: 1px solid var(--br-border);
  background: var(--br-bg-card);
}

.input-bar {
  display: flex;
  gap: 12px;
  align-items: flex-end;
}

.kb-select {
  width: 180px;
  flex-shrink: 0;
}

.send-btn {
  flex-shrink: 0;
}

.input-hint {
  margin: 8px 0 0;
  font-size: 12px;
  text-align: center;
}

.enhance-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  padding: 0 4px;
}

.enhance-toggle {
  --el-checkbox-font-size: 12px;
}

.enhance-tip {
  font-size: 12px;
}
</style>
