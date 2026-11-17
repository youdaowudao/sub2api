import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
enableAutoUnmount(afterEach)
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})

it('keeps an opted-in dialog and its scrolled content when returning from details', async () => {
  const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Group', keepMounted: true }, slots: { default: '<div style="height:2000px">rows</div>' }, global: { stubs: { Icon: true } } })
  await nextTick()
  const body = document.body.querySelector<HTMLElement>('.modal-body')!
  body.scrollTop = 480
  await wrapper.setProps({ show: false })
  expect(document.body.querySelector('.modal-body')).toBe(body)
  await wrapper.setProps({ show: true }); await nextTick()
  expect(document.body.querySelector('.modal-body')).toBe(body)
  expect(body.scrollTop).toBe(480)
  wrapper.unmount()
})
