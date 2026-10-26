import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useCacheHitRateDefaultRange } from '../useCacheHitRateDefaultRange'

const mocks = vi.hoisted(() => ({ getSettings: vi.fn(), getConfig: vi.fn() }))
vi.mock('@/api', () => ({ adminAPI: { settings: { getSettings: mocks.getSettings }, payment: { getConfig: mocks.getConfig } } }))

beforeEach(() => {
  vi.clearAllMocks(); localStorage.clear(); setActivePinia(createPinia())
  vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 9, 26, 12))
  mocks.getConfig.mockResolvedValue({ data: { enabled: false } })
})
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

it('uses the cached period before fetching and notifies when the server default arrives', async () => {
  localStorage.setItem('cache_hit_rate_default_range_cached', 'yesterday')
  let resolve!: (value: unknown) => void
  mocks.getSettings.mockImplementation(() => new Promise(r => { resolve = r }))
  const changed = vi.fn()
  const wrapper = mount(defineComponent({ setup() {
    const { dates } = useCacheHitRateDefaultRange(changed)
    return () => h('span', `${dates.value.start}/${dates.value.end}`)
  } }))
  expect(wrapper.text()).toBe('2026-10-25/2026-10-25')
  resolve({ cache_hit_rate_default_range: 'yesterdayAndToday' }); await flushPromises()
  expect(changed).toHaveBeenCalledTimes(1)
  expect(wrapper.text()).toBe('2026-10-25/2026-10-26')
  wrapper.unmount()
})

it('recomputes calendar dates when a list refreshes after midnight', async () => {
  mocks.getSettings.mockResolvedValue({ cache_hit_rate_default_range: 'today' })
  let readDates!: () => { start: string; end: string }
  const wrapper = mount(defineComponent({ setup() {
    const { dates } = useCacheHitRateDefaultRange(vi.fn())
    readDates = () => dates.value
    return () => h('span')
  } }))
  await flushPromises()
  expect(readDates()).toEqual({ start: '2026-10-26', end: '2026-10-26' })
  vi.setSystemTime(new Date(2026, 9, 27, 0, 1))
  expect(readDates()).toEqual({ start: '2026-10-27', end: '2026-10-27' })
  wrapper.unmount()
})
