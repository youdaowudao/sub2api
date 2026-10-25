import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupSelector from '../GroupSelector.vue'

const authState = { isSimpleMode: false }

vi.mock('@/stores', () => ({ useAuthStore: () => authState }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'Basic', platform: 'anthropic', status: 'active' },
  { id: 2, name: 'Composite', platform: 'composite', status: 'active' }
] as any

const mountSelector = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups },
  global: { stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true } }
})

describe('GroupSelector simple-mode binding policy', () => {
  beforeEach(() => { authState.isSimpleMode = false })

  it('hides composite groups in simple mode and preserves basic groups', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Basic')
    expect(wrapper.text()).not.toContain('Composite')
  })

  it('keeps composite groups available in advanced mode', () => {
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Composite')
  })

  it('cleans hidden historical composite IDs while preserving visible selections', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector([1, 2])
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
  })
})

describe('GroupSelector rendering and business IDs', () => {
  it('sorts the render copy and preserves click order in the submitted ID array', async () => {
    authState.isSimpleMode = false
    const rows = [
      { id: 3, name: 'OpenAI', platform: 'openai', sort_order: -10 },
      { id: 2, name: 'Anthropic later', platform: 'anthropic', sort_order: 20 },
      { id: 1, name: 'Anthropic first', platform: 'anthropic', sort_order: 0 },
    ] as any
    const selected = [3, 2]
    const wrapper = mount(GroupSelector, { props: { groups: rows, modelValue: selected }, global: {
      stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true },
    } })
    expect(wrapper.findAll('input[type="checkbox"]').map(n => Number(n.element.value))).toEqual([1, 2, 3])
    await wrapper.get('input[value="1"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[3, 2, 1]]])
    expect(rows.map((g: any) => g.id)).toEqual([3, 2, 1])
    expect(selected).toEqual([3, 2])
  })
})
