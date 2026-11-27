import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, shallowMount, flushPromises } from '@vue/test-utils'
enableAutoUnmount(afterEach)
import { reactive } from 'vue'
import OpsDashboard from '../OpsDashboard.vue'
import { opsAPI } from '@/api/admin/ops'

const { getGroups } = vi.hoisted(() => ({ getGroups: vi.fn() }))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: getGroups } } }))

const route = reactive({ query: {} as Record<string, string> })
const settings = { opsMonitoringEnabled: true, opsQueryModeDefault: 'auto', fetch: vi.fn(async () => { settings.opsQueryModeDefault = 'raw' }) }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace: vi.fn(async (target: { query?: Record<string, string> }) => { if (target.query) route.query = target.query }) }) }))
vi.mock('@/stores', () => ({ useAdminSettingsStore: () => settings, useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/ops', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/ops')>()
  const api = Object.fromEntries(Object.keys(actual.opsAPI).map(key => [key, vi.fn().mockResolvedValue({})]))
  return { ...actual, opsAPI: api }
})
const mountDashboard = () => shallowMount(OpsDashboard, { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  OpsErrorRecordsPanel: { name: 'OpsErrorRecordsPanel', props: ['refreshToken', 'timeRange', 'customStartTime', 'customEndTime', 'platform', 'groupId', 'groupOptions', 'groupOptionsFailed'], methods: { focus: vi.fn(), refresh: vi.fn() }, template: '<section />' },
} } })
beforeEach(() => {
  vi.clearAllMocks(); route.query = {}; settings.opsQueryModeDefault = 'auto'
  getGroups.mockReset().mockResolvedValue([])
  vi.mocked(opsAPI.getAdvancedSettings).mockResolvedValue({ auto_refresh_enabled: false, auto_refresh_interval_seconds: 30 } as never)
})
describe('dashboard and embedded errors', () => {
  it('establishes the account range when the first overview failed and auto refresh recovers', async () => {
    vi.useFakeTimers()
    route.query={tr:'3h'}
    vi.mocked(opsAPI.getAdvancedSettings).mockResolvedValue({auto_refresh_enabled:true,auto_refresh_interval_seconds:30} as never)
    vi.mocked(opsAPI.getDashboardSnapshotV2).mockRejectedValueOnce(new Error('snapshot unavailable'))
    vi.mocked(opsAPI.getDashboardOverview).mockRejectedValueOnce(new Error('overview unavailable'))
    const window={start_time:'2026-11-27T00:00:00Z',end_time:'2026-11-27T03:00:00Z'}
    vi.mocked(opsAPI.getDashboardSnapshotV2).mockResolvedValueOnce({overview:window} as never)
    const w=mountDashboard()
    try {
      await flushPromises()
      const panel=w.getComponent({name:'AccountPerformancePanel'})
      expect(panel.props('range')).toBeNull()
      await vi.advanceTimersByTimeAsync(32_000);await flushPromises()
      expect(panel.props('range')).toMatchObject(window)
      expect(panel.props('refreshToken')).toBe(0)
    } finally {w.unmount();vi.useRealTimers()}
  })
  it('loads today using the local day and supports the three-hour toolbar without extra list refreshes', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 10, 27, 15, 10, 0))
    route.query = {tr:'today'}
    const wrapper = mountDashboard()
    try {
      await flushPromises()
      const header=wrapper.findComponent({name:'OpsDashboardHeader'})
      const panel=wrapper.findComponent({name:'OpsErrorRecordsPanel'})
      const first=vi.mocked(opsAPI.getDashboardSnapshotV2).mock.lastCall?.[0]
      expect(first).toMatchObject({start_time:new Date(2026,10,27,0,0,0).toISOString(),end_time:new Date(2026,10,27,15,10,0).toISOString()})
      expect(first?.time_range).toBeUndefined()
      expect(panel.props('timeRange')).toBe('today')
      expect(panel.props('customStartTime')).toBeNull()
      expect(panel.props('refreshToken')).toBe(0)
      header.vm.$emit('update:time-range','3h');await flushPromises()
      expect(opsAPI.getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({time_range:'3h'}),expect.anything())
      expect(panel.props('refreshToken')).toBe(0)
    } finally {wrapper.unmount();vi.useRealTimers()}
  })
  it('does not reload charts or reset query mode when only list route filters change', async () => {
    const wrapper = mountDashboard(); await flushPromises()
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    route.query = { errors_scope: 'upstream', errors_finance: 'only' }; await flushPromises()
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent({ name: 'OpsDashboardHeader' }).props('queryMode')).toBe('auto')
    route.query = { ...route.query, platform: 'openai', group_id: '12' }; await flushPromises()
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ platform: 'openai', group_id: 12 }), expect.anything())
    wrapper.unmount()
  })
  it('manual refresh advances the list token once, while chart filter refreshes leave it alone', async () => {
    const wrapper = mountDashboard(); await flushPromises()
    const panel = wrapper.findComponent({ name: 'OpsErrorRecordsPanel' })
    const accountPanel = wrapper.findComponent({ name: 'AccountPerformancePanel' })
    expect(panel.props('refreshToken')).toBe(0)
    expect(accountPanel.props('refreshToken')).toBe(0)
    wrapper.findComponent({ name: 'OpsDashboardHeader' }).vm.$emit('refresh'); await flushPromises()
    expect(panel.props('refreshToken')).toBe(1)
    expect(accountPanel.props('refreshToken')).toBe(1)
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
    route.query = { tr: 'custom', start_time: '2026-11-16T01:00:00Z', end_time: '2026-11-16T02:00:00Z' }; await flushPromises()
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    expect(panel.props('refreshToken')).toBe(1)
    expect(panel.props('customStartTime')).toBe('2026-11-16T01:00:00Z')
    expect(opsAPI.getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ start_time: '2026-11-16T01:00:00Z', end_time: '2026-11-16T02:00:00Z' }), expect.anything())
    wrapper.unmount()
  })
})

it('translates legacy alert links and chart details into the same inline scope', async () => {
  route.query = { open_error_details: '1', error_type: 'upstream', alert_rule_id: '17', platform: 'openai', group_id: '12', errors_owner: 'provider' }
  const wrapper = mountDashboard(); await flushPromises()
  expect(route.query).toEqual(expect.objectContaining({ errors_scope: 'upstream', errors_view: 'errors', errors_focus: '1', alert_rule_id: '17', platform: 'openai', group_id: '12', errors_owner: 'provider' }))
  expect(route.query.open_error_details).toBeUndefined()
  expect(route.query.error_type).toBeUndefined()
  wrapper.findComponent({ name: 'OpsErrorTrendChart' }).vm.$emit('open-request-errors'); await flushPromises()
  expect(route.query.errors_scope).toBe('request')
  expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
  expect(wrapper.findComponent({ name: 'OpsErrorDetailsModal' }).exists()).toBe(false)
  wrapper.unmount()
})

it('restores the normal one-hour entry when a reused custom route is cleared', async () => {
  route.query = { tr: 'custom', start_time: '2026-11-16T01:00:00Z', end_time: '2026-11-16T02:00:00Z', errors_scope: 'request' }
  const wrapper = mountDashboard(); await flushPromises()
  route.query = {}; await flushPromises()
  expect(wrapper.findComponent({ name: 'OpsErrorRecordsPanel' }).props('timeRange')).toBe('1h')
  expect(wrapper.findComponent({ name: 'OpsDashboardHeader' }).props('timeRange')).toBe('1h')
  wrapper.unmount()
})

it('canonicalizes a reselected group even if the legacy parsed value is unchanged', async () => {
  route.query = { group_id: '12junk' }
  const wrapper = mountDashboard(); await flushPromises()
  wrapper.findComponent({ name: 'OpsDashboardHeader' }).vm.$emit('update:group', 12)
  await new Promise(resolve => setTimeout(resolve, 300)); await flushPromises()
  expect(route.query.group_id).toBe('12')
  wrapper.unmount()
})

it('preserves an ungrouped legacy alert link without an invalid empty group', async () => {
  route.query = { group_id: '', platform: '', open_error_details: '1', error_type: 'request', alert_rule_id: '17' }
  const wrapper = mountDashboard(); await flushPromises()
  expect(route.query.group_id).toBeUndefined()
  expect(route.query).toEqual(expect.objectContaining({ errors_scope: 'request', errors_view: 'errors', errors_focus: '1', alert_rule_id: '17' }))
  wrapper.unmount()
})

it('shares the group filter and retains a missing historical group until cleared', async () => {
  route.query = { group_id: '12' }
  getGroups.mockResolvedValue([{ id: 21, name: 'OpenAI fixture', platform: 'openai' }])
  const wrapper = mountDashboard()
  await flushPromises()
  const header = wrapper.findComponent({ name: 'OpsDashboardHeader' })
  const panel = wrapper.findComponent({ name: 'OpsErrorRecordsPanel' })
  expect(getGroups).toHaveBeenCalledTimes(1)
  expect(panel.props('groupOptions')).toEqual(header.props('groupOptions'))
  expect(panel.props('groupOptions')).toContainEqual({ value: 12, label: 'admin.ops.records.groupFallback' })
  expect(panel.props('groupId')).toBe(12)
  panel.vm.$emit('update:group', 21)
  await flushPromises()
  expect(header.props('groupId')).toBe(21)
  expect(opsAPI.getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ group_id: 21 }), expect.anything())
  header.vm.$emit('update:platform', 'anthropic')
  await flushPromises()
  expect(panel.props('groupId')).toBeNull()
  expect(panel.props('groupOptions')).not.toContainEqual(expect.objectContaining({ value: 21 }))
})

it('retries unavailable group options without changing the active group', async () => {
  route.query = { group_id: '12' }
  getGroups.mockRejectedValueOnce(new Error('group fixture failure')).mockResolvedValueOnce([
    { id: 12, name: 'Recovered group', platform: 'openai' },
  ])
  const wrapper = mountDashboard()
  await flushPromises()
  const panel = wrapper.findComponent({ name: 'OpsErrorRecordsPanel' })
  expect(panel.props('groupOptionsFailed')).toBe(true)
  expect(panel.props('groupId')).toBe(12)
  panel.vm.$emit('retry-groups')
  await flushPromises()
  expect(panel.props('groupOptionsFailed')).toBe(false)
  expect(panel.props('groupOptions')).toContainEqual({ value: 12, label: 'Recovered group' })
  expect(panel.props('groupId')).toBe(12)
  expect(opsAPI.getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
})
