import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import DashboardView from '../DashboardView.vue'
import Charts from '@/components/user/dashboard/UserDashboardCharts.vue'
import Recent from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
const { trend, models, recent, stats } = vi.hoisted(() => ({ trend: vi.fn(), models: vi.fn(), recent: vi.fn(), stats: vi.fn() }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: stats, getDashboardTrend: trend, getDashboardModels: models, getByDateRange: recent } }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: vi.fn().mockResolvedValue({ platform_quotas: [] }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: {}, isSimpleMode: false, refreshUser: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (k: string) => k, locale: ref('en') }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 9, 25, 12))
  vi.clearAllMocks()
  stats.mockResolvedValue({}); trend.mockResolvedValue({ trend: [] }); models.mockResolvedValue({ models: [] }); recent.mockResolvedValue({ items: [] })
})
afterEach(() => vi.useRealTimers())
const log = (id: number) => ({ id, model: `model-${id}`, created_at: '2026-10-25', actual_cost: 0, total_cost: 0, input_tokens: 1, output_tokens: 1 })
async function view() {
  const w = mount(DashboardView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, UserDashboardStats: true, UserDashboardQuickActions: true, Doughnut: true, TokenUsageTrend: true, Icon: true, RouterLink: { template: '<a><slot /></a>' } } } })
  await flushPromises(); return w
}
async function range(w: ReturnType<typeof mount>, start: string, end: string) {
  const picker = w.getComponent(DateRangePicker)
  await picker.get('.date-picker-trigger').trigger('click')
  const inputs = picker.findAll('input[type="date"]')
  await inputs[0]!.setValue(start); await inputs[1]!.setValue(end)
  await picker.get('.date-picker-apply').trigger('click')
}
function deferred<T>() { let resolve!: (v: T) => void; let reject!: (e: Error) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
describe('user dashboard applied range', () => {
  it('requests today with hourly charts and changes recent usage and granularity together', async () => {
    const w = await view()
    expect(trend).toHaveBeenLastCalledWith({ start_date: '2026-10-25', end_date: '2026-10-25', granularity: 'hour' })
    expect(recent).toHaveBeenLastCalledWith('2026-10-25', '2026-10-25')
    expect(w.getComponent(Recent).text()).toContain('2026-10-25')
    await range(w, '2026-10-01', '2026-10-25'); await flushPromises()
    expect(trend).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-01', granularity: 'day' }))
    expect(recent).toHaveBeenLastCalledWith('2026-10-01', '2026-10-25')
    expect(w.getComponent(Recent).text()).toContain('2026-10-01 — 2026-10-25')
    const count = recent.mock.calls.length
    w.getComponent(Charts).vm.$emit('update:granularity', 'hour')
    w.getComponent(Charts).vm.$emit('granularityChange'); await flushPromises()
    expect(trend).toHaveBeenLastCalledWith(expect.objectContaining({ granularity: 'hour' }))
    expect(recent).toHaveBeenCalledTimes(count)
    await range(w, '2026-10-24', '2026-10-25'); await flushPromises()
    expect(w.getComponent(Charts).props('granularity')).toBe('hour')
  })
  it('ignores late data and late errors without stopping a newer loading state', async () => {
    const w = await view()
    const a = deferred<{ items: ReturnType<typeof log>[] }>(), b = deferred<{ items: ReturnType<typeof log>[] }>()
    const ta = deferred<{ trend: never[] }>(), tb = deferred<{ trend: never[] }>()
    recent.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    trend.mockReturnValueOnce(ta.promise).mockReturnValueOnce(tb.promise)
    await range(w, '2026-10-01', '2026-10-01')
    await range(w, '2026-10-02', '2026-10-02')
    a.resolve({ items: [log(1)] }); ta.reject(new Error('late')); await flushPromises()
    expect(w.getComponent(Recent).props('loading')).toBe(true)
    expect(w.getComponent(Charts).props('loading')).toBe(true)
    expect(w.text()).not.toContain('model-1'); expect(w.text()).not.toContain('dashboard.chartsLoadFailed')
    b.resolve({ items: [log(2)] }); tb.resolve({ trend: [] }); await flushPromises()
    expect(w.text()).toContain('model-2'); expect(w.text()).not.toContain('model-1')
    expect(w.getComponent(Recent).props('loading')).toBe(false)
    expect(w.getComponent(Charts).props('loading')).toBe(false)
  })
  it('does not overwrite newer charts or recent records when an earlier successful response arrives last', async () => {
    const w = await view()
    const a = deferred<{ items: ReturnType<typeof log>[] }>(), b = deferred<{ items: ReturnType<typeof log>[] }>()
    const ta = deferred<{ trend: { date: string }[] }>(), tb = deferred<{ trend: { date: string }[] }>()
    recent.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    trend.mockReturnValueOnce(ta.promise).mockReturnValueOnce(tb.promise)
    await range(w, '2026-10-01', '2026-10-01'); await range(w, '2026-10-02', '2026-10-02')
    b.resolve({ items: [log(2)] }); tb.resolve({ trend: [{ date: '2026-10-02' }] }); await flushPromises()
    a.resolve({ items: [log(1)] }); ta.resolve({ trend: [{ date: '2026-10-01' }] }); await flushPromises()
    expect(w.getComponent(Charts).props('trend')).toEqual([{ date: '2026-10-02' }])
    expect(w.text()).toContain('model-2'); expect(w.text()).not.toContain('model-1')
  })

  it('clears previous dates and distinguishes failures from empty data; refresh retains the applied dates', async () => {
    recent.mockResolvedValueOnce({ items: [log(1)] })
    const w = await view(); expect(w.text()).toContain('model-1')
    recent.mockRejectedValueOnce(new Error('offline')); trend.mockRejectedValueOnce(new Error('offline'))
    await range(w, '2026-10-03', '2026-10-03'); await flushPromises()
    expect(w.text()).toContain('dashboard.recentLoadFailed'); expect(w.text()).toContain('dashboard.chartsLoadFailed')
    expect(w.text()).not.toContain('model-1'); expect(w.text()).not.toContain('dashboard.noUsageRecords')
    await w.getComponent(Charts).get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(recent).toHaveBeenLastCalledWith('2026-10-03', '2026-10-03')
    expect(w.text()).not.toContain('dashboard.recentLoadFailed')
    expect(w.text()).toContain('dashboard.noUsageRecords')
  })
})
