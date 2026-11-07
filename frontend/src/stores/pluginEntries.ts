import { computed, ref, shallowRef, watch } from 'vue'
import { defineStore } from 'pinia'
import pluginsAPI, { pluginEntryKey, type PluginEntry, type PluginMount } from '@/api/plugins'
import { useAuthStore } from './auth'

export const usePluginEntriesStore = defineStore('plugin-entries', () => {
  const auth = useAuthStore()
  const entries = shallowRef<PluginEntry[]>([])
  const ready = ref(false)
  const error = shallowRef<unknown>(null)
  const identityVersion = ref(0)
  const selectedShellKey = ref<string | null>(null)
  let shellInitialized = false
  let consumers = 0
  let timer: number | undefined
  let controller: AbortController | undefined
  let pending: Promise<void> | undefined

  const canLoad = computed(() => auth.isAuthenticated && !!auth.user && auth.user.status === 'active')
  const visibleEntries = computed(() => entries.value.filter(entry => {
    const role = auth.user?.role
    return !!role && entry.roles.includes(role) && (entry.mount !== 'admin' || role === 'admin')
  }))
  const forMount = (mount: PluginMount) => visibleEntries.value.filter(entry => entry.mount === mount)

  function cancelRefresh(): void {
    controller?.abort()
    controller = undefined
    pending = undefined
  }

  async function refresh(): Promise<void> {
    if (!canLoad.value) return
    if (pending) return pending
    const current = new AbortController()
    controller = current
    const version = identityVersion.value
    pending = (async () => {
      try {
        const result = await pluginsAPI.listEntries(current.signal)
        if (current.signal.aborted || version !== identityVersion.value) return
        entries.value = result
        ready.value = true
        error.value = null
        const shells = forMount('shell')
        if (!shellInitialized) {
          selectedShellKey.value = shells[0] ? pluginEntryKey(shells[0]) : null
          shellInitialized = true
        } else if (!shells.some(entry => pluginEntryKey(entry) === selectedShellKey.value)) {
          selectedShellKey.value = null
        }
      } catch (cause) {
        if (current.signal.aborted || version !== identityVersion.value) return
        error.value = cause
        entries.value = []
        ready.value = false
        selectedShellKey.value = null
      } finally {
        if (controller === current) {
          controller = undefined
          pending = undefined
        }
      }
    })()
    return pending
  }

  function acquire(): () => void {
    consumers++
    if (consumers === 1) {
      void refresh()
      timer = window.setInterval(() => { void refresh() }, 5_000)
    }
    let released = false
    return () => {
      if (released) return
      released = true
      consumers--
      if (consumers === 0) {
        window.clearInterval(timer)
        timer = undefined
        cancelRefresh()
      }
    }
  }

  function openShell(entry: PluginEntry): void {
    if (forMount('shell').some(item => pluginEntryKey(item) === pluginEntryKey(entry))) {
      selectedShellKey.value = pluginEntryKey(entry)
    }
  }

  function closeShell(): void {
    selectedShellKey.value = null
    shellInitialized = true
  }

  // Access-token replacement also denotes a new authenticated document context.
  // The token is used only as a private watch key and never sent to a frame.
  watch(() => `${auth.token || ''}:${auth.user?.id || ''}:${auth.user?.role || ''}:${auth.user?.status || ''}`, () => {
    cancelRefresh()
    // Clear selection before the list so synchronous shell watchers do not
    // interpret this authenticated-context reset as a disabled plugin.
    selectedShellKey.value = null
    entries.value = []
    ready.value = false
    error.value = null
    shellInitialized = false
    // Synchronous page watchers must join the new identity's request, never the
    // cancelled promise or entries that belonged to the previous identity.
    identityVersion.value++
    if (consumers > 0) void refresh()
  }, { flush: 'sync' })

  return { entries: visibleEntries, ready, error, identityVersion, selectedShellKey, forMount, refresh, acquire, openShell, closeShell }
})
