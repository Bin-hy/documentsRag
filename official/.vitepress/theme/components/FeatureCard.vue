<script setup lang="ts">
// 特性墙卡片：内联 SVG 图标 + 渐变描边 + hover 抬升
defineProps<{ icon: string; title: string; desc: string }>()

// 内联 SVG 图标集（24x24 stroke 风格，无外部依赖）
const icons: Record<string, string> = {
  doc: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M8 13h8M8 17h8"/>',
  layers: '<path d="m12 2 10 6-10 6L2 8z"/><path d="m2 14 10 6 10-6"/><path d="m2 20 10 6 10-6"/>',
  filter: '<path d="M22 3H2l8 9.46V19l4 2v-8.54z"/>',
  bolt: '<path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/>',
  task: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="m9 12 2 2 4-4"/><path d="M8 7h8"/>',
  lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  mcp: '<path d="M8 3v4M16 3v4M4 7h16v14H4z"/><path d="M12 12v5"/><circle cx="12" cy="19" r="1.5"/>',
  desktop: '<rect x="2" y="4" width="20" height="14" rx="2"/><path d="M8 22h8M12 18v4"/>',
}
</script>

<template>
  <div class="feature-card">
    <div class="fc-icon" v-html="icons[icon] || icons.doc" />
    <h4>{{ title }}</h4>
    <p>{{ desc }}</p>
  </div>
</template>

<style scoped>
.feature-card {
  position: relative;
  padding: 22px 20px;
  border-radius: 14px;
  background: var(--vp-c-brand-soft);
  border: 1px solid color-mix(in srgb, var(--vp-c-brand-1) 28%, transparent);
  transition: transform 0.25s ease, box-shadow 0.25s ease, border-color 0.25s ease;
}

.feature-card:hover {
  transform: translateY(-4px);
  border-color: color-mix(in srgb, var(--vp-c-brand-1) 55%, transparent);
  box-shadow:
    0 10px 28px rgba(var(--vp-c-brand-1-rgb), 0.16),
    0 0 0 1px rgba(var(--vp-c-brand-1-rgb), 0.08);
}

.fc-icon {
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  margin-bottom: 14px;
  border-radius: 11px;
  background: linear-gradient(135deg, var(--vp-c-brand-1), #a855f7);
  box-shadow: 0 6px 14px rgba(var(--vp-c-brand-1-rgb), 0.3);
}

.fc-icon :deep(svg) {
  width: 22px;
  height: 22px;
  stroke: #fff;
  fill: none;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.feature-card h4 {
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 600;
}

.feature-card p {
  margin: 0;
  font-size: 13px;
  line-height: 1.65;
  color: var(--vp-c-text-2);
}
</style>
