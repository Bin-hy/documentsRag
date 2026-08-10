<script setup lang="ts">
// 单条消息：用户右对齐 / 助手左对齐（Markdown + 来源 + 流式光标 + 错误态 + 思考链路）
import MarkdownRenderer from './MarkdownRenderer.vue'
import SourceCard from './SourceCard.vue'
import ThinkingPanel from './ThinkingPanel.vue'
import type { LocalMessage } from '../stores/chat'

defineProps<{ message: LocalMessage; streaming: boolean }>()
</script>

<template>
  <div class="msg" :class="message.role">
    <div class="msg-avatar">{{ message.role === 'user' ? '我' : 'AI' }}</div>
    <div class="msg-body">
      <div class="msg-bubble" :class="{ error: message.error }">
        <template v-if="message.role === 'assistant'">
          <!-- 思考链路面板：流式中逐步累积（active 表示最后一步为当前环节） -->
          <ThinkingPanel v-if="message.thinking && message.thinking.length" :steps="message.thinking" :active="streaming" />
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
  margin-bottom: 22px;
  animation: msg-in 260ms var(--br-ease) both;
}

@keyframes msg-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.msg.user {
  flex-direction: row-reverse;
}

/* 头像：圆角方块（替代通用圆形） */
.msg-avatar {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  font-size: 12.5px;
  font-weight: 600;
  flex-shrink: 0;
}

.msg.user .msg-avatar {
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  color: #fff;
  box-shadow: 0 4px 10px rgba(79, 91, 213, 0.3);
}

.msg.assistant .msg-avatar {
  background: var(--br-bg-inset);
  border: 1px solid var(--br-border);
  color: var(--br-primary);
}

.msg-body {
  max-width: 82%;
  min-width: 0;
}

/* 用户消息收敛、助手回答突出（回答是视觉主体） */
.msg.user .msg-body {
  max-width: 64%;
}

.msg-bubble {
  padding: 13px 15px;
  border-radius: var(--br-radius-lg);
  background: var(--br-bg-card);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-sm);
}

.msg.user .msg-bubble {
  background: linear-gradient(135deg, #5561d6, #4650bd);
  border-color: transparent;
  color: #fff;
  box-shadow: 0 6px 16px rgba(79, 91, 213, 0.26);
  border-bottom-right-radius: 6px;
}

.msg.assistant .msg-bubble {
  border-top-left-radius: 6px;
  box-shadow: var(--br-shadow-md);
}

.msg-bubble.error {
  border-color: rgba(245, 108, 108, 0.5);
  background: color-mix(in srgb, #f56c6c 7%, var(--br-bg-card));
}

.user-text {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.65;
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
