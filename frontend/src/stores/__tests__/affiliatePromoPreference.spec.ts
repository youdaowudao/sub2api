import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAffiliatePromoPreferenceStore } from '../affiliatePromoPreference'

const key = (id: number) => `sub2api:affiliate-promo:collapsed:${id}`
beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })
afterEach(() => { vi.restoreAllMocks() })

describe('referral card preference', () => {
  it('persists both choices through app recreation and logout, separately for each account', () => {
    const store = useAffiliatePromoPreferenceStore()
    store.bindUser(1)
    expect(store.collapsed).toBe(false)
    expect(store.toggleCollapsed()).toBe(true)
    expect(localStorage.getItem(key(1))).toBe('1')
    store.bindUser(null); store.bindUser(2)
    expect(store.collapsed).toBe(false)
    store.bindUser(1)
    expect(store.collapsed).toBe(true)
    setActivePinia(createPinia())
    const restored = useAffiliatePromoPreferenceStore()
    restored.bindUser(1)
    expect(restored.collapsed).toBe(true)
    expect(restored.toggleCollapsed()).toBe(true)
    expect(localStorage.getItem(key(1))).toBe('0')
    restored.bindUser(null); restored.bindUser(1)
    expect(restored.collapsed).toBe(false)
  })

  it('does not let sidebar remounts overwrite the in-memory choice when storage is denied', () => {
    const store = useAffiliatePromoPreferenceStore()
    store.bindUser(1)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
    expect(store.toggleCollapsed()).toBe(false)
    store.bindUser(1)
    expect(store.collapsed).toBe(true)
  })

  it('accepts only current-account storage events and never writes them back', () => {
    const store = useAffiliatePromoPreferenceStore()
    store.bindUser(1)
    const write = vi.spyOn(Storage.prototype, 'setItem')
    const event = (id: number, value: string) => {
      // A native event is delivered after the other tab has changed storage.
      localStorage.setItem(key(id), value)
      write.mockClear()
      return new StorageEvent('storage', { key: key(id), newValue: value, storageArea: window.localStorage })
    }
    store.applyStorageEvent(event(2, '1'))
    store.applyStorageEvent(event(1, 'invalid'))
    expect(store.collapsed).toBe(false)
    store.applyStorageEvent(event(1, '1'))
    expect(store.collapsed).toBe(true)
    store.applyStorageEvent(event(1, '0'))
    expect(store.collapsed).toBe(false)
    expect(write).not.toHaveBeenCalled()
  })

  it('keeps the latest choice when an older cross-tab event arrives after local toggles', () => {
    const store = useAffiliatePromoPreferenceStore()
    store.bindUser(1)
    localStorage.setItem(key(1), '1')
    const delayed = new StorageEvent('storage', { key: key(1), newValue: '1', storageArea: window.localStorage })
    store.toggleCollapsed(); store.toggleCollapsed()
    expect(localStorage.getItem(key(1))).toBe('0')
    expect(store.collapsed).toBe(false)
    store.applyStorageEvent(delayed)
    expect(store.collapsed).toBe(false)
    expect(localStorage.getItem(key(1))).toBe('0')
  })

  it('defaults safely on invalid or unreadable data and never writes without an account', () => {
    localStorage.setItem(key(1), 'true')
    const store = useAffiliatePromoPreferenceStore()
    store.bindUser(1); expect(store.collapsed).toBe(false)
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied') })
    store.bindUser(2); expect(store.collapsed).toBe(false)
    store.bindUser(null); expect(store.toggleCollapsed()).toBe(false)
    expect(store.collapsed).toBe(false)
  })
})
