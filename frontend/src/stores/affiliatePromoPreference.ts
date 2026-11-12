import { defineStore } from 'pinia'
import { ref } from 'vue'

const keyFor = (id: number) => `sub2api:affiliate-promo:collapsed:${id}`

export const useAffiliatePromoPreferenceStore = defineStore('affiliate-promo-preference', () => {
  const userId = ref<number | null>(null)
  const collapsed = ref(false)

  function bindUser(id: number | null): void {
    if (userId.value === id) return
    userId.value = id
    collapsed.value = false
    if (id === null) return
    try { collapsed.value = window.localStorage.getItem(keyFor(id)) === '1' } catch { /* Storage may be blocked. */ }
  }

  function toggleCollapsed(): boolean {
    if (userId.value === null) return false
    collapsed.value = !collapsed.value
    try {
      window.localStorage.setItem(keyFor(userId.value), collapsed.value ? '1' : '0')
      return true
    } catch { return false }
  }

  function applyStorageEvent(event: StorageEvent): void {
    if (userId.value === null || event.key !== keyFor(userId.value)) return
    if (event.newValue !== '1' && event.newValue !== '0') return
    try {
      if (event.storageArea !== window.localStorage) return
      // Events are asynchronous: a newer local/user choice may already exist.
      const latest = window.localStorage.getItem(keyFor(userId.value))
      if (latest === '1' || latest === '0') collapsed.value = latest === '1'
    } catch { /* Keep the current choice if storage has become unavailable. */ }
  }

  return { userId, collapsed, bindUser, toggleCollapsed, applyStorageEvent }
})
