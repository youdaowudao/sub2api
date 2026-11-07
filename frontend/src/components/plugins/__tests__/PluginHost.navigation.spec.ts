import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { defineComponent, reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '@/App.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import type { PluginEntry } from '@/api/plugins'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import { createBridgeDriver } from './bridgeDriver'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({
  list: vi.fn(), session: vi.fn(), call: vi.fn(), action: vi.fn(),
  run: vi.fn((action: () => Promise<unknown>) => action()), cancel: vi.fn(), replay: vi.fn(),
  error: vi.fn(), info: vi.fn(), success: vi.fn(),
}))
const auth = reactive({ token: 'fixture-auth', isAuthenticated: true, isAdmin: false, user: { id: 42, role: 'user', status: 'active' } })
const appStore = { sidebarCollapsed: false, siteName: 'Fixture', siteLogo: '', cachedPublicSettings: null, fetchPublicSettings: async () => {}, showError: mocks.error, showInfo: mocks.info, showSuccess: mocks.success }
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores', () => ({
  useAppStore: () => appStore, useAuthStore: () => auth,
  useSubscriptionStore: () => ({ clear() {} }),
  useAnnouncementStore: () => ({ fetchAnnouncements() {}, reset() {} }),
  useAdminComplianceStore: () => ({ reset() {} }),
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({ setReplayCallback() {} }) }))
vi.mock('@/composables/useOnboardingTour', () => ({ useOnboardingTour: () => ({ replayTour: mocks.replay }) }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: mocks.run, onCancel: mocks.cancel }),
  isStepUpBlocked: () => false, isStepUpCancelled: () => false, stepUpBlockReason: () => '',
}))
vi.mock('@/api/setup', () => ({ getSetupStatus: async () => ({ needs_setup: false }) }))
vi.mock('@/utils/featureFlags', async importOriginal => ({ ...await importOriginal<typeof import('@/utils/featureFlags')>(), isFeatureFlagEnabled: () => false }))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'Fixture' }))
vi.mock('@/api/plugins', async importOriginal => ({ ...await importOriginal<typeof import('@/api/plugins')>(), default: { listEntries: mocks.list, createSession: mocks.session, call: mocks.call, action: mocks.action } }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const entry: PluginEntry = {
  plugin_id: 7, plugin_key: 'example.shell', entry_id: 'helper', revision: 'revision-a', label: 'Helper', mount: 'shell', roles: ['user', 'admin'],
  methods: [{ name: 'read', role: 'user', access: 'read' }, { name: 'write', role: 'user', access: 'write' }],
}
const Dashboard = defineComponent({ components: { AppLayout }, template: '<AppLayout><p>Dashboard page</p></AppLayout>' })
const Keys = defineComponent({ components: { AppLayout }, template: '<AppLayout><p>Keys page</p></AppLayout>' })

async function mountApp(path = '/dashboard') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/dashboard', name: 'Dashboard', component: Dashboard, meta: { requiresAuth: true } },
    { path: '/keys', name: 'Keys', component: Keys, meta: { requiresAuth: true } },
    { path: '/login', component: { template: '<p>Login</p>' }, meta: { requiresAuth: false } },
    { path: '/without-layout', component: { template: '<p>Standalone</p>' }, meta: { requiresAuth: true } },
  ] })
  await router.push(path)
  const wrapper = mount(App, { attachTo: document.body, global: { plugins: [createPinia(), router], stubs: {
    AppSidebar: { template: '<aside />' }, AppHeader: true, TotpStepUpDialog: true,
    Toast: true, NavigationProgress: true, AnnouncementPopup: true, AdminComplianceDialog: true,
  } } })
  await flushPromises()
  return { wrapper, router }
}

describe('the actual App router owns one continuous plugin shell', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] })
    vi.clearAllMocks()
    auth.token = 'fixture-auth'; auth.isAuthenticated = true; auth.user.status = 'active'
    mocks.list.mockResolvedValue([entry])
    mocks.session.mockImplementation(async () => ({ url: '/shell-fixture', bridge_token: 'fixture-bridge', ui_bridge_version: 2, expires_at: '', entry }))
    mocks.call.mockResolvedValue({ request_id: 'read', payload: {} })
    mocks.run.mockImplementation(action => action())
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  it('keeps the original frame, session and port across two different AppLayout pages and ui.navigate', async () => {
    const { wrapper, router } = await mountApp()
    const iframe = wrapper.get('iframe')
    const original = iframe.element
    const driver = createBridgeDriver(original as HTMLIFrameElement, 'fixture-bridge')
    await iframe.trigger('load')
    const firstContent = wrapper.get('main').element
    await router.push('/keys?private=hidden#fragment')
    await flushPromises()
    expect(wrapper.get('main').element).not.toBe(firstContent)
    expect(wrapper.get('iframe').element).toBe(original)
    expect(mocks.session).toHaveBeenCalledTimes(1)
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(driver.windowMessages.mock.calls.filter(([data]) => data.type === 'bridge.challenge')).toHaveLength(1)
    expect(driver.post).toHaveBeenCalledWith(expect.objectContaining({ type: 'context.changed', context: { path: '/keys', route_name: 'Keys', user_id: 42, role: 'user' } }))
    driver.message('context.get', 'new-page')
    expect(driver.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'new-page', context: expect.objectContaining({ path: '/keys' }) }))
    const currentContent = wrapper.get('main').element
    vi.spyOn(currentContent, 'getBoundingClientRect').mockReturnValue({ x: 20, y: 30, right: 220, bottom: 330 } as DOMRect)
    driver.message('ui.anchor', 'new-anchor', { target: 'content' })
    await flushPromises()
    expect(driver.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'new-anchor', rect: { x: 20, y: 30, width: 200, height: 300 } }))
    driver.message('ui.navigate', 'navigate', { target: 'dashboard' })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(wrapper.get('iframe').element).toBe(original)
    expect(document.querySelector('[data-plugin-anchor-outline]')).toBeNull()
    expect(mocks.session).toHaveBeenCalledTimes(1)
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(wrapper.get('iframe').element).toBe(original)
  })

  it('cancels old reads and waiting writes while keeping the original port usable on the new page', async () => {
    let finish!: (value: unknown) => void
    let approve!: () => void
    mocks.call.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    mocks.run.mockImplementationOnce(action => new Promise(resolve => { approve = () => { Promise.resolve().then(action).then(resolve, resolve) } }))
    const { wrapper, router } = await mountApp()
    const iframe = wrapper.get('iframe')
    const driver = createBridgeDriver(iframe.element as HTMLIFrameElement, 'fixture-bridge')
    await iframe.trigger('load')
    driver.message('plugin.call', 'old-read', { method: 'read', payload: {} })
    driver.message('plugin.call', 'old-write', { method: 'write', payload: {} })
    const signal = mocks.call.mock.calls[0][3] as AbortSignal
    driver.post.mockClear()
    await router.push('/keys'); await flushPromises()
    expect(signal.aborted).toBe(true)
    approve(); finish({ request_id: 'obsolete', payload: {} }); await flushPromises()
    expect(mocks.action).not.toHaveBeenCalled()
    expect(driver.post.mock.calls.some(([data]) => data.request_id === 'old-read' || data.request_id === 'old-write')).toBe(false)
    driver.message('context.get', 'still-connected')
    expect(driver.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'still-connected', ok: true, context: expect.objectContaining({ path: '/keys' }) }))
  })

  it.each(['/login', '/without-layout'])('does not load plugins on %s and releases them when entering it', async path => {
    const { wrapper, router } = await mountApp(path)
    expect(mocks.list).not.toHaveBeenCalled()
    expect(wrapper.find('iframe').exists()).toBe(false)
    await router.push('/dashboard'); await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(1)
    await router.push(path); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(mocks.list).toHaveBeenCalledTimes(1)
  })

  it('keeps a manually closed shell closed across navigation and cleans on logout', async () => {
    const { wrapper, router } = await mountApp()
    usePluginEntriesStore().closeShell(); await flushPromises()
    await router.push('/keys'); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(mocks.session).toHaveBeenCalledTimes(1)
    usePluginEntriesStore().openShell(entry); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(true)
    auth.isAuthenticated = false; auth.token = ''; await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('removes a disabled user\'s shell and stops its entry subscription', async () => {
    const { wrapper } = await mountApp()
    auth.user.status = 'disabled'; await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(mocks.list).toHaveBeenCalledTimes(1)
  })

  it('replaces the frame on token renewal or a changed revision, and removes it on disable', async () => {
    const { wrapper } = await mountApp()
    const old = wrapper.get('iframe').element
    auth.token = 'renewed-fixture'; await flushPromises()
    const renewed = wrapper.get('iframe').element
    expect(renewed).not.toBe(old)
    expect(mocks.session).toHaveBeenCalledTimes(2)
    const revised = { ...entry, revision: 'revision-b' }
    mocks.list.mockResolvedValue([revised])
    mocks.session.mockResolvedValue({ url: '/revised-shell', bridge_token: 'fixture-bridge', ui_bridge_version: 2, expires_at: '', entry: revised })
    await usePluginEntriesStore().refresh(); await flushPromises()
    expect(wrapper.get('iframe').element).not.toBe(renewed)
    expect(mocks.session).toHaveBeenCalledTimes(3)
    mocks.list.mockResolvedValue([])
    await usePluginEntriesStore().refresh(); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
