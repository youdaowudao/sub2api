import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupBadge from '../GroupBadge.vue'
import PlatformIcon from '../PlatformIcon.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null })
}))

describe('GroupBadge iconPlatform', () => {
  it('uses the specified icon and keeps the platform color', () => {
    const wrapper = mount(GroupBadge, {
      props: { name: 'g', platform: 'openai', iconPlatform: 'kimi' },
      global: { stubs: { PlatformIcon: true } }
    })

    expect.soft(wrapper.getComponent(PlatformIcon).props('platform')).toBe('kimi')
    expect.soft(wrapper.classes()).toContain('bg-green-50')
    expect.soft(wrapper.classes()).not.toContain('bg-pink-50')
  })

  it('uses the platform icon and color when no icon is specified', () => {
    const wrapper = mount(GroupBadge, {
      props: { name: 'g', platform: 'openai' },
      global: { stubs: { PlatformIcon: true } }
    })

    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('openai')
    expect(wrapper.classes()).toContain('bg-green-50')
  })
})
