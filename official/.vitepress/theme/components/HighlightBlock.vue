<script setup lang="ts">
// 亮点块：单卡（badge + 标题 + 要点列表），index.md 中用两次（MCP / OIDC）
defineProps<{ badge: string; title: string; points: string[] }>()
</script>

<template>
  <div class="highlight">
    <span class="hl-badge">{{ badge }}</span>
    <h4>{{ title }}</h4>
    <ul>
      <li v-for="p in points" :key="p">{{ p }}</li>
    </ul>
  </div>
</template>

<style scoped>
.highlight {
  position: relative;
  overflow: hidden;
  padding: 24px 22px;
  border-radius: 16px;
  border: 1px solid color-mix(in srgb, var(--vp-c-brand-1) 30%, transparent);
  background: linear-gradient(150deg, rgba(var(--vp-c-brand-1-rgb), 0.1), transparent 60%);
}

/* 左上角光斑 */
.highlight::before {
  content: '';
  position: absolute;
  top: -60px;
  left: -60px;
  width: 140px;
  height: 140px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(var(--vp-c-brand-1-rgb), 0.22), transparent 70%);
  pointer-events: none;
}

.hl-badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  color: #fff;
  background: linear-gradient(135deg, var(--vp-c-brand-1), #a855f7);
  margin-bottom: 12px;
}

.highlight h4 {
  margin: 0 0 12px;
  font-size: 17px;
  font-weight: 700;
}

.highlight ul {
  margin: 0;
  padding-left: 18px;
  display: grid;
  gap: 6px;
}

.highlight li {
  font-size: 13.5px;
  line-height: 1.7;
  color: var(--vp-c-text-2);
}

.highlight li::marker {
  color: var(--vp-c-brand-1);
}
</style>
