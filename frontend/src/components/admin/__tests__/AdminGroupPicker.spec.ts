import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import AdminGroupPicker from '../AdminGroupPicker.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

const groups = [
  { id: 11, platform: 'openai', sort_order: 1 },
  { id: 22, platform: 'anthropic', sort_order: 1 },
  { id: 33, platform: 'gemini', sort_order: 1 },
]
const options = [
  { value: 11, label: 'GPT Group', platform: 'openai', description: 'Standard access', rate: 2 },
  { value: 22, label: 'Claude Group', platform: 'anthropic', description: 'Premium access' },
  { value: 33, label: 'Gemini Group', platform: 'gemini', description: 'Standard access' },
]
const specialOptions = [
  { value: null, label: 'All groups' },
  { value: 'ungrouped', label: 'Ungrouped' },
]
const mountPicker = (modelValue: number | string | null = 22) => mount(AdminGroupPicker, {
  props: { modelValue, groups, options, specialOptions },
  global: { plugins: [createPinia()], stubs: { Teleport: true } },
})
const category = (wrapper: ReturnType<typeof mountPicker>, name: string) =>
  wrapper.findAll('button[aria-pressed]').find(button => button.text().includes('keys.providers.' + name))!
const groupNames = (wrapper: ReturnType<typeof mountPicker>) =>
  wrapper.findAllComponents(GroupOptionItem).map(item => item.props('name'))

describe('AdminGroupPicker', () => {
  it('renders the current GroupBadge and opens on its category', async () => {
    const wrapper = mountPicker()
    expect(wrapper.findComponent(GroupBadge).props('name')).toBe('Claude Group')
    expect(wrapper.text()).not.toContain('keys.selectGroup')
    await wrapper.get('button').trigger('click')
    expect(category(wrapper, 'anthropic').attributes('aria-pressed')).toBe('true')
    expect(groupNames(wrapper)).toEqual(['Claude Group'])
  })

  it('switches categories by click and after a 150ms pointer hover', async () => {
    vi.useFakeTimers()
    const wrapper = mountPicker(null)
    expect(wrapper.get('button').text()).toContain('All groups')
    await wrapper.get('button').trigger('click')
    expect(category(wrapper, 'openai').attributes('aria-pressed')).toBe('true')
    await category(wrapper, 'gemini').trigger('click')
    expect(groupNames(wrapper)).toEqual(['Gemini Group'])
    await category(wrapper, 'anthropic').trigger('pointerenter', { pointerType: 'mouse' })
    vi.advanceTimersByTime(149)
    await nextTick()
    expect(groupNames(wrapper)).toEqual(['Gemini Group'])
    vi.advanceTimersByTime(1)
    await nextTick()
    expect(groupNames(wrapper)).toEqual(['Claude Group'])
  })

  it('searches labels and descriptions across categories and freezes category changes', async () => {
    const wrapper = mountPicker()
    await wrapper.get('button').trigger('click')
    const search = wrapper.get('input')
    await search.setValue('STANDARD')
    expect(groupNames(wrapper)).toEqual(['GPT Group', 'Gemini Group'])
    expect(category(wrapper, 'openai').attributes('disabled')).toBeDefined()
    await search.setValue('claude')
    expect(groupNames(wrapper)).toEqual(['Claude Group'])
    await search.setValue('missing')
    expect(groupNames(wrapper)).toEqual([])
    expect(wrapper.text()).toContain('keys.noGroupFound')
    expect(wrapper.text()).toContain('All groups')
    expect(wrapper.text()).toContain('Ungrouped')
  })

  it('places special options first in every category and emits Select-compatible events', async () => {
    const wrapper = mountPicker()
    await wrapper.get('button').trigger('click')
    for (const name of ['openai', 'anthropic', 'gemini', 'image']) {
      await category(wrapper, name).trigger('click')
      const buttons = wrapper.findAll('button[title]').slice(1)
      expect(buttons.slice(0, 2).map(button => button.text())).toEqual(['All groups', 'Ungrouped'])
    }
    await category(wrapper, 'openai').trigger('click')
    await wrapper.get('button[title="Standard access"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[11]])
    expect(wrapper.emitted('change')).toEqual([[11, options[0]]])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('button').trigger('click')
    await wrapper.get('button[title="Ungrouped"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['ungrouped'])
  })

  it('keeps account group values as strings, including the all-groups value', async () => {
    const wrapper = mount(AdminGroupPicker, {
      props: {
        modelValue: '22', groups,
        options: options.map(option => ({ ...option, value: String(option.value) })),
        specialOptions: [{ value: '', label: 'All groups' }, { value: 'ungrouped', label: 'Ungrouped' }],
      },
      global: { plugins: [createPinia()], stubs: { Teleport: true } },
    })
    expect(wrapper.findComponent(GroupBadge).props('name')).toBe('Claude Group')
    await wrapper.get('button').trigger('click')
    expect(wrapper.find('button[aria-pressed="true"]').text()).toContain('keys.providers.anthropic')
    await wrapper.get('button[title="All groups"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['']])
  })

  it('closes on Escape, outside click and scroll without changing selection', async () => {
    const wrapper = mountPicker()
    await wrapper.get('button').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('button').trigger('click')
    document.body.click()
    await nextTick()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('button').trigger('click')
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('change')).toBeUndefined()
  })

  it('uses the same boxed trigger as neighbouring filter fields', () => {
    const trigger = mountPicker().get('button')
    expect(trigger.classes()).toEqual(expect.arrayContaining(['input', 'h-[42px]']))
    expect(trigger.classes()).not.toContain('-mx-2')
  })

  it('shows the placeholder when nothing is selected and marks errors', () => {
    const wrapper = mount(AdminGroupPicker, {
      props: { modelValue: null, groups, options, specialOptions: [], placeholder: 'Pick a group', error: true },
      global: { plugins: [createPinia()], stubs: { Teleport: true } },
    })
    const trigger = wrapper.get('button')
    expect(trigger.text()).toContain('Pick a group')
    expect(trigger.classes()).toContain('input-error')
    expect(trigger.attributes('aria-invalid')).toBe('true')
  })
})
