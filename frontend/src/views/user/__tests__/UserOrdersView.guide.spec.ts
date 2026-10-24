import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/client', () => ({ apiClient: {}, default: {} }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { getMyOrders: vi.fn().mockResolvedValue({ data: { items: [], total: 0 } }), getRefundEligibleProviders: vi.fn().mockResolvedValue({ data: { provider_instance_ids: [] } }) } }))

describe('order table guide target', () => {
  it('forwards the stable marker through OrderTable onto the actual DataTable root, including empty orders', async () => {
    const wrapper = mount(UserOrdersView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, BaseDialog: true, Icon: true, Select: true } } })
    await flushPromises()
    expect(wrapper.findAll('[data-tour="orders-list"]')).toHaveLength(1)
    expect(wrapper.get('[data-tour="orders-list"]').classes()).toContain('table-wrapper')
    expect(wrapper.get('[data-tour="orders-list"]').findAll('tbody tr[data-row-id]')).toHaveLength(0)
    expect(wrapper.get('[data-tour="orders-list"]').text()).toContain('empty.noData')
    wrapper.unmount()
  })
})
