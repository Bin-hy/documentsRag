<script setup lang="ts">
// 统一文档查看入口：按 fileType 解析 Viewer 并渲染，定位信息透传给子 Viewer
import { computed } from 'vue'
import type { Component } from 'vue'
import { resolveViewer } from './ViewerRegistry'
import type { ViewerLocation, ViewerType } from './types'

const props = defineProps<{
  documentId: string
  filename: string
  fileType: ViewerType
  location: ViewerLocation
}>()

const comp = computed<Component | undefined>(() => resolveViewer(props.fileType))
</script>

<template>
  <component
    :is="comp"
    v-if="comp"
    :document-id="documentId"
    :filename="filename"
    :location="location"
  />
  <div v-else class="viewer-unsupported">暂不支持该类型的原文查看</div>
</template>

<style scoped>
.viewer-unsupported {
  padding: 24px;
  text-align: center;
  color: var(--br-text-secondary, #888);
  font-size: 13px;
}
</style>
