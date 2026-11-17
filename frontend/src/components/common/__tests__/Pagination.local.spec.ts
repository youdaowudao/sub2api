import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/tablePreferences', () => ({ getConfiguredTablePageSizeOptions: () => [200, 500], normalizeTablePageSize: () => 200 }))
vi.mock('@/composables/usePersistedPageSize', () => ({ setPersistedPageSize: vi.fn() }))
describe('independent page size', () => {
  it('uses explicit local choices and leaves the global preference alone', () => {
    const wrapper = mount(Pagination, { props: { total: 100, page: 1, pageSize: 10, pageSizeOptions: [10, 20, 50], persistPageSize: false }, global: { stubs: { Select: true, Icon: true } } })
    const select = wrapper.findComponent({ name: 'Select' })
    expect(select.props('options').map((x: { value: number }) => x.value)).toEqual([10, 20, 50])
    select.vm.$emit('update:modelValue', 50)
    expect(wrapper.emitted('update:pageSize')).toEqual([[50]])
    expect(setPersistedPageSize).not.toHaveBeenCalled()
    select.vm.$emit('update:modelValue', 500)
    expect(wrapper.emitted('update:pageSize')).toHaveLength(1)
    wrapper.unmount()
  })
})
