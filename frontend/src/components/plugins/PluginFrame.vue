<template>
  <div class="relative overflow-hidden bg-white dark:bg-dark-900" :style="{ height: `${height}px` }">
    <div v-if="loading" class="absolute inset-0 flex items-center justify-center text-sm text-gray-500">
      {{ t('common.loading') }}
    </div>
    <div v-if="failure" role="alert" class="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
      <p class="text-sm text-gray-700 dark:text-gray-200">{{ failure }}</p>
      <button type="button" class="btn btn-secondary btn-sm" @click="loadSession">{{ t('admin.plugins.retry') }}</button>
    </div>
    <iframe
      v-if="session"
      ref="frame"
      :src="session.url"
      :title="entry.label"
      sandbox="allow-scripts"
      referrerpolicy="no-referrer"
      class="h-full w-full border-0"
      @load="frameLoaded"
      @error="frameFailed"
    />
    <TotpStepUpDialog :controller="stepUp" />
    <Teleport to="body">
      <div
        v-if="anchorRect"
        data-plugin-anchor-outline
        aria-hidden="true"
        class="pointer-events-none fixed z-30 rounded-lg border-2 border-primary-500"
        :style="{ left: `${anchorRect.x}px`, top: `${anchorRect.y}px`, width: `${anchorRect.width}px`, height: `${anchorRect.height}px` }"
      />
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import pluginsAPI, { type PluginBusinessSession, type PluginEntry } from '@/api/plugins'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { createPluginBridge, type PluginBridgeMessage, type PluginBridgeRequest } from './bridge'

const props = defineProps<{
  entry: PluginEntry
  anchors?: { navigation: HTMLElement | null; content: HTMLElement | null }
}>()
const emit = defineEmits<{ close: []; unavailable: [reason: 'disabled' | 'forbidden'] }>()
const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const stepUp = useStepUp()
const frame = ref<HTMLIFrameElement | null>(null)
const session = ref<PluginBusinessSession | null>(null)
const loading = ref(true)
const failure = ref('')
const height = ref(props.entry.mount === 'shell' ? 360 : 640)
const anchorRect = ref<{ x: number; y: number; width: number; height: number } | null>(null)
let sessionController: AbortController | undefined
let loadTimeout: number | undefined
let disposed = false

function context() {
  return { path: route.path, route_name: typeof route.name === 'string' ? route.name : '', user_id: auth.user?.id, role: auth.user?.role }
}

function bridgeError(cause: unknown) {
  if (isStepUpCancelled(cause)) return { code: 'CANCELLED', message: t('common.cancel'), retryable: false, outcome_unknown: false }
  const error = (cause || {}) as { reason?: string; code?: string; message?: string; metadata?: Record<string, unknown> }
  const blocked = isStepUpBlocked(cause)
  const message = blocked
    ? t(stepUpBlockReason(cause) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
    : error.reason === 'PLUGIN_UNAVAILABLE' ? t('admin.plugins.operationUnavailable') : (error.message || t('admin.plugins.operationFailed'))
  return {
    code: error.reason || error.code || 'PLUGIN_FAILED', message,
    retryable: error.metadata?.retryable === true || error.metadata?.retryable === 'true',
    outcome_unknown: error.metadata?.outcome_unknown === true || error.metadata?.outcome_unknown === 'true',
  }
}

function rejectRequest(): never {
  throw { reason: 'PLUGIN_INVALID_REQUEST', message: t('admin.plugins.bridgeRejected') }
}

const businessTypes = ['plugin.call', 'context.get', 'ui.resize', 'ui.notify']
const shellTypes = ['ui.close', 'ui.navigate', 'ui.anchor']
const bridge = createPluginBridge({
  frame: () => frame.value,
  token: () => session.value?.bridge_token,
  version: () => 2,
  accepts: type => businessTypes.includes(type) || (props.entry.mount === 'shell' && shellTypes.includes(type)),
  expectsResponse: () => true,
  handle: handleMessage,
  connected: documentConnected,
  timeoutPayload: { ok: false, error: { code: 'PLUGIN_TIMEOUT', message: t('admin.plugins.operationTimeout'), retryable: false, outcome_unknown: true } },
})

async function handleMessage(message: PluginBridgeMessage, request: PluginBridgeRequest): Promise<void> {
  const currentSession = session.value
  if (!currentSession) return
  try {
    switch (message.type) {
      case 'plugin.call': {
        const method = currentSession.entry.methods?.find(method => method.name === message.method)
        if (!method || !message.payload || typeof message.payload !== 'object' || Array.isArray(message.payload)) rejectRequest()
        const payload = message.payload as Record<string, unknown>
        const invoke = () => {
          if (!request.active()) throw new Error('Request expired')
          return method.access === 'write'
            ? pluginsAPI.action(currentSession.entry.plugin_id, method.name, payload, request.signal)
            : pluginsAPI.call(currentSession.entry.plugin_id, method.name, payload, request.signal)
        }
        const cancelStepUp = () => stepUp.onCancel()
        if (method.access === 'write') request.signal.addEventListener('abort', cancelStepUp, { once: true })
        try {
          const result = method.access === 'write' ? await stepUp.run(invoke) : await invoke()
          request.reply({ ok: true, result })
        } finally {
          request.signal.removeEventListener('abort', cancelStepUp)
        }
        break
      }
      case 'context.get':
        request.reply({ ok: true, context: context() })
        break
      case 'ui.resize': {
        if (typeof message.height !== 'number' || !Number.isFinite(message.height)) rejectRequest()
        const shell = props.entry.mount === 'shell'
        height.value = Math.min(shell ? 720 : 960, Math.max(shell ? 240 : 160, Math.round(message.height)))
        request.reply({ ok: true })
        break
      }
      case 'ui.notify': {
        if (typeof message.message !== 'string' || !['info', 'success', 'error'].includes(String(message.level))) rejectRequest()
        const text = message.message.slice(0, 500)
        if (text) {
          if (message.level === 'error') app.showError(text)
          else if (message.level === 'success') app.showSuccess(text)
          else app.showInfo(text)
        }
        request.reply({ ok: true })
        break
      }
      case 'ui.close':
        request.reply({ ok: true })
        emit('close')
        break
      case 'ui.navigate': {
        const destinations: Record<string, string> = { dashboard: auth.isAdmin ? '/admin/dashboard' : '/dashboard', profile: '/profile', keys: '/keys' }
        if (typeof message.target !== 'string' || !Object.prototype.hasOwnProperty.call(destinations, message.target)) rejectRequest()
        request.reply({ ok: true })
        await router.push(destinations[message.target])
        break
      }
      case 'ui.anchor': {
        clearAnchor()
        if (message.target !== null && message.target !== 'navigation' && message.target !== 'content') rejectRequest()
        if (message.target !== null) {
          const element = props.anchors?.[message.target]
          if (!element) rejectRequest()
          const rect = element.getBoundingClientRect()
          const x = Math.max(0, Math.min(window.innerWidth, rect.x))
          const y = Math.max(0, Math.min(window.innerHeight, rect.y))
          anchorRect.value = { x, y, width: Math.max(0, Math.min(window.innerWidth, rect.right) - x), height: Math.max(0, Math.min(window.innerHeight, rect.bottom) - y) }
        }
        request.reply({ ok: true, rect: anchorRect.value ? { ...anchorRect.value } : null })
        break
      }
    }
  } catch (cause) {
    const active = request.active()
    request.reply({ ok: false, error: bridgeError(cause) })
    const reason = (cause as { reason?: string })?.reason
    if (active && (reason === 'PLUGIN_DISABLED' || reason === 'PLUGIN_FORBIDDEN')) emit('unavailable', reason === 'PLUGIN_DISABLED' ? 'disabled' : 'forbidden')
  }
}

function clearAnchor(): void { anchorRect.value = null }

function clearDocument(): void {
  bridge.disconnect()
  stepUp.onCancel()
  clearAnchor()
  window.clearTimeout(loadTimeout)
}

async function loadSession(): Promise<void> {
  sessionController?.abort()
  clearDocument()
  session.value = null
  loading.value = true
  failure.value = ''
  const controller = new AbortController()
  sessionController = controller
  try {
    const result = await pluginsAPI.createSession(props.entry.plugin_id, props.entry.entry_id, controller.signal)
    if (disposed || controller.signal.aborted) return
    if (result.ui_bridge_version !== 2 || result.entry.plugin_id !== props.entry.plugin_id || result.entry.entry_id !== props.entry.entry_id ||
      result.entry.revision !== props.entry.revision || result.entry.mount !== props.entry.mount || !auth.user ||
      !result.entry.roles.includes(auth.user.role) || (result.entry.mount === 'admin' && !auth.isAdmin)) rejectRequest()
    session.value = result
    loadTimeout = window.setTimeout(frameFailed, 30_000)
  } catch (cause) {
    if (disposed || controller.signal.aborted) return
    loading.value = false
    failure.value = (cause as { reason?: string })?.reason === 'PLUGIN_FORBIDDEN' ? t('admin.plugins.pageForbidden') : t('admin.plugins.pageLoadFailed')
  }
}

function frameLoaded(): void {
  if (!session.value) return
  stepUp.onCancel()
  clearAnchor()
  window.clearTimeout(loadTimeout)
  loading.value = true
  loadTimeout = window.setTimeout(frameFailed, 30_000)
  bridge.documentLoaded()
}

function documentConnected(): void {
  window.clearTimeout(loadTimeout)
  loading.value = false
  bridge.send({ type: 'context.changed', context: context() })
}

function frameFailed(): void {
  clearDocument()
  session.value = null
  loading.value = false
  failure.value = t('admin.plugins.pageLoadFailed')
}

watch(() => route.fullPath, () => {
  bridge.reset()
  stepUp.onCancel()
  clearAnchor()
  bridge.send({ type: 'context.changed', context: context() })
})
watch([() => props.entry.plugin_id, () => props.entry.entry_id, () => props.entry.revision], () => { void loadSession() })
watch(() => `${auth.token || ''}:${auth.user?.id || ''}:${auth.user?.role || ''}:${auth.user?.status || ''}`, () => {
  sessionController?.abort()
  clearDocument()
  session.value = null
  loading.value = false
  // The entries owner replaces this frame after an authentication change.
  // Token renewal is not a permission failure and must not close a new shell.
}, { flush: 'sync' })

onMounted(() => {
  bridge.start()
  window.addEventListener('resize', clearAnchor)
  window.addEventListener('scroll', clearAnchor, true)
  void loadSession()
})
onBeforeUnmount(() => {
  disposed = true
  sessionController?.abort()
  clearDocument()
  bridge.stop()
  window.removeEventListener('resize', clearAnchor)
  window.removeEventListener('scroll', clearAnchor, true)
})
</script>
