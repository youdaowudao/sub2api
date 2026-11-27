import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia } from 'pinia'
import OpsErrorRecordsPanel from '../OpsErrorRecordsPanel.vue'
import OpsErrorIdentityFilter from '../OpsErrorIdentityFilter.vue'
import OpsErrorLogTable from '../OpsErrorLogTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import { opsAPI, type OpsErrorRecord, type OpsErrorLog } from '@/api/admin/ops'
import { adminAPI } from '@/api/admin'

enableAutoUnmount(afterEach)
const route = reactive({ query: {} as Record<string, string>, path: '/admin/ops' })
const auth = reactive({ user: { id: 987651 }, isAdmin: true })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace: vi.fn(async (target: { query: Record<string, string> }) => { route.query = target.query }) }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/admin/ops', async original => { const actual = await original<typeof import('@/api/admin/ops')>(); return { ...actual, opsAPI: { ...actual.opsAPI, listErrorRecords: vi.fn(), listErrorRecordMembers: vi.fn() } } })
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const sample = { id: 5, created_at: '2026-11-16T00:00:00Z', message: 'unavailable', phase: 'upstream', type: 'upstream_error', error_owner: 'provider', error_source: 'upstream_http', severity: 'P1', status_code: 503, platform: 'openai', model: 'model-a', resolved: false, client_request_id: '', request_id: '', user_id: 999, user_email: 'representative@example.test', account_name: '', group_name: '' } as OpsErrorLog
const record = { representative: sample, representative_id: 5, count: 20, first_at: sample.created_at, last_at: sample.created_at, is_request_error: true, is_upstream_error: true, user_count: 1, api_key_count: 0, single_user: { id: 42, email: 'unique@example.test' }, has_unknown_user: false, has_unknown_api_key: true, client_ip_count: 0, has_unknown_client_ip: true, user_agent_count: 0, has_unknown_user_agent: true } as OpsErrorRecord
const result = { items: [record], total: 20, total_records: 20, page: 1, page_size: 10, window_start: '2026-11-16T00:00:00Z', window_end: '2026-11-16T01:00:00Z' }
const memberResult = { ...result, items: [{ ...record, ...sample, representative: undefined, id: 110, representative_id: 110, count: 1 }] }
const mountPanel = () => mount(OpsErrorRecordsPanel, { props: { timeRange: '1h', platform: '', groupId: null, refreshToken: 0 }, global: { stubs: {
  IpGeoBatchToolbar: true, OpsErrorDetailModal: true, OpsErrorIdentityFilter: true,
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
  UserBalanceHistoryModal: { name: 'UserBalanceHistoryModal', props: { show: Boolean, user: Object, hideActions: Boolean }, template: '<div v-if="show" data-testid="balance-modal">{{ user.id }}</div>' }
} } })
const table = (wrapper: ReturnType<typeof mountPanel>, index = 0) => wrapper.findAllComponents(OpsErrorLogTable)[index]
const header = (wrapper: ReturnType<typeof mountPanel>, key: string, index = 0) => table(wrapper, index).findAll('thead th').find(th => th.text() === `admin.ops.errorLog.${key === 'created_at' ? 'time' : key}`)!
async function column(wrapper: ReturnType<typeof mountPanel>, key: string, show: boolean) {
  await wrapper.findAll('label').find(label => label.text() === `admin.ops.errorLog.${key}`)!.get('input').setValue(show)
  await flushPromises()
}
async function retry(wrapper: ReturnType<typeof mountPanel>) {
  await wrapper.findAll('button').find(button => button.text() === 'admin.ops.records.retry')!.trigger('click'); await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks(); route.query = {}; auth.user.id = 987651; localStorage.clear()
  vi.mocked(opsAPI.listErrorRecords).mockResolvedValue(result)
  vi.mocked(opsAPI.listErrorRecordMembers).mockResolvedValue(memberResult)
})
afterEach(() => vi.restoreAllMocks())

it('reads actual today and 3h error windows and keeps the returned window for pagination', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026,10,27,15,10,0))
  const wrapper=mountPanel()
  try {
    await flushPromises()
    vi.mocked(opsAPI.listErrorRecords).mockClear()
    const today={start_time:new Date(2026,10,27,0,0,0).toISOString(),end_time:new Date(2026,10,27,15,10,0).toISOString()}
    vi.mocked(opsAPI.listErrorRecords).mockResolvedValue({...result,window_start:today.start_time,window_end:today.end_time})
    await wrapper.setProps({timeRange:'today'});await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenCalledTimes(1)
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining(today),expect.anything())
    expect(vi.mocked(opsAPI.listErrorRecords).mock.lastCall?.[0].time_range).toBeUndefined()
    vi.setSystemTime(new Date(2026,10,27,15,20,0))
    table(wrapper).findComponent(Pagination).vm.$emit('update:page',2);await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({...today,page:2}),expect.anything())
    await wrapper.setProps({refreshToken:1});await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({start_time:today.start_time,end_time:new Date(2026,10,27,15,20,0).toISOString()}),expect.anything())
    await wrapper.setProps({timeRange:'3h'});await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({time_range:'3h'}),expect.anything())
  } finally {wrapper.unmount();vi.useRealTimers()}
})

it('selects the shared browser user history in the real ops identity filter without user lookup', async () => {
  localStorage.setItem('admin-user-query-history:987651',JSON.stringify([{id:42,email:'saved@example.test',lastQueriedAt:1,pinned:true}]))
  const lookup=vi.spyOn(adminAPI.usage,'searchUsers').mockResolvedValue([])
  const w=mount(OpsErrorIdentityFilter,{props:{kind:'user',label:'User'},global:{plugins:[createPinia()]}})
  try {
    await w.get('input').trigger('focus')
    await w.findAll('button').find(b=>b.text().includes('saved@example.test'))!.trigger('click')
    expect(w.emitted('update:id')).toEqual([[42]])
    expect(lookup).not.toHaveBeenCalled()
  } finally {w.unmount();localStorage.removeItem('admin-user-query-history:987651')}
})

describe('sorting stays consistent with requests', () => {
  it.each(['model', 'status'] as const)('restores main %s sorting after a failed page and retry', async key => {
    const wrapper = mountPanel(); await flushPromises()
    if (key === 'status') await column(wrapper, key, true)
    await header(wrapper, key).trigger('click'); await flushPromises()
    vi.mocked(opsAPI.listErrorRecords).mockRejectedValueOnce(new Error('page failed'))
    table(wrapper).findComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
    expect(wrapper.findAllComponents(OpsErrorLogTable)).toHaveLength(0)
    await retry(wrapper)
    expect(header(wrapper, key).attributes('aria-sort')).toBe('ascending')
    const calls = vi.mocked(opsAPI.listErrorRecords).mock.calls.length
    await header(wrapper, key).trigger('click'); await flushPromises()
    expect(opsAPI.listErrorRecords).toHaveBeenCalledTimes(calls + 1)
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: key === 'status' ? 'status_code' : key, sort_order: 'desc' }), expect.anything())
  })
  it.each(['model', 'status'] as const)('restores member %s sorting after a failed page and retry', async key => {
    const wrapper = mountPanel(); await flushPromises()
    table(wrapper).vm.$emit('openErrorDetail', 5); await flushPromises()
    await header(wrapper, key, 1).trigger('click'); await flushPromises()
    vi.mocked(opsAPI.listErrorRecordMembers).mockRejectedValueOnce(new Error('member page failed'))
    table(wrapper, 1).findComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
    expect(wrapper.findAllComponents(OpsErrorLogTable)).toHaveLength(1)
    await retry(wrapper)
    expect(header(wrapper, key, 1).attributes('aria-sort')).toBe('ascending')
    const calls = vi.mocked(opsAPI.listErrorRecordMembers).mock.calls.length
    await header(wrapper, key, 1).trigger('click'); await flushPromises()
    expect(opsAPI.listErrorRecordMembers).toHaveBeenCalledTimes(calls + 1)
    expect(opsAPI.listErrorRecordMembers).toHaveBeenLastCalledWith(5, expect.objectContaining({ sort_by: key === 'status' ? 'status_code' : key, sort_order: 'desc' }), expect.anything())
  })
  it.each(['model', 'status'] as const)('resets both the query and indicator when sorted %s is hidden', async key => {
    const wrapper = mountPanel(); await flushPromises()
    if (key === 'status') await column(wrapper, key, true)
    await header(wrapper, key).trigger('click'); await flushPromises()
    await column(wrapper, key, false)
    expect(header(wrapper, 'created_at').attributes('aria-sort')).toBe('descending')
    expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'created_at', sort_order: 'desc' }), expect.anything())
  })
})

it('keeps loading and in-memory page changes usable when storage reads and writes throw', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('storage blocked') })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage blocked') })
  const wrapper = mountPanel(); await flushPromises()
  expect(table(wrapper).findAll('tbody tr')).toHaveLength(1)
  table(wrapper).findComponent(Pagination).findComponent(Select).vm.$emit('update:modelValue', 20); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ page_size: 20 }), expect.anything())
  expect(table(wrapper).findComponent(Pagination).props('pageSize')).toBe(20)
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
})

it('isolates administrator preferences and the member width key across remounts', async () => {
  localStorage.setItem('admin-ops-error-records:987651:column-widths', JSON.stringify({ message: 640 }))
  localStorage.setItem('admin-ops-error-records:987652:column-widths', JSON.stringify({ message: 360 }))
  localStorage.setItem('admin-ops-error-records:987652:members-column-widths', JSON.stringify({ message: 500 }))
  let wrapper = mountPanel(); await flushPromises()
  expect(header(wrapper, 'message').attributes('style')).toContain('width: 640px')
  const finance = wrapper.findAllComponents(Select).find(select => select.props('options').some((option: { value: unknown }) => option.value === 'only'))!
  finance.vm.$emit('update:modelValue', 'only'); finance.vm.$emit('change', 'only'); await flushPromises()
  table(wrapper).findComponent(Pagination).findComponent(Select).vm.$emit('update:modelValue', 20); await flushPromises()
  expect(localStorage.getItem('admin-ops-error-records:987651:finance')).toBe('"only"')
  wrapper.unmount(); route.query = {}; auth.user.id = 987652
  wrapper = mountPanel(); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ finance: 'hide', page_size: 10 }), expect.anything())
  expect(table(wrapper).props('columnWidthStorageKey')).toBe('admin-ops-error-records:987652:column-widths')
  expect(header(wrapper, 'message').attributes('style')).toContain('width: 360px')
  table(wrapper).vm.$emit('openErrorDetail', 5); await flushPromises()
  expect(table(wrapper, 1).props('columnWidthStorageKey')).toBe('admin-ops-error-records:987652:members-column-widths')
  expect(header(wrapper, 'message', 1).attributes('style')).toContain('width: 500px')
  wrapper.unmount(); route.query = {}; auth.user.id = 987651
  wrapper = mountPanel(); await flushPromises()
  expect(opsAPI.listErrorRecords).toHaveBeenLastCalledWith(expect.objectContaining({ finance: 'only', page_size: 20 }), expect.anything())
  expect(header(wrapper, 'message').attributes('style')).toContain('width: 640px')
})

it('opens the unique actual user balance history with actions hidden', async () => {
  const getUser = vi.spyOn(adminAPI.users, 'getById').mockResolvedValue({ id: 42, email: 'unique@example.test' } as never)
  const wrapper = mountPanel(); await flushPromises()
  await table(wrapper).findAll('button').find(button => button.text() === 'unique@example.test')!.trigger('click'); await flushPromises()
  expect(getUser).toHaveBeenCalledTimes(1)
  expect(getUser).toHaveBeenCalledWith(42, true)
  const modal = wrapper.findComponent({ name: 'UserBalanceHistoryModal' })
  expect(modal.props('show')).toBe(true)
  expect(modal.props('user')).toEqual(expect.objectContaining({ id: 42 }))
  expect(modal.props('hideActions')).toBe(true)
  expect(opsAPI.listErrorRecordMembers).not.toHaveBeenCalled()
})
