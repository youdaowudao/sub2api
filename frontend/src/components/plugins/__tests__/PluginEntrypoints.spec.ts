import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive, ref } from 'vue'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import PluginShell from '../PluginShell.vue'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import type { PluginEntry } from '@/api/plugins'
import { createBridgeDriver } from './bridgeDriver'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({ list: vi.fn(), session: vi.fn(), call: vi.fn(), info: vi.fn() }))
const auth = reactive({ token: 'private', isAuthenticated: true, isAdmin: false, isSimpleMode: false, user: { id: 42, role: 'user', status: 'active' } })
const app = reactive({ sidebarCollapsed: false, mobileOpen: false, cachedPublicSettings: {}, publicSettingsLoaded: true, sidebarScrollTop: 0, siteName: 'Sub2API' })
const route = reactive({ path: '/keys', fullPath: '/keys', name: 'Keys' })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showInfo: mocks.info, showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/stores', () => ({
  useAuthStore: () => auth, useAppStore: () => app,
  useAdminSettingsStore: () => ({ customMenuItems: [], fetch: vi.fn() }),
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: {}, makeSidebarFlag: () => () => true, resolveFeatureFlag: () => true }))
vi.mock('@/api/plugins', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/plugins')>(), default: { listEntries: mocks.list, createSession: mocks.session, call: mocks.call, action: vi.fn() },
}))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: (action: () => Promise<unknown>) => action(), onCancel: vi.fn() }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, values?: { name?: string }) => key + (values?.name || '') }) }))

const entry: PluginEntry = { plugin_id: 7, plugin_key: 'example.ui', revision: 'one', entry_id: 'home', label: 'Plugin business', mount: 'user', roles: ['user', 'admin'], methods: [{ name: 'read', role: 'user', access: 'read' }] }

describe('plugin menu and shell entrypoints', () => {
  beforeEach(() => {
    setActivePinia(createPinia()); vi.clearAllMocks()
    auth.isAdmin = false; auth.user.role = 'user'; auth.token = 'private'
    mocks.list.mockResolvedValue([entry])
  })

  it('renders server entries as existing sidebar links and removes them on disable or role changes', async () => {
    mocks.list.mockResolvedValue([entry, { ...entry, mount: 'admin', entry_id: 'admin', label: 'Admin business', roles: ['admin'] }])
    const store = usePluginEntriesStore()
    await store.refresh()
    const wrapper = mount(AppSidebar, { global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }, VersionBadge: true, Icon: true } } })
    expect(wrapper.find('a[href="/extensions/7/home"]').text()).toContain('Plugin business')
    expect(wrapper.find('a[href="/admin/extensions/7/admin"]').exists()).toBe(false)
    auth.user.role = 'admin'; auth.isAdmin = true
    await store.refresh(); await flushPromises()
    expect(wrapper.find('a[href="/admin/extensions/7/admin"]').text()).toContain('Admin business')
    expect(wrapper.find('a[href="/extensions/7/home"]').exists()).toBe(true)
    mocks.list.mockResolvedValue([])
    await store.refresh(); await flushPromises()
    expect(wrapper.find('a[href="/extensions/7/home"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/extensions/7/admin"]').exists()).toBe(false)
  })

  it('keeps one frame, destroys requests and outlines on close, and obtains a fresh session on reopen', async () => {
    const shells: PluginEntry[] = [
      { ...entry, mount: 'shell', entry_id: 'first', label: 'First helper' },
      { ...entry, mount: 'shell', entry_id: 'second', label: 'Second helper' },
    ]
    mocks.list.mockResolvedValue(shells)
    mocks.session.mockImplementation(async (_id: number, entryID: string) => ({ url: `/plugin-ui/${entryID}`, bridge_token: entryID, ui_bridge_version: 2, expires_at: '', entry: shells.find(entry => entry.entry_id === entryID) }))
    mocks.call.mockImplementation(() => new Promise(() => {}))
    const store = usePluginEntriesStore()
    await store.refresh()
    const anchor = document.createElement('main')
    const wrapper = mount(PluginShell, { props: { anchors: { navigation: anchor, content: anchor } }, attachTo: document.body, global: { stubs: { TotpStepUpDialog: true } } })
    await flushPromises()
    expect(wrapper.findAll('iframe')).toHaveLength(1)
    const iframe = wrapper.get('iframe')
    const { message: dispatch } = createBridgeDriver(iframe.element as HTMLIFrameElement, 'first')
    await iframe.trigger('load')
    dispatch('ui.anchor', 'anchor', { target: 'content' })
    dispatch('plugin.call', 'pending', { method: 'read', payload: {} })
    await flushPromises()
    expect(document.querySelector('[data-plugin-anchor-outline]')).not.toBeNull()
    const signal = mocks.call.mock.calls[0][3] as AbortSignal
    await wrapper.findAll('button').find(button => button.text() === 'admin.plugins.closePanel')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(signal.aborted).toBe(true)
    expect(document.querySelector('[data-plugin-anchor-outline]')).toBeNull()
    dispatch('plugin.call', 'stale', { method: 'read', payload: {} })
    expect(mocks.call).toHaveBeenCalledTimes(1)
    await wrapper.findAll('button').find(button => button.text().includes('First helper'))!.trigger('click')
    await flushPromises()
    expect(mocks.session).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('iframe')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text().includes('Second helper'))!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('iframe')).toHaveLength(1)
    expect(wrapper.get('iframe').attributes('src')).toBe('/plugin-ui/second')
    mocks.list.mockResolvedValue([])
    await store.refresh(); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(mocks.info).toHaveBeenCalledWith('admin.plugins.pageDisabled')
  })
})
