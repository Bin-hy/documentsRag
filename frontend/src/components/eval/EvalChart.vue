<script setup lang="ts">
// vue-echarts 薄封装（frontend-design §2.4）：
// - echarts/core 按需注册（Radar/Bar + CanvasRenderer），本组件只经 defineAsyncComponent 懒加载，不进主包
// - props.makeOption 为 option 工厂（颜色取自 --br-* 变量，见 utils/evalChart.ts）
// - MutationObserver 监听 html.dark 变化 → 重建 option，亮暗主题与全站一致
// - autoresize 处理容器尺寸变化；卸载时断开 observer
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, RadarChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import type { EChartsOption } from 'echarts'

use([CanvasRenderer, BarChart, RadarChart, GridComponent, LegendComponent, TooltipComponent])

const props = withDefaults(
  defineProps<{
    /** option 工厂：isDark 变化或依赖的响应式数据变化时重新求值 */
    makeOption: (isDark: boolean) => EChartsOption
    height?: string
  }>(),
  { height: '320px' },
)

const isDark = ref(document.documentElement.classList.contains('dark'))

// computed 求值时会执行 makeOption → 其中读取的 store/响应式数据会被自动追踪
const option = computed<EChartsOption>(() => props.makeOption(isDark.value))

let observer: MutationObserver | null = null

onMounted(() => {
  observer = new MutationObserver(() => {
    isDark.value = document.documentElement.classList.contains('dark')
  })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})
</script>

<template>
  <VChart :option="option" :style="{ height, width: '100%' }" autoresize />
</template>
