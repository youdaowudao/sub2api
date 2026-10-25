import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountGroupsCell from '../AccountGroupsCell.vue'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (k: string) => k }) }))
describe('account group display copies', () => {
  it('uses the same order for the summary and expanded list without changing account bindings', async () => {
    const groups = [
      { id: 3, name: 'OpenAI', platform: 'openai', sort_order: -100 },
      { id: 2, name: 'Later', platform: 'anthropic', sort_order: 10 },
      { id: 1, name: 'First', platform: 'anthropic', sort_order: 0 },
    ] as any
    const w = mount(AccountGroupsCell, { props: { groups, maxDisplay: 2 }, global: { stubs: { Teleport: true, GroupBadge: { props: ['name'], template: '<span data-badge>{{ name }}</span>' } } } })
    expect(w.findAll('[data-badge]').map(n => n.text())).toEqual(['First'])
    await w.get('button').trigger('click')
    expect(w.findAll('[data-badge]').slice(1).map(n => n.text())).toEqual(['First', 'Later', 'OpenAI'])
    expect(groups.map((g: any) => g.id)).toEqual([3, 2, 1])
    w.unmount()
  })
})
