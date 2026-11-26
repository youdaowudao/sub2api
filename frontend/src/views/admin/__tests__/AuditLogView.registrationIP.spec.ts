import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AuditLogView from '../AuditLogView.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import IpGeoBatchToolbar from '@/components/common/IpGeoBatchToolbar.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), fetchOne: vi.fn(), fetchBatch: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { audit: { list: mocks.list, get: mocks.get } } }))
vi.mock('@/api', () => ({ totpAPI: {} }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: mocks.showError }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/ipGeoLookup', () => ({
  getEntry: () => ({ status: 'idle' }), fetchOne: mocks.fetchOne, fetchBatch: mocks.fetchBatch
}))

const row = { id: 1, client_ip: '192.0.2.1', actor_email: '', path: '/api/v1/auth/register', action: 'auth.register', method: 'POST', status_code: 200, created_at: '2025-01-01T00:00:00Z' }
const layout = defineComponent({ template: '<div><slot /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' })
const table = defineComponent({
  props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-client_ip" :value="row.client_ip" /><slot name="cell-actions" :row="row" /></div></div>'
})
beforeEach(() => {
  vi.clearAllMocks()
  mocks.list.mockResolvedValue({ items: [row, { ...row, id: 2, client_ip: '' }, { ...row, id: 3 }], total: 3 })
  mocks.get.mockResolvedValue(row)
  mocks.fetchBatch.mockResolvedValue(false)
})
describe('AuditLog registration IP', () => {
  it('keeps email filter and IP, and provides manual list, batch and detail lookups', async () => {
    const wrapper = mount(AuditLogView, {
      global: { stubs: { AppLayout: layout, TablePageLayout: layout, DataTable: table, Select: true, Pagination: true, Icon: true, ConfirmDialog: true, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } }
    })
    await flushPromises()
    expect(wrapper.findAllComponents(IpGeoCell)).toHaveLength(2)
    expect(mocks.fetchOne).not.toHaveBeenCalled()
    expect(mocks.fetchBatch).not.toHaveBeenCalled()
    await wrapper.get('input[placeholder="admin.audit.filters.actorEmailPlaceholder"]').setValue('fixture@example.com')
    await wrapper.get('input[placeholder="admin.audit.filters.actorEmailPlaceholder"]').trigger('keyup.enter')
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(expect.objectContaining({ actor_email: 'fixture@example.com' }))
    await wrapper.findAllComponents(IpGeoCell)[0].get('button').trigger('click')
    expect(mocks.fetchOne).toHaveBeenCalledWith(row.client_ip)
    await wrapper.getComponent(IpGeoBatchToolbar).get('button').trigger('click')
    await flushPromises()
    expect(mocks.fetchBatch).toHaveBeenCalledTimes(1)
    expect(mocks.fetchBatch).toHaveBeenCalledWith([row.client_ip])
    expect(mocks.showError).toHaveBeenCalledWith('usage.ipGeo.batchFailed')
    const detail = wrapper.findAll('button').find(button => button.text().includes('admin.audit.columns.detail'))
    expect(detail).toBeDefined()
    await detail!.trigger('click')
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(row.id)
    expect(wrapper.findAllComponents(IpGeoCell)).toHaveLength(3)
    wrapper.unmount()
  })
})
