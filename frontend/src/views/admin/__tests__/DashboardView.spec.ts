import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats, UserSpendingRankingItem } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking, refreshBatchImageAccess, showError, push } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn(),
  refreshBatchImageAccess: vi.fn(),
  showError: vi.fn(),
  push: vi.fn()
}))

vi.mock('vue-chartjs', () => ({
  Line: { name: 'Line', props: ['data', 'options'], template: '<div class="line-chart" />' }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: false, refreshBatchImageAccess })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => key === 'admin.dashboard.connectionUsageUsers' ? params?.count : key === 'admin.dashboard.connectionUsageAsOf' ? params?.time : key
    })
  }
})

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  total_account_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  today_account_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

const windowStart = '2026-11-27T00:37:00.000Z'
const windowEnd = '2026-11-27T01:37:00.000Z'
const trendPoint = {
  date: windowStart,
  requests: 1,
  input_tokens: 10,
  output_tokens: 20,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  total_tokens: 30,
  cost: 0.1,
  actual_cost: 0.08
}

const mountDashboard = () => mount(DashboardView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    LoadingSpinner: true,
    Icon: true,
    DateRangePicker: true,
    Select: true,
    ModelDistributionChart: true,
    TokenUsageTrend: true,
    Line: true
  } }
})

const rankingItem = (id: number, username: string): UserSpendingRankingItem => ({
  user_id: id, username, email: '', requests: 4, tokens: 50, actual_cost: 2
})
const rankingResponse = (...items: UserSpendingRankingItem[]) => ({
  ranking: items, total_actual_cost: items.length * 2, total_requests: items.length * 4,
  total_tokens: items.length * 50, total_users: items.length, start_date: '', end_date: ''
})
const deferred = () => {
  let resolve!: (value: any) => void
  let reject!: (error: Error) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

async function applyRange(wrapper: ReturnType<typeof mountDashboard>, start: string, end: string) {
  wrapper.findComponent({ name: 'DateRangePicker' }).vm.$emit('change', { startDate: start, endDate: end, preset: null })
  await flushPromises()
}
const button = (wrapper: ReturnType<typeof mountDashboard>, key: string) => wrapper.findAll('button').find(b => b.text() === key)!
const ranking = (wrapper: ReturnType<typeof mountDashboard>) => wrapper.findComponent({ name: 'AdminDashboardUserRanking' })

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-11-27T01:37:25Z'))
    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()
    refreshBatchImageAccess.mockReset()
    showError.mockReset()
    push.mockReset()
    getSnapshotV2.mockResolvedValue({ stats: createDashboardStats(), trend: [trendPoint], models: [], groups: [] })
    getUserUsageTrend.mockResolvedValue({ trend: [] })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [], total_actual_cost: 0, total_requests: 0, total_tokens: 0, total_users: 0,
      start_date: '', end_date: ''
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('V1 removes the old cards, quick actions and user trend requests', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect.soft(wrapper.text()).not.toContain('admin.dashboard.apiKeys')
      expect.soft(wrapper.text()).not.toContain('admin.dashboard.accounts')
      expect.soft(wrapper.text()).not.toContain('admin.dashboard.quickActions')
      expect.soft(wrapper.text()).not.toContain('admin.dashboard.recentUsage')
      expect.soft(getUserUsageTrend).not.toHaveBeenCalled()
      expect(refreshBatchImageAccess).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }
  })

  it('V2 defaults to the last sixty complete minutes', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(getSnapshotV2).toHaveBeenCalledTimes(1)
      const [params] = getSnapshotV2.mock.calls[0]
      expect(params).toStrictEqual({
        start_time: windowStart,
        end_time: windowEnd,
        granularity: 'minute',
        include_stats: true,
        include_trend: true,
        include_model_stats: true,
        include_group_stats: true,
        include_users_trend: false
      })
      expect(params).not.toHaveProperty('start_date')
      expect(params).not.toHaveProperty('end_date')
      expect(wrapper.getComponent({ name: 'TokenUsageTrend' }).props('showCacheHitRate')).toBe(false)
    } finally {
      wrapper.unmount()
    }
  })

  it('V3 loads matching current and yesterday ranking windows and uses local minute labels', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect.soft(getUserSpendingRanking).toHaveBeenCalledTimes(2)
      expect.soft(getUserSpendingRanking).toHaveBeenCalledWith(expect.objectContaining({
        start_time: windowStart, end_time: windowEnd, limit: 12
      }))
      expect.soft(getUserSpendingRanking).toHaveBeenCalledWith(expect.objectContaining({
        start_time: '2026-11-26T00:37:00.000Z', end_time: '2026-11-26T01:37:00.000Z', limit: 50
      }))
      const local = new Date(windowStart)
      const label = `${String(local.getHours()).padStart(2, '0')}:${String(local.getMinutes()).padStart(2, '0')}`
      expect(wrapper.findComponent({ name: 'TokenUsageTrend' }).props('trendData')).toEqual([
        { ...trendPoint, date: label }
      ])
    } finally {
      wrapper.unmount()
    }
  })

  it('V5 does not issue requests automatically after five minutes', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const counts = [getSnapshotV2, getUserUsageTrend, getUserSpendingRanking].map(fn => fn.mock.calls.length)
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
      await flushPromises()
      expect([getSnapshotV2, getUserUsageTrend, getUserSpendingRanking].map(fn => fn.mock.calls.length)).toEqual(counts)
    } finally {
      wrapper.unmount()
    }
  })

  it('V4 applies today and seven-day ranges, reloads on granularity change, then restores last hour', async () => {
    getUserSpendingRanking.mockResolvedValue(rankingResponse(rankingItem(1, 'alice')))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(button(wrapper, 'admin.dashboard.lastHour').attributes('aria-pressed')).toBe('true')
      expect(ranking(wrapper).props('showCompare')).toBe(true)
      expect(wrapper.text()).toContain('admin.dashboard.vsYesterday')
      expect(wrapper.findComponent({ name: 'Select' }).exists()).toBe(false)

      for (const [start, end, grain] of [
        ['2026-11-27', '2026-11-27', 'hour'], ['2026-11-21', '2026-11-27', 'day']
      ]) {
        const calls = getUserSpendingRanking.mock.calls.length
        await applyRange(wrapper, start, end)
        expect(getSnapshotV2.mock.lastCall?.[0]).toMatchObject({ start_date: start, end_date: end, granularity: grain, include_stats: false })
        expect(getSnapshotV2.mock.lastCall?.[0]).not.toHaveProperty('start_time')
        expect(getUserSpendingRanking.mock.calls.slice(calls)).toEqual([[{ start_date: start, end_date: end, limit: 12 }]])
        expect(ranking(wrapper).props('showCompare')).toBe(false)
        expect(wrapper.text()).not.toContain('admin.dashboard.vsYesterday')
        expect(wrapper.findComponent({ name: 'Select' }).exists()).toBe(true)
        expect(button(wrapper, 'admin.dashboard.lastHour').attributes('aria-pressed')).toBe('false')
      }
      const calls = getSnapshotV2.mock.calls.length
      const select = wrapper.findComponent({ name: 'Select' })
      select.vm.$emit('update:modelValue', 'hour')
      select.vm.$emit('change', 'hour')
      await flushPromises()
      expect(getSnapshotV2).toHaveBeenCalledTimes(calls + 1)
      expect(getSnapshotV2.mock.lastCall?.[0].granularity).toBe('hour')
      await button(wrapper, 'admin.dashboard.lastHour').trigger('click')
      await flushPromises()
      expect(getSnapshotV2.mock.lastCall?.[0]).toMatchObject({ start_time: windowStart, end_time: windowEnd, granularity: 'minute', include_stats: false })
      expect(getSnapshotV2.mock.lastCall?.[0]).not.toHaveProperty('start_date')
      expect(ranking(wrapper).props('showCompare')).toBe(true)
      expect(wrapper.findComponent({ name: 'Select' }).exists()).toBe(false)
    } finally { wrapper.unmount() }
  })

  it('V6 renders six fixed overview cards and distinguishes today average from historical average', async () => {
    const stats: DashboardStats = {
      ...createDashboardStats(), today_requests: 12, total_requests: 123,
      today_tokens: 1500, total_tokens: 23000, today_actual_cost: 3,
      today_account_cost: 2, today_cost: 4, total_actual_cost: 51,
      total_account_cost: 42, total_cost: 63, today_new_users: 3, active_users: 7,
      total_users: 23, rpm: 8, tpm: 6000, average_duration_ms: 9999
    }
    getSnapshotV2.mockResolvedValue({ stats, trend: [], models: [], groups: [] })
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const cards = wrapper.get('[data-testid="dashboard-overview"]').findAll('.card')
      expect(cards).toHaveLength(6)
      const paragraphs = (card: number) => cards[card].findAll('p').map(p => p.text())
      expect(paragraphs(0)).toEqual(['admin.dashboard.todayRequestsCard', '12', 'common.total: 123'])
      expect(paragraphs(1)).toEqual(['admin.dashboard.todayTokens', '1.50K', 'common.total: 23.00K'])
      expect(paragraphs(2)).toEqual(['admin.dashboard.todayConsumption', '$3.00', '$2.00 / $4.00', 'common.total: $51.00'])
      expect(cards[2].findAll('[title]').at(-1)?.attributes('title')).toBe('admin.dashboard.actual: $51.00 / admin.dashboard.accountCost: $42.00 / admin.dashboard.standard: $63.00')
      expect(paragraphs(3)).toEqual(['admin.dashboard.users', '+3', 'admin.dashboard.activeUsers: 7', 'common.total: 23'])
      expect(paragraphs(4)).toEqual(['admin.dashboard.performance', '8', '6.00K', 'admin.dashboard.todayAvgResponse: —'])
      expect(cards[4].findAll('span').map(span => span.text())).toContain('RPM')
      expect(cards[4].findAll('span').map(span => span.text())).toContain('TPM')
      expect(wrapper.get('[data-testid="today-average-response"]').text()).toBe('—')
      getSnapshotV2.mockResolvedValue({ stats: { ...stats, today_average_duration_ms: null } })
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="today-average-response"]').text()).toBe('—')
      getSnapshotV2.mockResolvedValue({ stats: { ...stats, today_average_duration_ms: 1250 } })
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="today-average-response"]').text()).toBe('1.25s')
      expect(cards[4].text()).not.toContain('10.00s')
      getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [], stats: createDashboardStats() })
      await applyRange(wrapper, '2026-11-21', '2026-11-27')
      expect(wrapper.get('[data-testid="today-average-response"]').text()).toBe('1.25s')
    } finally { wrapper.unmount() }
  })

  it('connection usage keeps its sampling time on range changes and failures; refresh replaces it', async () => {
    const old = { ws_users: 12, http_users: 45, window_end: '2026-11-27T01:35:30Z' }
    const latest = { ws_users: 0, http_users: 2, window_end: '2026-11-27T01:36:45Z' }
    getSnapshotV2.mockResolvedValue({ stats: { ...createDashboardStats(), connection_usage: old }, generated_at: windowEnd })
    const wrapper = mountDashboard()
    const ws = () => wrapper.get('[data-testid="connection-ws-users"]').text()
    const http = () => wrapper.get('[data-testid="connection-http-users"]').text()
    const time = () => wrapper.get('[data-testid="connection-usage-time"]').text()
    try {
      await flushPromises()
      expect(ws()).toBe('12')
      expect(http()).toBe('45')
      expect(time()).toContain('35:30')
      const originalTime = time()
      expect(wrapper.get('[data-testid="connection-usage"]').text()).toContain('admin.dashboard.connectionUsageScope')
      getSnapshotV2.mockResolvedValue({ stats: { ...createDashboardStats(), connection_usage: latest } })
      await button(wrapper, 'admin.usage.recentRange.30').trigger('click')
      await flushPromises()
      expect(getSnapshotV2.mock.calls.at(-1)?.[0].include_stats).toBe(false)
      expect(ws()).toBe('12')
      expect(time()).toBe(originalTime)
      await button(wrapper, 'dates.today').trigger('click')
      await flushPromises()
      expect(ws()).toBe('12')
      await applyRange(wrapper,'2026-11-21','2026-11-27')
      expect(ws()).toBe('12')
      getSnapshotV2.mockRejectedValueOnce(new Error('snapshot failure'))
      await button(wrapper,'common.refresh').trigger('click')
      await flushPromises()
      expect(showError).toHaveBeenCalled()
      expect(ws()).toBe('12')
      expect(time()).toBe(originalTime)
      await button(wrapper,'common.refresh').trigger('click')
      await flushPromises()
      expect(getSnapshotV2.mock.calls.at(-1)?.[0].include_stats).toBe(true)
      expect(ws()).toBe('0')
      expect(http()).toBe('2')
      expect(time()).toContain('36:45')
      const obsolete = deferred()
      getSnapshotV2.mockReturnValueOnce(obsolete.promise)
      await button(wrapper,'common.refresh').trigger('click')
      await button(wrapper,'admin.usage.recentRange.180').trigger('click')
      await flushPromises()
      obsolete.resolve({ stats: { ...createDashboardStats(), connection_usage: old } })
      await flushPromises()
      expect(ws()).toBe('0')
      expect(time()).toContain('36:45')
    } finally { wrapper.unmount() }
  })

  it('connection usage displays unavailable rather than zero for old or invalid responses', async () => {
    const valid = { ws_users: 0, http_users: 0, window_end: '2026-11-27T01:35:30Z' }
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(wrapper.get('[data-testid="connection-ws-users"]').text()).toBe('—')
      expect(wrapper.get('[data-testid="connection-usage-time"]').text()).toBe('admin.dashboard.connectionUsageMissing')
      for (const usage of [valid, null, undefined, { ...valid, ws_users: -1 }, { ...valid, http_users: 0.5 }, { ...valid, window_end: 'invalid' }]) {
        getSnapshotV2.mockResolvedValue({ stats: { ...createDashboardStats(), connection_usage: usage } })
        await button(wrapper,'common.refresh').trigger('click')
        await flushPromises()
        const expected = usage === valid ? '0' : '—'
        expect(wrapper.get('[data-testid="connection-ws-users"]').text()).toBe(expected)
        expect(wrapper.get('[data-testid="connection-http-users"]').text()).toBe(expected)
        if (usage !== valid) expect(wrapper.get('[data-testid="connection-usage-time"]').text()).toBe('admin.dashboard.connectionUsageMissing')
      }
    } finally { wrapper.unmount() }
  })

  it('V10 clears failed chart data and hides loading or failed ranking rows; refresh follows only snapshot', async () => {
    const initial = rankingItem(1, 'old-user')
    const group = { group_id: 1, group_name: 'old-group', requests: 3, total_tokens: 10, cost: 1, actual_cost: 1, cache_hit_rate: 50 }
    const model = { model: 'old-model', requests: 3, total_tokens: 10, cost: 1, actual_cost: 1 }
    getSnapshotV2.mockResolvedValueOnce({ stats: createDashboardStats(), trend: [trendPoint], models: [model], groups: [group] })
    getUserSpendingRanking.mockResolvedValue(rankingResponse(initial))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('old-user')
      expect(wrapper.text()).toContain('old-group')
      const snapshot = deferred()
      const nextRank = deferred()
      getSnapshotV2.mockReturnValueOnce(snapshot.promise)
      getUserSpendingRanking.mockReturnValueOnce(nextRank.promise)
      await applyRange(wrapper, '2026-11-21', '2026-11-27')
      expect(button(wrapper, 'common.refresh').attributes('disabled')).toBeDefined()
      expect(ranking(wrapper).props('loading')).toBe(true)
      expect(wrapper.text()).not.toContain('old-user')
      snapshot.reject(new Error('test snapshot failure'))
      await flushPromises()
      expect(showError).toHaveBeenCalledWith('admin.dashboard.failedToLoad')
      expect(wrapper.findComponent({ name: 'TokenUsageTrend' }).props('trendData')).toEqual([])
      expect(wrapper.findComponent({ name: 'ModelDistributionChart' }).props('modelStats')).toEqual([])
      expect(wrapper.findComponent({ name: 'AdminDashboardGroupCacheRate' }).props('groups')).toEqual([])
      expect(wrapper.text()).not.toContain('old-group')
      expect(button(wrapper, 'common.refresh').attributes('disabled')).toBeUndefined()
      expect(ranking(wrapper).props('loading')).toBe(true)
      nextRank.reject(new Error('test ranking failure'))
      await flushPromises()
      expect(ranking(wrapper).props('error')).toBe(true)
      expect(wrapper.text()).toContain('admin.dashboard.failedToLoad')
      expect(wrapper.text()).not.toContain('old-user')
    } finally { wrapper.unmount() }
  })

  it('V10 discards late current and yesterday responses when switching ranges', async () => {
    const staleRank = deferred()
    const staleYesterday = deferred()
    const current = rankingItem(2, 'current-user')
    getUserSpendingRanking.mockReturnValueOnce(staleRank.promise).mockReturnValueOnce(staleYesterday.promise)
    getUserSpendingRanking.mockResolvedValue(rankingResponse(current))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      await applyRange(wrapper, '2026-11-21', '2026-11-27')
      expect(ranking(wrapper).props('items')).toEqual([current])
      expect(ranking(wrapper).props('compare')).toBeNull()
      staleRank.resolve(rankingResponse(rankingItem(1, 'stale-rank')))
      staleYesterday.resolve(rankingResponse(rankingItem(1, 'stale-yesterday')))
      await flushPromises()
      expect(ranking(wrapper).props('items')).toEqual([current])
      expect(ranking(wrapper).props('compare')).toBeNull()
      expect(wrapper.text()).not.toContain('stale-rank')
      expect(wrapper.text()).not.toContain('stale-yesterday')
    } finally { wrapper.unmount() }
  })

  it('V10 clears a successful yesterday comparison on refresh and rejects stale yesterday results', async () => {
    const item = rankingItem(1, 'same-user')
    const initialYesterday = { ...item, actual_cost: 1 }
    getUserSpendingRanking.mockResolvedValueOnce(rankingResponse(item)).mockResolvedValueOnce(rankingResponse(initialYesterday))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const comparisonCell = () => ranking(wrapper).get('tbody tr').findAll('td').at(-1)!.text()
      expect(ranking(wrapper).props('compare')).toEqual({ items: [initialYesterday], totalUsers: 1, window: expect.any(String) })
      expect(comparisonCell()).toBe('↑ 100.0%')
      expect(getSnapshotV2.mock.lastCall?.[0]).toMatchObject({ start_time: windowStart, end_time: windowEnd })

      const failedYesterday = deferred()
      getUserSpendingRanking.mockResolvedValueOnce(rankingResponse(item)).mockReturnValueOnce(failedYesterday.promise)
      await vi.advanceTimersByTimeAsync(2 * 60 * 1000)
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(getSnapshotV2).toHaveBeenCalledTimes(2)
      expect(getSnapshotV2.mock.lastCall?.[0]).toStrictEqual({
        start_time: '2026-11-27T00:39:00.000Z', end_time: '2026-11-27T01:39:00.000Z',
        granularity: 'minute', include_stats: true, include_trend: true,
        include_model_stats: true, include_group_stats: true, include_users_trend: false
      })
      expect(getUserSpendingRanking.mock.calls.slice(2)).toEqual([
        [{ start_time: '2026-11-27T00:39:00.000Z', end_time: '2026-11-27T01:39:00.000Z', limit: 12 }],
        [{ start_time: '2026-11-26T00:39:00.000Z', end_time: '2026-11-26T01:39:00.000Z', limit: 50 }]
      ])
      // The current row has loaded, but yesterday is still pending. Do not show its previous-period value.
      expect(ranking(wrapper).props('loading')).toBe(false)
      expect(ranking(wrapper).text()).toContain('same-user')
      expect(ranking(wrapper).props('compare')).toBeNull()
      expect(comparisonCell()).toBe('—')
      failedYesterday.reject(new Error('test yesterday ranking failure'))
      await flushPromises()
      expect(ranking(wrapper).props('compare')).toBeNull()
      expect(comparisonCell()).toBe('—')

      const staleYesterday = deferred()
      getUserSpendingRanking.mockResolvedValueOnce(rankingResponse(item)).mockReturnValueOnce(staleYesterday.promise)
      await vi.advanceTimersByTimeAsync(60 * 1000)
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(comparisonCell()).toBe('—')
      const currentYesterday = { ...item, actual_cost: 4 }
      getUserSpendingRanking.mockResolvedValueOnce(rankingResponse(item)).mockResolvedValueOnce(rankingResponse(currentYesterday))
      await vi.advanceTimersByTimeAsync(60 * 1000)
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(getSnapshotV2.mock.lastCall?.[0]).toMatchObject({ start_time: '2026-11-27T00:41:00.000Z', end_time: '2026-11-27T01:41:00.000Z' })
      expect(ranking(wrapper).props('compare')).toEqual({ items: [currentYesterday], totalUsers: 1, window: expect.any(String) })
      expect(comparisonCell()).toBe('↓ 50.0%')
      staleYesterday.resolve(rankingResponse(initialYesterday))
      await flushPromises()
      expect(ranking(wrapper).props('compare')).toEqual({ items: [currentYesterday], totalUsers: 1, window: expect.any(String) })
      expect(comparisonCell()).toBe('↓ 50.0%')
    } finally { wrapper.unmount() }
  })

  it('V10 does not render overview or charts when the first snapshot fails', async () => {
    getSnapshotV2.mockRejectedValueOnce(new Error('test first load failure'))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(showError).toHaveBeenCalledWith('admin.dashboard.failedToLoad')
      expect(wrapper.find('[data-testid="dashboard-overview"]').exists()).toBe(false)
      expect(wrapper.findComponent({ name: 'TokenUsageTrend' }).exists()).toBe(false)
    } finally { wrapper.unmount() }
  })

  it('V11 passes precise model filters and preserves the cross-midnight window when drilling down', async () => {
    vi.setSystemTime(new Date(2026, 10, 27, 0, 20, 25))
    const item = rankingItem(9, 'midnight')
    getUserSpendingRanking.mockResolvedValue(rankingResponse(item))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const model = wrapper.findComponent({ name: 'ModelDistributionChart' })
      const oldInstance = model.vm.$.uid
      const end = new Date(2026, 10, 27, 0, 20).toISOString()
      const start = new Date(2026, 10, 26, 23, 20).toISOString()
      expect(model.props('filters')).toEqual({ start_time: start, end_time: end })
      expect(model.props('enableRankingView')).toBe(false)
      ranking(wrapper).vm.$emit('row-click', item)
      expect(push).toHaveBeenLastCalledWith({ path: '/admin/usage', query: { user_id: '9', start_time: start, end_time: end } })
      await applyRange(wrapper, '2026-11-21', '2026-11-27')
      expect(wrapper.findComponent({ name: 'ModelDistributionChart' }).vm.$.uid).not.toBe(oldInstance)
      expect(wrapper.findComponent({ name: 'ModelDistributionChart' }).props('filters')).toEqual({})
      ranking(wrapper).vm.$emit('row-click', item)
      expect(push).toHaveBeenLastCalledWith({ path: '/admin/usage', query: { user_id: '9', start_date: '2026-11-21', end_date: '2026-11-27' } })
    } finally { wrapper.unmount() }
  })

  it('V11 remounts the model chart for both same-mode range changes', async () => {
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const model = () => wrapper.getComponent({ name: 'ModelDistributionChart' })
      const initialHourUid = model().vm.$.uid
      expect(model().props('filters')).toEqual({ start_time: windowStart, end_time: windowEnd })
      await vi.advanceTimersByTimeAsync(60 * 1000)
      await button(wrapper, 'common.refresh').trigger('click')
      await flushPromises()
      expect(model().props('filters')).toEqual({ start_time: '2026-11-27T00:38:00.000Z', end_time: '2026-11-27T01:38:00.000Z' })
      expect.soft(model().vm.$.uid).not.toBe(initialHourUid)

      await applyRange(wrapper, '2026-11-21', '2026-11-27')
      const initialDateUid = model().vm.$.uid
      expect(model().props('startDate')).toBe('2026-11-21')
      expect(model().props('endDate')).toBe('2026-11-27')
      expect(model().props('filters')).toEqual({})
      await applyRange(wrapper, '2026-11-20', '2026-11-26')
      expect(model().props('startDate')).toBe('2026-11-20')
      expect(model().props('endDate')).toBe('2026-11-26')
      expect(model().props('filters')).toEqual({})
      expect.soft(model().vm.$.uid).not.toBe(initialDateUid)
    } finally { wrapper.unmount() }
  })

  it('V11 excludes the next date when the precise window ends at local midnight', async () => {
    vi.setSystemTime(new Date(2026, 10, 27, 0, 0, 25))
    const item = rankingItem(9, 'exact-midnight')
    getUserSpendingRanking.mockResolvedValue(rankingResponse(item))
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      expect(getSnapshotV2.mock.lastCall?.[0]).toMatchObject({
        start_time: new Date(2026, 10, 26, 23, 0).toISOString(),
        end_time: new Date(2026, 10, 27, 0, 0).toISOString()
      })
      await ranking(wrapper).get('tbody tr').trigger('click')
      expect(push).toHaveBeenCalledTimes(1)
      expect(push).toHaveBeenLastCalledWith({ path: '/admin/usage', query: { user_id: '9', start_time: new Date(2026, 10, 26, 23, 0).toISOString(), end_time: new Date(2026, 10, 27, 0, 0).toISOString() } })
    } finally { wrapper.unmount() }
  })
})
