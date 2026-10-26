import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminSettingsStore } from '../adminSettings'

const mocks = vi.hoisted(() => ({ getSettings: vi.fn(), getConfig: vi.fn() }))
vi.mock('@/api', () => ({ adminAPI: { settings: { getSettings: mocks.getSettings }, payment: { getConfig: mocks.getConfig } } }))
beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); setActivePinia(createPinia())
  mocks.getConfig.mockResolvedValue({ data: { enabled: false } })
})
it('keeps the cached default on failed fetches', async () => {
  localStorage.setItem('cache_hit_rate_default_range_cached', 'yesterday')
  mocks.getSettings.mockRejectedValue(new Error('fixture offline'))
  const store = useAdminSettingsStore(); await store.fetch()
  expect(store.cacheHitRateDefaultRange).toBe('yesterday')
})
it('normalizes invalid fetched and cached values to today', async () => {
  localStorage.setItem('cache_hit_rate_default_range_cached', 'week')
  const store = useAdminSettingsStore(); expect(store.cacheHitRateDefaultRange).toBe('today')
  store.setCacheHitRateDefaultRangeLocal('yesterday')
  mocks.getSettings.mockResolvedValue({ cache_hit_rate_default_range: 'week' }); await store.fetch(true)
  expect(store.cacheHitRateDefaultRange).toBe('today')
  expect(localStorage.getItem('cache_hit_rate_default_range_cached')).toBe('today')
})
it('does not let a fetch started before saving overwrite the saved period', async () => {
  let resolve!: (value: unknown) => void
  mocks.getSettings.mockImplementation(() => new Promise(r => { resolve = r }))
  const store = useAdminSettingsStore(); const fetch = store.fetch()
  store.setCacheHitRateDefaultRangeLocal('yesterday')
  resolve({ cache_hit_rate_default_range: 'today' }); await fetch
  expect(store.cacheHitRateDefaultRange).toBe('yesterday')
  expect(localStorage.getItem('cache_hit_rate_default_range_cached')).toBe('yesterday')
})
