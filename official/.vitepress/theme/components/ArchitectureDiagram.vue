<script setup lang="ts">
// 架构图：纯 CSS 三段式（前端/API+MCP → 入库链路 → 问答链路），无 mermaid 依赖
</script>

<template>
  <div class="arch">
    <!-- 段 1：接入层 -->
    <div class="arch-row">
      <span class="arch-row-label">接入层</span>
      <div class="arch-nodes">
        <div class="arch-node">Web 前端<small>Vue 3 SPA</small></div>
        <div class="arch-node">桌面应用<small>Wails v3</small></div>
        <div class="arch-node">外部 Agent<small>MCP Client</small></div>
      </div>
    </div>
    <div class="arch-arrow">↓</div>

    <!-- 段 2：API / MCP -->
    <div class="arch-row">
      <span class="arch-row-label">API / MCP</span>
      <div class="arch-nodes">
        <div class="arch-node arch-node-brand">
          REST API<small>知识库 / 文档 / 问答 / 认证</small>
        </div>
        <div class="arch-node arch-node-brand">
          MCP Server<small>streamable HTTP · 6 个只读 Tool</small>
        </div>
      </div>
    </div>
    <div class="arch-arrow">↓</div>

    <!-- 段 3：能力链路 -->
    <div class="arch-row">
      <span class="arch-row-label">能力链路</span>
      <div class="arch-nodes">
        <div class="arch-node">
          入库链路<small>Load → Chunk → Embed → Store</small>
        </div>
        <div class="arch-node">
          问答链路<small>改写 → 混合检索(RRF) → 重排序 → LLM</small>
        </div>
      </div>
    </div>

    <div class="arch-store">
      <span>Qdrant 向量库</span>
      <span>BM25 索引</span>
      <span>PostgreSQL 元数据</span>
    </div>
  </div>
</template>

<style scoped>
.arch {
  padding: 24px 20px;
  border-radius: 16px;
  border: 1px solid var(--vp-c-brand-soft);
  background: var(--vp-c-bg-soft);
}

.arch-row {
  display: grid;
  grid-template-columns: 76px 1fr;
  gap: 14px;
  align-items: center;
}

.arch-row-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--vp-c-text-2);
  writing-mode: vertical-rl;
  text-orientation: mixed;
  letter-spacing: 0.1em;
}

.arch-nodes {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
}

.arch-node {
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--vp-c-bg);
  border: 1px solid var(--vp-c-divider);
  font-size: 14px;
  font-weight: 600;
  transition: border-color 0.2s ease, transform 0.2s ease;
}

.arch-node:hover {
  border-color: var(--vp-c-brand-1);
  transform: translateY(-2px);
}

.arch-node-brand {
  border-color: color-mix(in srgb, var(--vp-c-brand-1) 45%, transparent);
  background: linear-gradient(135deg, rgba(var(--vp-c-brand-1-rgb), 0.12), rgba(var(--vp-c-brand-1-rgb), 0.04));
}

.arch-node small {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  font-weight: 400;
  color: var(--vp-c-text-2);
}

.arch-arrow {
  text-align: center;
  color: var(--vp-c-brand-1);
  font-size: 16px;
  line-height: 1;
  padding: 8px 0;
}

.arch-store {
  margin-top: 16px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
}

.arch-store span {
  padding: 6px 14px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--vp-c-brand-1);
  border: 1px dashed color-mix(in srgb, var(--vp-c-brand-1) 45%, transparent);
  background: rgba(var(--vp-c-brand-1-rgb), 0.06);
}

@media (max-width: 720px) {
  .arch-row {
    grid-template-columns: 1fr;
  }
  .arch-row-label {
    writing-mode: horizontal-tb;
  }
}
</style>
