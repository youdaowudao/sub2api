<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { createGuideController } from '../controller'
import type { ModuleId } from '../config'
import ReplayPicker from './ReplayPicker.vue'
import '../driver/driver.css'
import '../styles/guide.css'

const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()
let controller: ReturnType<typeof createGuideController> | null = null
const status = shallowRef<ReturnType<ReturnType<typeof createGuideController>['status']> | null>(null)
const pickerOpen = ref(false)
const launcher = ref<HTMLButtonElement>()
const canReplay = computed(() => status.value?.available && !status.value.active && !status.value.launching)
function sync() {
  status.value = controller?.status() || null
  if (!canReplay.value) pickerOpen.value = false
}
onMounted(() => {
  try {
    controller = createGuideController({
      getState: () => ({ user: authStore.user, authenticated: authStore.isAuthenticated, simpleMode: authStore.isSimpleMode, backendMode: appStore.backendModeEnabled, path: route.path, requiresAuth: route.meta.requiresAuth === true, requiresAdmin: route.meta.requiresAdmin === true }),
      getPublicSettings: () => appStore.cachedPublicSettings,
      onChange: sync,
    })
    void controller.update()
  } catch { controller?.destroy(); controller = null; status.value = null }
})
watch(() => [authStore.user?.id, authStore.user?.role, authStore.isAuthenticated, authStore.isSimpleMode, appStore.backendModeEnabled, route.fullPath], async () => {
  pickerOpen.value = false
  await nextTick()
  void controller?.update()
}, { flush: 'post' })
function closePicker() { pickerOpen.value = false; void nextTick(() => launcher.value?.focus({ preventScroll: true })) }
function replay(module: ModuleId) { closePicker(); void controller?.replay(module) }
onBeforeUnmount(() => { controller?.destroy(); controller = null })
</script>

<template>
  <Teleport to="body">
    <button v-if="canReplay" id="s2o-launcher" ref="launcher" type="button" aria-controls="s2o-replay-picker" :aria-expanded="pickerOpen" @click="pickerOpen = !pickerOpen">重播使用引导</button>
    <div v-if="status?.available && status.note" id="s2o-note" role="status">{{ status.note }}</div>
    <ReplayPicker v-if="canReplay && pickerOpen && status" :modules="status.modules" @select="replay" @close="closePicker" />
  </Teleport>
</template>
