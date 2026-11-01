<template>
  <canvas ref="canvasRef" class="streams-bg" aria-hidden="true"></canvas>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { createStreams, type StreamsHandle } from '../streams'
import { streamModels } from '../config'

const props = withDefaults(
  defineProps<{
    /** 线条汇入的元素选择器 */
    focalSelector?: string
    /** 宽屏时在两侧显示模型名 */
    labels?: boolean
    /** 淡化模式（认证页） */
    soft?: boolean
  }>(),
  {
    focalSelector: '[data-streams-focal]',
    labels: false,
    soft: false
  }
)

const canvasRef = ref<HTMLCanvasElement | null>(null)
let handle: StreamsHandle | null = null

onMounted(async () => {
  await nextTick()
  if (!canvasRef.value) return
  handle = createStreams(canvasRef.value, {
    focal: () => document.querySelector(props.focalSelector),
    labels: props.labels,
    soft: props.soft,
    models: streamModels
  })
})

onBeforeUnmount(() => {
  handle?.destroy()
  handle = null
})

defineExpose({
  refresh: () => handle?.refresh()
})
</script>

<style scoped>
.streams-bg {
  position: fixed;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 0;
}
</style>
