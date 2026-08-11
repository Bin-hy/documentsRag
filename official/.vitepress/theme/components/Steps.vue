<script setup lang="ts">
// 编号步骤（三步上手）
defineProps<{ steps: { title: string; desc: string }[] }>()
</script>

<template>
  <div class="steps">
    <div v-for="(s, i) in steps" :key="i" class="step">
      <div class="step-badge">{{ i + 1 }}</div>
      <div class="step-body">
        <h5>{{ s.title }}</h5>
        <p>{{ s.desc }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
}

.step {
  position: relative;
  display: flex;
  gap: 14px;
  padding: 18px 16px;
  border-radius: 14px;
  border: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-soft);
  transition: border-color 0.2s ease, transform 0.2s ease;
}

.step:hover {
  border-color: color-mix(in srgb, var(--vp-c-brand-1) 50%, transparent);
  transform: translateY(-2px);
}

.step-badge {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  font-size: 14px;
  font-weight: 700;
  color: #fff;
  background: linear-gradient(135deg, var(--vp-c-brand-1), #a855f7);
  box-shadow: 0 4px 10px rgba(var(--vp-c-brand-1-rgb), 0.3);
}

.step-body h5 {
  margin: 0 0 6px;
  font-size: 14px;
  font-weight: 600;
}

.step-body p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.65;
  color: var(--vp-c-text-2);
}
</style>
