<script setup lang="ts">
// 单条消息：用户右对齐 / 助手左对齐（Markdown + 来源 + 流式光标 + 错误态）
import MarkdownRenderer from './MarkdownRenderer.vue'
import SourceCard from './SourceCard.vue'
import type { LocalMessage } from '../stores/chat'

defineProps<{ message: LocalMessage; streaming: boolean }>()
</script>

<template>
  <div class="msg" :class="message.role">
    <div class="msg-avatar">{{ message.role === 'user' ? '我' : 'AI' }}</div>
    <div class="msg-body">
      <div class="msg-bubble" :class="{ error: message.error }">
        <template v-if="message.role === 'assistant'">
          <MarkdownRenderer v-if="message.content" :content="message.content" />
          <span v-else-if="streaming" class="typing-cursor">▍</span>
          <span v-else class="br-muted">（无回答）</span>
          <SourceCard v-if="message.sources && message.sources.length" :sources="message.sources" />
        </template>
        <template v-else>
          <span class="user-text">{{ message.content }}</span>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.msg {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
}

.msg.user {
  flex-direction: row-reverse;
}

.msg-avatar {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  font-size: 13px;
  flex-shrink: 0;
}

.msg.user .msg-avatar {
  background: var(--br-primary);
  color: #fff;
}

.msg.assistant .msg-avatar {
  background: var(--br-hover);
  border: 1px solid var(--br-border);
}

.msg-body {
  max-width: 78%;
  min-width: 0;
}

.msg-bubble {
  padding: 12px 14px;
  border-radius: 12px;
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow);
}

.msg.user .msg-bubble {
  background: var(--br-primary);
  border-color: var(--br-primary);
  color: #fff;
}

.msg-bubble.error {
  border-color: #f56c6c;
  background: rgba(245, 108, 108, 0.08);
}

.user-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.typing-cursor {
  display: inline-block;
  color: var(--br-primary);
  animation: blink 0.8s infinite;
}

@keyframes blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0;
  }
}
</style>
