import { describe, expect, it, vi } from 'vitest'
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ checkAuth: vi.fn(), isAuthenticated: false, isAdmin: false, isSimpleMode: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ siteName: 'Sub2API', backendModeEnabled: false, cachedPublicSettings: null }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/stores/adminCompliance', () => ({ useAdminComplianceStore: () => ({}) }))
vi.mock('@/composables/useNavigationLoading', () => ({ useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn() }) }))
vi.mock('@/composables/useRoutePrefetch', () => ({ useRoutePrefetch: () => ({ triggerPrefetch: vi.fn() }) }))
vi.mock('@/views/auth/LoginView.vue', () => ({ default: { template: '<div />' } }))

describe('inline monitor scroll', () => {
  it('retains position on monitor query updates and still restores browser history', async () => {
    const { default: router } = await import('@/router')
    const behavior = router.options.scrollBehavior!
    const ops = router.resolve('/admin/ops?errors_scope=request')
    expect(await behavior(ops, router.resolve('/admin/ops?errors_scope=all'), null)).toBe(false)
    expect(await behavior(ops, router.resolve('/admin/usage'), null)).toEqual({ top: 0 })
    expect(await behavior(ops, ops, { left: 0, top: 930 })).toEqual({ left: 0, top: 930 })
    expect(await behavior(router.resolve('/admin/usage?page=2'), router.resolve('/admin/usage'), null)).toEqual({ top: 0 })
  })
})
