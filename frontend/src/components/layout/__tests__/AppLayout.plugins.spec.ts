import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, reactive } from 'vue'
import AppLayout from '../AppLayout.vue'
import PluginHost from '@/components/plugins/PluginHost.vue'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({ list: vi.fn(), replay: vi.fn(), setReplay: vi.fn() }))
const auth = reactive({ token: 'private', isAuthenticated: true, user: { id: 42, role: 'user', status: 'active' } })
const route = reactive({ meta: { requiresAuth: false } })
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ sidebarCollapsed: false }) }))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({ setReplayCallback: mocks.setReplay }) }))
vi.mock('@/composables/useOnboardingTour', () => ({ useOnboardingTour: () => ({ replayTour: mocks.replay }) }))
vi.mock('@/api/plugins', async importOriginal => ({ ...await importOriginal<typeof import('@/api/plugins')>(), default: { listEntries: mocks.list } }))

describe('AppLayout plugin lifecycle', () => {
  beforeEach(() => {
    setActivePinia(createPinia()); vi.clearAllMocks()
    route.meta.requiresAuth = false; auth.isAuthenticated = true; auth.token = 'private'
    mocks.list.mockResolvedValue([])
  })

  it('keeps public pages free of plugin loading, keeps the existing tour, and cleans the authenticated poller', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    try {
      const root = defineComponent({ components: { PluginHost, AppLayout }, template: '<PluginHost><AppLayout><p>Page content</p></AppLayout></PluginHost>' })
      const wrapper = mount(root, { global: { stubs: {
        AppSidebar: { template: '<aside />' }, AppHeader: true,
        PluginShell: { props: ['anchors'], template: '<section data-plugin-shell />' },
      } } })
      await flushPromises()
      expect(mocks.list).not.toHaveBeenCalled()
      expect(wrapper.find('[data-plugin-shell]').exists()).toBe(false)
      expect(mocks.setReplay).toHaveBeenCalledWith(mocks.replay)
      route.meta.requiresAuth = true
      await flushPromises()
      expect(mocks.list).toHaveBeenCalledTimes(1)
      expect(wrapper.find('[data-plugin-shell]').exists()).toBe(true)
      expect(vi.getTimerCount()).toBe(1)
      auth.isAuthenticated = false; auth.token = ''
      await flushPromises()
      expect(wrapper.find('[data-plugin-shell]').exists()).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
      wrapper.unmount()
      expect(vi.getTimerCount()).toBe(0)
    } finally { vi.useRealTimers() }
  })
})
