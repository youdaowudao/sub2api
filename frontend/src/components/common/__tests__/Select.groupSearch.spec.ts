import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import Select from '../Select.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: ReturnType<typeof mount>
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
const options = [
  { value: '', label: 'All groups' },
  { value: '__anthropic', label: 'Anthropic', kind: 'group', disabled: true },
  { value: 1, label: 'First', description: 'target description' },
  { value: '__openai', label: 'OpenAI', kind: 'group' },
  { value: 2, label: 'Second' },
]
async function open(extra = {}) {
  wrapper = mount(Select, { attachTo: document.body, props: { modelValue: 2, options, searchable: true, ...extra } })
  await wrapper.get('button').trigger('click')
  await nextTick()
}
async function search(query: string) {
  const input = document.querySelector<HTMLInputElement>('.select-search-input')!
  input.value = query
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
  return input
}
describe('Select platform headings', () => {
  it('retains only headings with matching children and selects the filtered keyboard index', async () => {
    await open()
    const input = await search('target')
    const text = document.querySelector('[role="listbox"]')!.textContent
    expect(text).toContain('Anthropic'); expect(text).toContain('First')
    expect(text).not.toContain('OpenAI'); expect(text).not.toContain('Second')
    document.querySelector<HTMLElement>('.select-option-group')!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[1]])
  })
  it('does not show a heading-only match or select a heading passed as current value', async () => {
    await open({ modelValue: '__openai' })
    expect(wrapper.get('button').text()).not.toContain('OpenAI')
    await search('Anthropic')
    expect(document.querySelectorAll('.select-option-group')).toHaveLength(0)
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(0)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('retains remote results and prepends creatable results in local search', async () => {
    await open({ remote: true })
    await search('unmatched')
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(5)
    wrapper.unmount()
    await open({ creatable: true })
    await search('unmatched')
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(1)
    document.querySelector<HTMLElement>('[role="option"]')!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([['unmatched']])
  })
})
