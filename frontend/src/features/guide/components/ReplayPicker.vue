<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { moduleCatalog, type ModuleId } from '../config'

const props = defineProps<{ modules: ModuleId[] }>()
const emit = defineEmits<{ select: [module: ModuleId]; close: [] }>()
const panel = ref<HTMLElement>()
const abort = new AbortController()
function close(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.isComposing || event.keyCode === 229) return
  event.preventDefault(); event.stopImmediatePropagation(); emit('close')
}
function outside(event: Event) {
  if (event.target instanceof Node && !panel.value?.contains(event.target) && !(event.target instanceof Element && event.target.closest('#s2o-launcher'))) emit('close')
}
onMounted(async () => {
  document.body.classList.add('s2o-picker-open')
  document.addEventListener('keydown', close, { capture: true, signal: abort.signal })
  document.addEventListener('pointerdown', outside, { capture: true, signal: abort.signal })
  document.addEventListener('focusin', outside, { signal: abort.signal })
  for (const name of ['popstate', 'hashchange', 'pagehide', 'storage']) window.addEventListener(name, () => emit('close'), { signal: abort.signal })
  await nextTick()
  panel.value?.querySelector<HTMLButtonElement>('[data-s2o-module]')?.focus({ preventScroll: true })
})
onBeforeUnmount(() => { abort.abort(); document.body.classList.remove('s2o-picker-open') })
</script>

<template>
  <section id="s2o-replay-picker" ref="panel" aria-labelledby="s2o-replay-title">
    <div class="s2o-replay-header">
      <h2 id="s2o-replay-title">选择要重播的内容</h2>
      <button type="button" class="s2o-replay-close" aria-label="关闭引导选择" @click="emit('close')">关闭</button>
    </div>
    <p>按需选择一段，随时可以退出。</p>
    <div class="s2o-replay-choices">
      <button v-for="id in props.modules" :key="id" type="button" :data-s2o-module="id" @click="emit('select', id)">
        <strong>{{ moduleCatalog.find(item => item.id === id)?.title }}</strong>
        <span>{{ moduleCatalog.find(item => item.id === id)?.description }}</span>
      </button>
    </div>
  </section>
</template>
