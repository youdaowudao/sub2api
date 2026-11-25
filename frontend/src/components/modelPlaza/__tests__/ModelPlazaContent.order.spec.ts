import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const rows = [
  { id: 2, name: 'Cheap', platform: 'anthropic', sort_order: 20, rate_multiplier: 0.1, models: [] },
  { id: 1, name: 'Admin first', platform: 'anthropic', sort_order: -10, rate_multiplier: 5, models: [] },
  { id: 3, name: 'OpenAI', platform: 'openai', sort_order: -100, rate_multiplier: 0, models: [] },
]
describe('model plaza display order', () => {
  it('keeps administrator order when base, user and peak multipliers change', async () => {
    const response = { description: '', groups: rows } as unknown as ModelPlazaResponse
    const wrapper = mount(ModelPlazaContent, { props: { response, loading: false }, global: {
      plugins: [createPinia()], stubs: {
        Icon: true, PlazaFilterBar: { props: ['groups'], template: '<div />' },
        PlazaGroupSection: { props: ['group'], template: '<div data-group>{{ group.id }}</div>' },
      },
    } })
    const ids = () => wrapper.findAll('[data-group]').map(n => Number(n.text()))
    expect(ids()).toEqual([3, 1, 2])
    await wrapper.setProps({ response: { ...response, groups: response.groups.map(g => ({ ...g, name: `new ${g.id}`, user_rate_multiplier: 100 - g.id, peak_rate_multiplier: g.id })) } })
    expect(ids()).toEqual([3, 1, 2])
    expect(rows.map(g => g.id)).toEqual([2, 1, 3])
  })

  it('按前台平台分类列出按钮、排序和筛选，国产模型保留原平台', async () => {
    const response = {
      description: '',
      groups: [
        { id: 1, name: 'Gemini 兼容接入', platform: 'openai', display_category: 'gemini', sort_order: 1, rate_multiplier: 1, models: [] },
        { id: 2, name: 'Claude', platform: 'anthropic', sort_order: 2, rate_multiplier: 1, models: [] },
        { id: 3, name: 'Kimi 接入', platform: 'openai', display_category: 'domestic', sort_order: 3, rate_multiplier: 1, models: [] }
      ]
    } as unknown as ModelPlazaResponse
    const wrapper = mount(ModelPlazaContent, {
      props: { response, loading: false },
      global: {
        plugins: [createPinia()],
        stubs: {
          Icon: true,
          PlazaFilterBar: { props: ['platforms', 'groups'], emits: ['update:platform'], template: '<div />' },
          PlazaGroupSection: { props: ['group'], template: '<div data-group>{{ group.id }}</div>' }
        }
      }
    })
    const filter = wrapper.getComponent(PlazaFilterBar)
    const ids = () => wrapper.findAll('[data-group]').map(node => Number(node.text()))

    expect.soft(filter.props('platforms')).toEqual(['openai', 'anthropic', 'gemini'])
    expect.soft(filter.props('groups').find(group => group.id === 1)?.platform).toBe('gemini')
    expect.soft(ids()).toEqual([3, 2, 1])

    filter.vm.$emit('update:platform', 'gemini')
    await nextTick()
    expect.soft(ids()).toEqual([1])

    filter.vm.$emit('update:platform', 'openai')
    await nextTick()
    expect.soft(ids()).toEqual([3])
  })
})
