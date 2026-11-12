import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import zh from '@/i18n/locales/zh'
import AppSidebar from '../AppSidebar.vue'

const mocks = vi.hoisted(() => ({ rate: vi.fn(), warning: vi.fn() }))
const auth = reactive({ isAuthenticated: true, isAdmin: false, isSimpleMode: false, user: { id: 1, role: 'user', status: 'active' } })
const app = reactive({ sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0, backendModeEnabled: false, publicSettingsLoaded: true,
  siteName: 'Test', siteLogo: '', siteVersion: '', cachedPublicSettings: { affiliate_enabled: true, custom_menu_items: [] },
  toggleSidebar() { app.sidebarCollapsed = !app.sidebarCollapsed }, setMobileOpen(open: boolean) { app.mobileOpen = open }, showWarning: mocks.warning,
})
const admin = { customMenuItems: [], fetch: vi.fn(), paymentEnabled: false }
const onboarding = { isCurrentStep: () => false }
vi.mock('@/stores', () => ({ useAppStore: () => app, useAuthStore: () => auth, useAdminSettingsStore: () => admin, useOnboardingStore: () => onboarding }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('@/stores/pluginEntries', () => ({ usePluginEntriesStore: () => ({ forMount: () => [] }) }))
vi.mock('@/api/user', () => ({ getAffiliateRebateRate: mocks.rate }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: ref(false), refreshBatchImageAccess: vi.fn() }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia()); localStorage.clear(); vi.clearAllMocks()
  auth.isAuthenticated = true; auth.isAdmin = false; auth.isSimpleMode = false; auth.user.id = 1
  app.sidebarCollapsed = false; app.mobileOpen = false; app.backendModeEnabled = false
  app.cachedPublicSettings.affiliate_enabled = true
  mocks.rate.mockResolvedValue({ effective_rebate_rate_percent: 10 })
})
afterEach(() => { vi.restoreAllMocks() })

async function create() {
  const router = createRouter({ history: createMemoryHistory(), routes: ['/dashboard', '/affiliate'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/dashboard'); await router.isReady()
  const wrapper = mount(AppSidebar, { global: { plugins: [createPinia(), router, createI18n({ legacy: false, locale: 'zh', messages: { zh } })], stubs: { VersionBadge: true, BrandName: true } } })
  await flushPromises()
  return { wrapper, router }
}

describe('sidebar referral promotion integration', () => {
  it('keeps folding separate from navigation and preserves the choice through remount', async () => {
    const { wrapper, router } = await create()
    expect(wrapper.get('[data-testid=affiliate-promo-card]').text()).toContain('10%')
    app.mobileOpen = true
    await wrapper.get('[aria-label="收起推广卡片"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(app.mobileOpen).toBe(true); expect(app.sidebarCollapsed).toBe(false)
    expect(localStorage.getItem('sub2api:affiliate-promo:collapsed:1')).toBe('1')
    wrapper.unmount()
    const restored = await create()
    expect(restored.wrapper.find('[aria-label="展开推广卡片"]').exists()).toBe(true)
    expect(mocks.rate).toHaveBeenCalledTimes(1)
    await restored.wrapper.get('[data-testid=affiliate-promo-card] a').trigger('click'); await flushPromises()
    expect(restored.router.currentRoute.value.path).toBe('/affiliate')
  })

  it.each(['disabled', 'simple', 'backend', 'logged out'])('hides the promotion and never requests a rate when %s', async mode => {
    if (mode === 'disabled') app.cachedPublicSettings.affiliate_enabled = false
    if (mode === 'simple') auth.isSimpleMode = true
    if (mode === 'backend') app.backendModeEnabled = true
    if (mode === 'logged out') auth.isAuthenticated = false
    const { wrapper } = await create()
    expect(wrapper.find('[data-testid=affiliate-promo-card]').exists()).toBe(false)
    expect(mocks.rate).not.toHaveBeenCalled()
  })

  it('rejects late responses from the previous account and reloads for the new account', async () => {
    let finish!: (value: { effective_rebate_rate_percent: number }) => void
    mocks.rate.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { wrapper } = await create()
    auth.user.id = 2
    mocks.rate.mockResolvedValueOnce({ effective_rebate_rate_percent: 12.5 })
    await flushPromises()
    finish({ effective_rebate_rate_percent: 99 }); await flushPromises()
    expect(mocks.rate).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid=affiliate-promo-card]').text()).toContain('12.5%')
    expect(wrapper.text()).not.toContain('99%')
  })

  it('uses the administrator personal invitation entry without changing the destination', async () => {
    auth.isAdmin = true
    app.backendModeEnabled = true
    const { wrapper, router } = await create()
    expect(wrapper.find('[data-testid=affiliate-promo-card]').exists()).toBe(true)
    await wrapper.get('[data-testid=affiliate-promo-card] a').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/affiliate')
  })

  it('does not fetch while compact, keeps card preference independent, and warns if saving fails', async () => {
    app.sidebarCollapsed = true
    const { wrapper } = await create()
    expect(mocks.rate).not.toHaveBeenCalled()
    app.sidebarCollapsed = false; await flushPromises()
    expect(mocks.rate).toHaveBeenCalledTimes(1)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
    await wrapper.get('[aria-label="收起推广卡片"]').trigger('click'); await flushPromises()
    expect(mocks.warning).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[aria-label="展开推广卡片"]').exists()).toBe(true)
  })
})
