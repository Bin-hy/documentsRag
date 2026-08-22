<script setup lang="ts">
// 视频播放器：带鉴权加载原文，seek 到命中时间，展示命中区间
import { onMounted, onUnmounted, ref } from 'vue'
import client from '../../api/client'
import { rawDocumentUrl } from '../../api/doc'
import type { ViewerProps } from './types'

const props = defineProps<ViewerProps>()

const videoRef = ref<HTMLVideoElement | null>(null)
const src = ref('')
const error = ref('')

function formatMs(ms?: number): string {
  if (ms == null) return '-'
  const s = Math.floor(ms / 1000)
  const m = Math.floor(s / 60)
  return `${m}:${String(s % 60).padStart(2, '0')}`
}

onMounted(async () => {
  try {
    const resp = await client.get(rawDocumentUrl(props.documentId), { responseType: 'blob' })
    src.value = URL.createObjectURL(resp.data as Blob)
  } catch (e) {
    error.value = (e as Error).message
  }
})

function onLoadedMetadata() {
  const v = videoRef.value
  if (v && props.location.startMs != null) {
    v.currentTime = props.location.startMs / 1000
  }
}

onUnmounted(() => {
  if (src.value) URL.revokeObjectURL(src.value)
})
</script>

<template>
  <div class="video-viewer">
    <div v-if="error" class="viewer-error">{{ error }}</div>
    <video v-else ref="videoRef" :src="src" controls class="video-el" @loadedmetadata="onLoadedMetadata" />
    <div v-if="!error && (props.location.startMs != null || props.location.endMs != null)" class="hit-range">
      命中区间：{{ formatMs(props.location.startMs) }} ~ {{ formatMs(props.location.endMs) }}
    </div>
  </div>
</template>

<style scoped>
.video-viewer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.video-el {
  width: 100%;
  max-height: 74vh;
  border-radius: 8px;
  background: #000;
}
.hit-range {
  font-size: 12px;
  color: var(--br-text-secondary, #888);
}
.viewer-error {
  padding: 16px;
  color: #d33;
  font-size: 13px;
}
</style>
