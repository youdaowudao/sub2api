import { createPinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UsersView from '../UsersView.vue'
const { getAll } = vi.hoisted(() => ({ getAll: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: { getAll, getAllIncludingInactive: getAll },
  users: { list: vi.fn().mockResolvedValue({ items: [], total: 0, pages: 0 }) },
  userAttributes: { listEnabledDefinitions: vi.fn().mockResolvedValue([]) },
} }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (k: string) => k }) }))
describe('UsersView group column and replacement menu', () => {
  it('keeps exclusive/public partitions ordered and replaces the clicked original group', async () => {
    localStorage.clear()
    const rows = [
      { id: 3, name: 'OpenAI exclusive', platform: 'openai', status: 'active', subscription_type: 'standard', is_exclusive: true, sort_order: -100 },
      { id: 2, name: 'Later exclusive', platform: 'anthropic', status: 'active', subscription_type: 'standard', is_exclusive: true, sort_order: 10 },
      { id: 1, name: 'First exclusive', platform: 'anthropic', status: 'active', subscription_type: 'standard', is_exclusive: true, sort_order: 0 },
      { id: 4, name: 'Public', platform: 'anthropic', status: 'active', subscription_type: 'standard', is_exclusive: false, sort_order: -100 },
    ]
    getAll.mockResolvedValue(rows)
    const w = mount(UsersView, { global: {
      plugins: [createPinia()], stubs: {
        AppLayout: { template: '<div><slot /></div>' }, TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: { template: '<div><slot name="cell-groups" :row="{ id: 42, allowed_groups: [3, 2, 1] }" /></div>' },
        Select: true, Pagination: true, Icon: true, UserCreateModal: true, UserEditModal: true, BulkEditUserModal: true, UserPlatformQuotaModal: true, UserApiKeysModal: true, UserAllowedGroupsModal: true, UserBalanceModal: true, UserBalanceHistoryModal: true, UserAttributesConfigModal: true, GroupReplaceModal: true,
      },
    } })
    await (w.vm as any).loadAllGroups(); await flushPromises()
    const exclusive = w.get('[class~="group/ex"]')
    expect(exclusive.findAll('div.flex-col > span').map(n => n.text())).toEqual(['First exclusive', 'Later exclusive', 'OpenAI exclusive'])
    expect(w.get('[class~="group/pub"]').text()).toContain('Public')
    await exclusive.trigger('click')
    const item = exclusive.findAll('div.cursor-pointer').find(n => n.text() === 'Later exclusive')!
    await item.trigger('click')
    const replacement = w.findComponent({ name: 'GroupReplaceModal' })
    expect(replacement.props('oldGroup').id).toBe(2)
    expect(replacement.props('user').id).toBe(42)
    expect(rows.map(g => g.id)).toEqual([3, 2, 1, 4])
    w.unmount(); localStorage.clear()
  })
})
