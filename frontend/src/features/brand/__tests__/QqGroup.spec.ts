import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import QqGroupCard from '../components/QqGroupCard.vue'
import QqGroupInline from '../components/QqGroupInline.vue'

const { copyToClipboard } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true)
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

function mountWith(component: typeof QqGroupCard | typeof QqGroupInline) {
  return mount(component, {
    attachTo: document.body,
    global: { stubs: { Icon: true } }
  })
}

describe.each([
  ['QqGroupCard', QqGroupCard],
  ['QqGroupInline', QqGroupInline]
])('%s', (_name, component) => {
  beforeEach(() => {
    copyToClipboard.mockClear()
    document.body.innerHTML = ''
  })

  it('shows the group number', () => {
    const wrapper = mountWith(component)

    expect(wrapper.text()).toContain('1020015031')
    wrapper.unmount()
  })

  it('copies the group number with the copied message', async () => {
    const wrapper = mountWith(component)
    await wrapper.get('[data-testid="qq-copy"]').trigger('click')

    expect(copyToClipboard).toHaveBeenCalledWith('1020015031', 'community.copied')
    wrapper.unmount()
  })

  it('opens the QR dialog with the group card image and join link', async () => {
    const wrapper = mountWith(component)
    expect(document.querySelector('[data-testid="qq-dialog"]')).toBeNull()

    await wrapper.get('[data-testid="qq-scan"]').trigger('click')
    await flushPromises()

    const image = document.querySelector('[data-testid="qq-qr-image"]') as HTMLImageElement
    const link = document.querySelector('[data-testid="qq-join-link"]') as HTMLAnchorElement
    expect(image.getAttribute('src')).toBe('/brand/qq-group-card.jpg')
    expect(link.getAttribute('href')).toBe('https://qm.qq.com/q/o80PFAbey4')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
    expect(document.body.textContent).toContain('好用AI客服通知群')
    wrapper.unmount()
  })
})

describe('QqGroupInline reward line', () => {
  it('shows the newcomer reward for joining the group', () => {
    const wrapper = mountWith(QqGroupInline)

    expect(wrapper.text()).toContain('community.newcomerReward')
    wrapper.unmount()
  })
})
