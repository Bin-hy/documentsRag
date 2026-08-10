<script setup lang="ts">
// 对话页：会话侧栏 + 消息区 + 输入区（知识库选择 / 增强面板 / 流式停止）
import { nextTick, onMounted, ref, watch } from 'vue'
import { Promotion, VideoPause } from '@element-plus/icons-vue'
import { getStoredApiKey, getStoredToken } from '../api/client'
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
      headers: { Authorization: `Bearer ${getStoredToken() || getStoredApiKey()}` },
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
  // kb_id 范围：选中的知识库（可为空）。后端契约：
  //   JWT 用户空 = 自动检索其名下全部知识库；API Key 空 = 不限定
  question.value = ''
  chatStore.send(q, selectedKbId.value)
  scrollToBottom()
}

function handleNewSession() {
  chatStore.newSession(selectedKbId.value)
}

async function handleSwitchSession(id: string) {
  await chatStore.switchSession(id)
  // 同步下拉选择与当前会话绑定的知识库
  selectedKbId.value = chatStore.activeSession?.kbId ?? ''
}

function handleDeleteSession(id: string) {
  chatStore.deleteSession(id)
}

function handleStop() {
  chatStore.stop()
}

// 发送/停止的 Enter 处理：中文输入法（IME）组合态下的回车不触发提交
function onEnterKey(e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  handleSend()
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
        <div class="message-area-inner">
          <div v-if="chatStore.messages.length === 0" class="message-welcome">
            <div class="welcome-mark">B</div>
            <h1 class="welcome-title">你好，我是 BinRag 知识库助手</h1>
            <p class="welcome-sub">
              基于企业知识库的智能问答。选择知识库后提问，回答将标注引用来源
            </p>
            <div class="welcome-cap">
              <span class="cap-chip">
                <span class="cap-dot" />多轮对话
              </span>
              <span class="cap-chip">
                <span class="cap-dot" />联网增强
              </span>
              <span class="cap-chip">
                <span class="cap-dot" />引用溯源
              </span>
            </div>
          </div>

          <ChatMessage
            v-for="(m, i) in chatStore.messages"
            :key="i"
            :message="m"
            :streaming="chatStore.streaming && i === chatStore.messages.length - 1"
          />
        </div>
      </div>

      <div class="input-area">
        <div class="input-island">
          <!-- 生成中状态指示 -->
          <transition name="status-fade">
            <div v-if="chatStore.streaming" class="stream-status">
              <span class="stream-dot" />
              <span class="stream-text">正在生成回答…</span>
              <span class="stream-hint">可点击右侧停止</span>
            </div>
          </transition>
          <div class="input-toolbar">
            <el-select
              v-model="selectedKbId"
              class="kb-select"
              placeholder="知识库（不限）"
              clearable
              size="default"
              @change="handleNewSession"
            >
              <el-option
                v-for="kb in kbStore.kbs"
                :key="kb.ID"
                :label="kb.Name"
                :value="kb.ID"
              />
            </el-select>
            <el-checkbox
              v-model="chatStore.enhanced"
              :disabled="chatStore.streaming || !webSearchAvailable"
              class="enhance-toggle"
              :title="webSearchAvailable ? '开启后回答可联网检索知识库外的最新信息' : '未配置联网搜索密钥（web_search.api_key），暂不可用'"
            >
              联网搜索
            </el-checkbox>
          </div>
          <div class="input-row">
            <el-input
              ref="inputRef"
              v-model="question"
              type="textarea"
              :rows="1"
              autosize
              resize="none"
              placeholder="输入问题，Enter 发送，Shift+Enter 换行"
              :disabled="chatStore.streaming"
              class="chat-input"
              @keydown.enter.exact="onEnterKey"
            />
            <button
              v-if="chatStore.streaming"
              class="send-btn stop"
              title="停止生成"
              @click="handleStop"
            >
              <el-icon><VideoPause /></el-icon>
            </button>
            <button
              v-else
              class="send-btn"
              title="发送"
              :disabled="!question.trim()"
              @click="handleSend"
            >
              <el-icon><Promotion /></el-icon>
            </button>
          </div>
          <div class="input-meta">
            <span class="kbd-hint">
              <kbd>Enter</kbd> 发送 · <kbd>Shift</kbd>+<kbd>Enter</kbd> 换行
            </span>
          </div>
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

/* ---------- 会话侧栏 ---------- */
.chat-side {
  width: 228px;
  flex-shrink: 0;
  border-right: 1px solid var(--br-border);
  background: var(--br-bg-card);
}

/* ---------- 消息区 ---------- */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.message-area {
  flex: 1;
  overflow-y: auto;
  padding: 28px 24px 8px;
}

/* 内容容器约束：宽屏不无限拉伸 */
.message-area-inner {
  max-width: 880px;
  margin: 0 auto;
}

/* ---------- 欢迎页 ---------- */
.message-welcome {
  text-align: center;
  padding: 9vh 16px 0;
  animation: welcome-in 520ms var(--br-ease) both;
}

@keyframes welcome-in {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.welcome-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 58px;
  height: 58px;
  border-radius: 17px;
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  color: #fff;
  font-size: 30px;
  font-weight: 700;
  box-shadow: 0 14px 34px rgba(79, 91, 213, 0.32);
  margin-bottom: 22px;
}

.welcome-title {
  margin: 0 0 10px;
  font-size: 27px;
  font-weight: 700;
  letter-spacing: -0.025em;
  line-height: 1.3;
}

.welcome-sub {
  margin: 0 auto 26px;
  max-width: 420px;
  color: var(--br-text-secondary);
  font-size: 14px;
}

.welcome-cap {
  display: flex;
  justify-content: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cap-chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 7px 14px;
  border-radius: var(--br-radius-pill);
  border: 1px solid var(--br-border);
  background: var(--br-bg-card);
  font-size: 12.5px;
  color: var(--br-text-secondary);
  transition:
    border-color var(--br-transition-fast),
    color var(--br-transition-fast),
    transform var(--br-transition-fast);
}

.cap-chip:hover {
  border-color: var(--br-primary-soft-2);
  color: var(--br-primary);
  transform: translateY(-1px);
}

.cap-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--br-primary);
  box-shadow: 0 0 0 3px var(--br-primary-soft);
}

/* ---------- 输入区（岛式双贝泽尔） ---------- */
.input-area {
  padding: 10px 24px 18px;
}

.input-island {
  max-width: 880px;
  margin: 0 auto;
  padding: 9px;
  border-radius: 22px;
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-md);
  transition:
    border-color var(--br-transition-base),
    box-shadow var(--br-transition-base);
}

.input-island:focus-within {
  border-color: var(--br-primary-soft-2);
  box-shadow: 0 0 0 4px var(--br-primary-soft), var(--br-shadow-md);
}

.input-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 2px 6px 8px;
}

.kb-select {
  width: 190px;
}

.kb-select :deep(.el-select__wrapper) {
  background: transparent;
  box-shadow: none;
  border-radius: var(--br-radius-pill);
}

.enhance-toggle {
  --el-checkbox-font-size: 12.5px;
}

.input-row {
  display: flex;
  align-items: flex-end;
  gap: 10px;
}

.chat-input :deep(.el-textarea__inner) {
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  border-radius: 14px;
  padding: 12px 16px;
  font-size: 14.5px;
  line-height: 1.55;
  box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.4);
  transition:
    border-color var(--br-transition-base),
    box-shadow var(--br-transition-base);
}

.chat-input :deep(.el-textarea__inner:focus) {
  border-color: var(--br-primary);
  box-shadow: 0 0 0 3px var(--br-primary-soft);
}

html.dark .chat-input :deep(.el-textarea__inner) {
  box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.05);
}

/* 圆形发送按钮：渐变主按钮 + 红色停止 */
.send-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  flex-shrink: 0;
  border: none;
  border-radius: 50%;
  cursor: pointer;
  color: #fff;
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  box-shadow: 0 6px 16px rgba(79, 91, 213, 0.35);
  font-size: 18px;
  transition:
    transform var(--br-transition-fast),
    box-shadow var(--br-transition-fast),
    background-color var(--br-transition-fast),
    opacity var(--br-transition-fast);
}

.send-btn:hover {
  transform: translateY(-1px) scale(1.04);
  box-shadow: 0 10px 22px rgba(79, 91, 213, 0.4);
}

.send-btn:active {
  transform: scale(0.96);
}

.send-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
  transform: none;
  box-shadow: none;
}

.send-btn.stop {
  background: linear-gradient(135deg, #f56c6c, #d94a4a);
  box-shadow: 0 6px 16px rgba(245, 108, 108, 0.32);
}

.input-hint {
  margin: 10px 0 0;
  font-size: 12px;
  text-align: center;
  color: var(--br-text-tertiary);
}

/* ---------- 生成中状态指示 ---------- */
.stream-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 6px 8px;
  padding: 7px 12px;
  border-radius: var(--br-radius-pill);
  background: var(--br-primary-soft);
  color: var(--br-primary);
  font-size: 12.5px;
}

.stream-dot {
  width: 7px;
  height: 7px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--br-primary);
  animation: stream-blink 1.1s var(--br-ease) infinite;
}

@keyframes stream-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}

.stream-text {
  font-weight: 600;
}

.stream-hint {
  margin-left: auto;
  font-size: 11.5px;
  color: var(--br-primary);
  opacity: 0.75;
}

.status-fade-enter-active,
.status-fade-leave-active {
  transition:
    opacity 220ms var(--br-ease),
    transform 220ms var(--br-ease);
}

.status-fade-enter-from,
.status-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

/* ---------- 键盘提示 ---------- */
.input-meta {
  display: flex;
  justify-content: flex-end;
  padding: 7px 6px 0;
}

.kbd-hint {
  font-size: 11.5px;
  color: var(--br-text-tertiary);
}

.kbd-hint kbd {
  display: inline-block;
  padding: 1px 6px;
  border: 1px solid var(--br-border);
  border-bottom-width: 2px;
  border-radius: 5px;
  background: var(--br-bg-card);
  color: var(--br-text-secondary);
  font-family: var(--br-font-mono);
  font-size: 10.5px;
  line-height: 1.5;
}
</style>
