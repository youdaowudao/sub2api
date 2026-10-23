import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import PluginView from '../PluginView.vue'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import type { PluginEntry } from '@/api/plugins'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({ list: vi.fn() }))
const route = reactive({ params: { pluginID: '7', entryID: 'home' }, meta: { requiresAdmin: false } })
const auth = reactive({ token: 'private', isAuthenticated: true, user: { id: 42, role: 'user', status: 'active' } })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ back: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/api/plugins', async importOriginal => ({ ...await importOriginal<typeof import('@/api/plugins')>(), default: { listEntries: mocks.list } }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const entry: PluginEntry = { plugin_id: 7, plugin_key: 'example.ui', revision: 'one', entry_id: 'home', label: 'Plugin page', mount: 'user', roles: ['user', 'admin'], methods: [] }
function mountView() {
  return mount(PluginView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, PluginFrame: { name: 'PluginFrame', props: ['entry'], emits: ['unavailable'], template: '<div data-business-frame>{{ entry.label }}</div>' } } } })
}

describe('plugin route access', () => {
  beforeEach(() => {
    setActivePinia(createPinia()); vi.clearAllMocks()
    auth.token = 'private'
    auth.user.role = 'user'; route.params.pluginID = '7'; route.params.entryID = 'home'; route.meta.requiresAdmin = false
    mocks.list.mockResolvedValue([entry])
  })

  it('reloads trusted entries on direct access and closes the page when disabled', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-business-frame]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('admin.plugins.pageForbidden')
    mocks.list.mockResolvedValue([])
    await usePluginEntriesStore().refresh()
    await flushPromises()
    expect(wrapper.find('[data-business-frame]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.plugins.pageDisabled')
  })

  it.each([
    ['admin', ['admin']], ['shell', ['user', 'admin']], ['user', ['admin']],
  ])('does not open an entry for the wrong mount or role (%s)', async (mountKind, roles) => {
    mocks.list.mockResolvedValue([{ ...entry, mount: mountKind, roles }])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-business-frame]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.plugins.pageForbidden')
  })

  it('opens an admin entry only on its admin route and checks the next direct entry again', async () => {
    auth.user.role = 'admin'; route.meta.requiresAdmin = true
    mocks.list.mockResolvedValue([{ ...entry, mount: 'admin', roles: ['admin'] }])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-business-frame]').exists()).toBe(true)
    route.params.entryID = 'unknown'
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-business-frame]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.plugins.pageForbidden')
  })

  it('checks the mount again when switching between the user and admin routes for the same IDs', async () => {
    auth.user.role = 'admin'
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-business-frame]').exists()).toBe(true)
    route.meta.requiresAdmin = true
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-business-frame]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.plugins.pageForbidden')
  })

  it.each([false, true])('recovers after a delayed token renewal, ignoring an obsolete frame notice (%s)', async staleFrameNotice => {
    const wrapper = mountView()
    await flushPromises()
    const originalFrame = wrapper.findComponent({ name: 'PluginFrame' })
    const pending: Array<{ signal: AbortSignal; resolve: (entries: PluginEntry[]) => void }> = []
    mocks.list.mockImplementation((signal: AbortSignal) => new Promise((resolve, reject) => {
      pending.push({ signal, resolve })
      signal.addEventListener('abort', () => reject(new Error('cancelled old identity')), { once: true })
    }))
    const release = usePluginEntriesStore().acquire()
    try {
      expect(pending).toHaveLength(1)
      auth.token = 'renewed-private'
      if (staleFrameNotice) originalFrame.vm.$emit('unavailable', 'forbidden')
      await flushPromises()
      expect(pending[0].signal.aborted).toBe(true)
      expect(pending.at(-1)!.signal.aborted).toBe(false)
      pending.at(-1)!.resolve([entry])
      await flushPromises()
      expect(wrapper.find('[data-business-frame]').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('admin.plugins.pageForbidden')
      expect(wrapper.text()).not.toContain('admin.plugins.pageDisabled')
    } finally {
      release()
    }
  })
})
