<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { createGuideController } from '../controller'
import { moduleCatalog, type ModuleId } from '../config'
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
const continueButton = ref<HTMLButtonElement>()
const canReplay = computed(() => status.value?.available && !status.value.active && !status.value.launching)
const canContinue = computed(() => status.value?.available && status.value.paused)
const nextTitle = computed(() => moduleCatalog.find(module => module.id === status.value?.pendingModule)?.title)
watch(canContinue, async pending => {
  if (!pending) return
  await nextTick()
  if (canContinue.value && !status.value?.launching && document.activeElement === document.body) continueButton.value?.focus({ preventScroll: true })
})
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
function closePicker(restoreFocus = false) {
  pickerOpen.value = false
  if (restoreFocus && canReplay.value && launcher.value?.isConnected) launcher.value.focus({ preventScroll: true })
}
function replay(module: ModuleId) { closePicker(true); void controller?.replay(module) }
function continueChapter() { void controller?.continue() }
function endGuide() { controller?.exit() }
onBeforeUnmount(() => { controller?.destroy(); controller = null })
</script>

<template>
  <Teleport to="body">
    <button v-if="canReplay" id="s2o-launcher" ref="launcher" type="button" aria-controls="s2o-replay-picker" :aria-expanded="pickerOpen" @click="pickerOpen = !pickerOpen">使用引导</button>
    <section v-if="canContinue" id="s2o-continuation" aria-label="继续章节引导">
      <p>下一章：{{ nextTitle }}</p>
      <p v-if="!status?.note">5 秒后自动继续，也可立即继续。</p>
      <p v-if="status?.note" role="alert">{{ status.note }}</p>
      <div class="s2o-continue-actions">
        <button id="s2o-continue" ref="continueButton" type="button" :disabled="status?.launching" @click="continueChapter">继续引导</button>
        <button type="button" @click="endGuide">结束引导</button>
      </div>
    </section>
    <div v-if="status?.available && status.note && !status.paused" id="s2o-note" role="status">{{ status.note }}</div>
    <ReplayPicker v-if="canReplay && pickerOpen && status" :modules="status.modules" :viewed-modules="status.viewedModules" @select="replay" @close="closePicker" />
  </Teleport>
</template>
