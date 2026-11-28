import { computed, onUnmounted, reactive, watch } from 'vue'
import { getActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

export interface AdminUserHistoryEntry {
  id: number
  email: string
  lastQueriedAt: number
  pinned: boolean
}

const limit = 20
const byAdmin = reactive(new Map<number, AdminUserHistoryEntry[]>())
const unavailable = reactive(new Set<number>())
const keyFor = (id: number) => 'admin-user-query-history:' + id

function normalize(value: unknown): AdminUserHistoryEntry[] {
  if (!Array.isArray(value)) return []
  const unique = new Map<number, AdminUserHistoryEntry>()
  for (const entry of value) {
    if (!entry || !Number.isSafeInteger(entry.id) || entry.id <= 0 ||
        typeof entry.email !== 'string' || !entry.email ||
        !Number.isFinite(entry.lastQueriedAt) || typeof entry.pinned !== 'boolean') continue
    const previous = unique.get(entry.id)
    if (!previous || entry.lastQueriedAt >= previous.lastQueriedAt) {
      unique.set(entry.id, { id: entry.id, email: entry.email, lastQueriedAt: entry.lastQueriedAt, pinned: entry.pinned })
    }
  }
  const sorted = [...unique.values()].sort((a, b) => b.lastQueriedAt - a.lastQueriedAt || a.id - b.id)
  return [...sorted.filter(x => x.pinned), ...sorted.filter(x => !x.pinned).slice(0, limit)]
}

function read(id: number): AdminUserHistoryEntry[] {
  try {
    const raw = localStorage.getItem(keyFor(id))
    const items = normalize(raw ? JSON.parse(raw) : [])
    unavailable.delete(id)
    return items
  } catch {
    unavailable.add(id)
    return byAdmin.get(id) || []
  }
}

export function useAdminUserHistory() {
  const auth = getActivePinia() ? useAuthStore() : null
  const adminId = computed(() => auth?.isAdmin && Number.isSafeInteger(auth.user?.id) ? auth.user!.id : null)
  const entries = computed(() => adminId.value == null ? [] : byAdmin.get(adminId.value) || [])
  watch(adminId, id => { if (id != null && !unavailable.has(id)) byAdmin.set(id, read(id)) }, { immediate: true })

  function sync(event: StorageEvent) {
    const id = adminId.value
    if (id != null && (event.key == null || event.key === keyFor(id))) byAdmin.set(id, read(id))
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('storage', sync)
    onUnmounted(() => window.removeEventListener('storage', sync))
  }

  function update(change: (items: AdminUserHistoryEntry[]) => AdminUserHistoryEntry[]) {
    const id = adminId.value
    if (id == null) return
    // Re-read so a change in a different tab is not discarded. If saving has
    // failed, retain the in-memory history for this session.
    const previous = unavailable.has(id) ? entries.value : read(id)
    const next = normalize(change(previous))
    byAdmin.set(id, next)
    try {
      localStorage.setItem(keyFor(id), JSON.stringify(next))
      unavailable.delete(id)
    } catch {
      unavailable.add(id)
    }
  }

  return {
    adminId,
    enabled: computed(() => adminId.value != null),
    entries,
    failed: computed(() => adminId.value != null && unavailable.has(adminId.value)),
    record(id: number, email: string, actor = adminId.value) {
      if (actor !== adminId.value) return
      if (!Number.isSafeInteger(id) || id <= 0 || !email?.trim()) return
      update(items => [{ id, email, lastQueriedAt: Date.now(), pinned: items.find(x => x.id === id)?.pinned ?? false }, ...items.filter(x => x.id !== id)])
    },
    togglePin(id: number) { update(items => items.map(x => x.id === id ? { ...x, pinned: !x.pinned } : x)) },
    remove(id: number) { update(items => items.filter(x => x.id !== id)) },
    clear() { update(() => []) }
  }
}
