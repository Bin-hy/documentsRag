<script setup lang="ts">
// 音频播放器：wavesurfer.js 波形 + 命中区间高亮，seek 到命中时间
import { onMounted, onUnmounted, ref } from 'vue'
import WaveSurfer from 'wavesurfer.js'
import client from '../../api/client'
import { rawDocumentUrl } from '../../api/doc'
import type { ViewerProps } from './types'

const props = defineProps<ViewerProps>()

const containerRef = ref<HTMLDivElement | null>(null)
const error = ref('')

let ws: WaveSurfer | null = null
let objectUrl = ''

onMounted(async () => {
  try {
    const resp = await client.get(rawDocumentUrl(props.documentId), { responseType: 'blob' })
    objectUrl = URL.createObjectURL(resp.data as Blob)
    ws = WaveSurfer.create({
      container: containerRef.value!,
      url: objectUrl,
      waveColor: '#c7c7d1',
      progressColor: '#4f5bd5',
      height: 96,
    })
    ws.on('ready', () => {
      const duration = ws!.getDuration()
      if (props.location.startMs != null && duration > 0) {
        ws!.seekTo(props.location.startMs / 1000 / duration)
      }
    })
  } catch (e) {
    error.value = (e as Error).message
  }
})

onUnmounted(() => {
  ws?.destroy()
  if (objectUrl) URL.revokeObjectURL(objectUrl)
})
</script>

<template>
  <div class="audio-viewer">
    <div v-if="error" class="viewer-error">{{ error }}</div>
    <template v-else>
      <div ref="containerRef" class="waveform" />
      <div v-if="props.location.startMs != null || props.location.endMs != null" class="hit-range">
        命中区间：{{ props.location.startMs ?? '-' }} ~ {{ props.location.endMs ?? '-' }} ms
      </div>
    </template>
  </div>
</template>

<style scoped>
.audio-viewer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.waveform {
  width: 100%;
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
