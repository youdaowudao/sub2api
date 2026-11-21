import { afterEach, describe, it, expect, vi, beforeEach } from 'vitest'
import { enableAutoUnmount, mount, flushPromises } from '@vue/test-utils'
enableAutoUnmount(afterEach)
import { reactive } from 'vue'
import OpsErrorRecordsPanel from '../OpsErrorRecordsPanel.vue'
import { opsAPI } from '@/api/admin/ops'

const route = reactive({ query: {} as Record<string, string>, path: '/admin/ops' })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace: vi.fn().mockResolvedValue(undefined) }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 987651 } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/admin/ops', async (importOriginal) => { const actual = await importOriginal<typeof import('@/api/admin/ops')>(); const api = { ...actual.opsAPI, listErrorRecords: vi.fn(), listErrorRecordMembers: vi.fn() }; return { ...actual, opsAPI: api, default: api } })
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, values?: unknown) => key + (values ? JSON.stringify(values) : '') }) }))
const result = { items: [], total: 0, total_records: 0, page: 1, page_size: 10, window_start: '2026-11-16T00:00:00Z', window_end: '2026-11-16T01:00:00Z' }
const mountPanel = () => mount(OpsErrorRecordsPanel, {
  props: { timeRange: '1h', platform: '', groupId: null, refreshToken: 0 },
  global: { stubs: { OpsErrorLogTable: true, OpsErrorDetailModal: true, OpsErrorIdentityFilter: true, UserBalanceHistoryModal: true, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } },
})

beforeEach(() => { vi.clearAllMocks(); route.query = {}; localStorage.clear(); vi.mocked(opsAPI.listErrorRecords).mockResolvedValue(result) })
describe('embedded error records', () => {
  it('queries once on mount and changes scope without opening a modal', async () => {
    const panel = mountPanel(); await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenCalledTimes(1)
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ scope: 'all', finance: 'hide', display: 'grouped', view: 'all' }), expect.anything())
    await panel.get('[data-testid="error-scope-upstream"]').trigger('click'); await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenCalledTimes(2)
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ scope: 'upstream' }), expect.anything())
    expect(opsAPI.listErrorRecordMembers).not.toHaveBeenCalled(); panel.unmount()
  })
  it('keeps an explicit Usage deep link request scope and owner filter', async () => {
    route.query = { errors_scope: 'request', errors_view: 'all', errors_owner: 'platform', errors_user_id: '42' }
    const panel = mountPanel(); await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ scope: 'request', view: 'all', error_owner: 'platform', user_id: 42 }), expect.anything()); panel.unmount()
  })
  it('cancels and ignores late results after criteria change', async () => {
    let resolveOld!: (value: typeof result) => void
    vi.mocked(opsAPI.listErrorRecords).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const panel = mountPanel(); await flushPromises()
    const signal = vi.mocked(opsAPI.listErrorRecords).mock.calls[0][1]?.signal
    await panel.get('[data-testid="error-scope-request"]').trigger('click'); await flushPromises()
    expect(signal?.aborted).toBe(true)
    resolveOld({ ...result, total: 999, total_records: 999 }); await flushPromises()
    expect(panel.text()).not.toContain('999'); panel.unmount()
  })
})

it('does not query malformed identities and can reset the invalid filter', async () => {
  route.query = { errors_user_id: '-2' }
  const panel = mountPanel(); await flushPromises()
  expect(opsAPI.listErrorRecords).not.toHaveBeenCalled()
  expect(panel.find('[role="alert"]').exists()).toBe(true)
  await panel.findAll('button').find(button => button.text() === 'admin.ops.records.reset')!.trigger('click'); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenCalledTimes(1); panel.unmount()
})

it('keeps explicit link finance ahead of a remembered preference', async () => {
  localStorage.setItem('admin-ops-error-records:987651:finance', JSON.stringify('only'))
  route.query = { errors_finance: 'hide', errors_scope: 'upstream' }
  const panel = mountPanel(); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ finance: 'hide', scope: 'upstream' }), expect.anything()); panel.unmount()
})

it('keeps the returned absolute time window on paging and cancels on unmount', async () => {
  const panel = mountPanel(); await flushPromises()
  panel.findComponent({ name: 'OpsErrorLogTable' }).vm.$emit('update:page', 2); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, start_time: result.window_start, end_time: result.window_end }), expect.anything())
  const signal = vi.mocked(opsAPI.listErrorRecords).mock.calls.at(-1)?.[1]?.signal
  panel.unmount(); expect(signal?.aborted).toBe(true)
})

it('preserves group page when returning from a single detail without an eager requery', async () => {
  const sample = { id: 5, created_at: result.window_start, message: 'timeout', phase: 'upstream', type: 'upstream_error', error_owner: 'provider', error_source: 'upstream_http', severity: 'P1', status_code: 503, platform: 'openai', model: 'model-a', resolved: false, client_request_id: '', request_id: '', user_email: '', account_name: '', group_name: '' }
  const record = { representative: sample, representative_id: 5, client_status_code: null, is_request_error: true, is_upstream_error: true, count: 12, first_at: result.window_start, last_at: result.window_end, user_count: 2, api_key_count: 2, has_unknown_user: false, has_unknown_api_key: false, single_client_ip: null, client_ip_count: 0, has_unknown_client_ip: true, single_user_agent: null, user_agent_count: 0, has_unknown_user_agent: true }
  vi.mocked(opsAPI.listErrorRecords).mockResolvedValueOnce({ ...result, items: [record], total: 1, total_records: 12 })
  vi.mocked(opsAPI.listErrorRecordMembers).mockImplementation(async (_id, query) => ({ ...result, page: query.page || 1, items: [{ ...record, ...sample, representative: undefined, id: 110, representative_id: 110, count: 1, user_count: 1, api_key_count: 1 }], total: 12, total_records: 12 }))
  const panel = mountPanel(); await flushPromises()
  panel.findComponent({ name: 'OpsErrorLogTable' }).vm.$emit('openErrorDetail', 5); await flushPromises()
  expect(opsAPI.listErrorRecordMembers).toHaveBeenCalledTimes(1)
  panel.findAllComponents({ name: 'OpsErrorLogTable' })[1].vm.$emit('update:page', 2); await flushPromises()
  panel.findAllComponents({ name: 'OpsErrorLogTable' })[1].vm.$emit('openErrorDetail', 110); await flushPromises()
  expect(panel.findComponent({ name: 'OpsErrorDetailModal' }).props('errorType')).toBe('request')
  panel.findComponent({ name: 'OpsErrorDetailModal' }).vm.$emit('back'); await flushPromises()
  expect(panel.findAllComponents({ name: 'OpsErrorLogTable' })[1].props('page')).toBe(2)
  expect(opsAPI.listErrorRecordMembers).toHaveBeenCalledTimes(2); panel.unmount()
})

it.each(['-2', '12junk', '0', '9007199254740992', ''])('does not query an invalid global group %s', async value => {
  route.query = { group_id: value }
  const panel = mountPanel(); await flushPromises()
  expect(opsAPI.listErrorRecords).not.toHaveBeenCalled()
  expect(panel.find('[role="alert"]').exists()).toBe(true)
  panel.unmount()
})

it('does not label first-page rows as page two when paging fails', async () => {
  vi.mocked(opsAPI.listErrorRecords).mockResolvedValueOnce({ ...result, total: 20, total_records: 20, items: [{ id: 81, representative_id: 81, message: 'first page witness', count: 1 }] } as never)
  const panel = mountPanel(); await flushPromises()
  expect(panel.findComponent({ name: 'OpsErrorLogTable' }).props('rows')).toHaveLength(1)
  vi.mocked(opsAPI.listErrorRecords).mockRejectedValueOnce(new Error('page two failed'))
  panel.findComponent({ name: 'OpsErrorLogTable' }).vm.$emit('update:page', 2); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }), expect.anything())
  expect(panel.findComponent({ name: 'OpsErrorLogTable' }).exists()).toBe(false)
  expect(panel.get('[role="alert"]').text()).not.toContain('admin.ops.records.refreshFailed')
  panel.unmount()
})

it('shows the common identity filters before expanding the advanced filters', async () => {
  const panel = mountPanel()
  await panel.setProps({ groupId: 31 })
  await flushPromises()
  const filters = panel.findAllComponents({ name: 'OpsErrorIdentityFilter' })
  expect(panel.find('[data-testid="error-more-filters"]').exists()).toBe(false)
  expect(filters.map(filter => filter.props('kind'))).toEqual(['user', 'key', 'account'])
  for (const [index, id] of [21, 22, 23].entries()) {
    filters[index].vm.$emit('update:id', id)
    await flushPromises()
  }
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(
    expect.objectContaining({ user_id: 21, api_key_id: 22, account_id: 23 }), expect.anything()
  )
  filters[0].vm.$emit('update:id', 24)
  await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(
    expect.objectContaining({ user_id: 24, account_id: 23 }), expect.anything()
  )
  expect(vi.mocked(opsAPI.listErrorRecords).mock.calls.at(-1)?.[0].api_key_id).toBeUndefined()
  filters[2].vm.$emit('update:id', undefined)
  await flushPromises()
  expect(vi.mocked(opsAPI.listErrorRecords).mock.calls.at(-1)?.[0]).toMatchObject({ user_id: 24, group_id: 31 })
  expect(vi.mocked(opsAPI.listErrorRecords).mock.calls.at(-1)?.[0].account_id).toBeUndefined()
  await panel.findAll('button').find(button => button.text() === 'admin.ops.records.reset')!.trigger('click')
  await flushPromises()
  const resetQuery = vi.mocked(opsAPI.listErrorRecords).mock.calls.at(-1)?.[0]
  expect(resetQuery?.group_id).toBe(31)
  expect(resetQuery?.user_id).toBeUndefined()

})
