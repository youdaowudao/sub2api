import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('public component structure used by the guide', () => {
  it('exposes a modal content/header/footer and native close button', async () => {
    const wrapper = mount(BaseDialog, { props: { show: true, title: 'Dialog' }, slots: { default: '<input value="draft" />', footer: '<button>Save</button>' }, global: { stubs: { Icon: true, transition: false } } })
    await flushPromises()
    const dialog = document.querySelector('[role="dialog"][aria-modal="true"] .modal-content')!
    expect(dialog).not.toBeNull()
    expect(dialog.querySelector('.modal-footer button')).not.toBeNull()
    const close = dialog.querySelector<HTMLButtonElement>('.modal-header button[aria-label="Close modal"]')!
    expect(close).not.toBeNull(); close.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(dialog.querySelector('input')?.value).toBe('draft')
    wrapper.unmount()
  })
  it('exposes aria-expanded and a teleported listbox with disabled/group option attributes', async () => {
    const wrapper = mount(Select, { props: { modelValue: null, options: [{ value: 'disabled', label: 'Disabled', disabled: true }, { value: 1, label: 'First' }] }, attachTo: document.body, global: { stubs: { Icon: true, transition: false } } })
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    const list = document.querySelector('.select-dropdown-portal[role="listbox"]')!
    expect(list).not.toBeNull(); expect(list.querySelectorAll('[role="option"]')).toHaveLength(2)
    expect(list.querySelector('[role="option"]')?.getAttribute('aria-disabled')).toBe('true')
    const option = list.querySelectorAll<HTMLElement>('[role="option"]')[1]; option.click(); await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([1]); expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })
})
