import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const read = (path: string) => readFileSync(resolve('src', path), 'utf8')
describe('guide source wiring boundaries (static evidence)', () => {
  it('mounts the host and wires only the two allowed registration success callbacks', () => {
    expect(read('App.vue')).toContain('<GuideHost />')
    for (const path of ['views/auth/RegisterView.vue', 'views/auth/EmailVerifyView.vue']) {
      const source = read(path)
      expect(source.match(/recordGuideRegistration\(registeredUser\)/g)).toHaveLength(1)
      expect(source.indexOf('recordGuideRegistration(registeredUser)')).toBeGreaterThan(source.indexOf('const registeredUser = await authStore.register('))
    }
    for (const path of ['views/auth/LoginView.vue', 'stores/auth.ts', 'views/auth/WechatCallbackView.vue', 'views/auth/LinuxDoCallbackView.vue']) expect(read(path)).not.toContain('recordGuideRegistration')
  })
  it('keeps administrator code independent and only changes the user sidebar expression', () => {
    for (const path of ['components/Guide/steps.ts', 'composables/useOnboardingTour.ts', 'stores/onboarding.ts', 'styles/onboarding.css']) expect(read(path)).not.toContain('features/guide')
    const sidebar = read('components/layout/AppSidebar.vue')
    expect(sidebar).toContain(':data-tour="sidebarTourId(item.path)"')
    expect(sidebar).toContain(':data-tour="item.path === \'/keys\' ? \'sidebar-my-keys\' : undefined"')
    const keys = read('views/user/KeysView.vue')
    expect(keys).toContain("if (onboardingStore.isCurrentStep('[data-tour=\"key-form-submit\"]')) {\n        onboardingStore.nextStep(500)\n      }")
  })
  it('marks every page target and keeps the settings card immediately after custom menus', () => {
    for (const id of ['header-menu', 'header-balance', 'header-user-menu', 'header-balance-mobile']) expect(read('components/layout/AppHeader.vue')).toContain(`data-tour="${id}"`)
    for (const id of ['key-name', 'key-copy', 'key-group', 'key-actions', 'key-use', 'key-ccs', 'key-toggle', 'key-edit', 'key-delete', 'key-group-dropdown', 'key-group-option', 'key-form-quota']) expect(read('views/user/KeysView.vue')).toContain(`data-tour="${id}"`)
    for (const id of ['recharge-amount', 'recharge-method', 'recharge-submit']) expect(read('views/user/PaymentView.vue')).toContain(`data-tour="${id}"`)
    expect(read('views/user/UserOrdersView.vue')).toContain('<OrderTable data-tour="orders-list"')
    expect(read('views/user/CustomPageView.vue')).toContain('<QuickStartArticle v-else-if="isBuiltinQuickStart" />')
    const settings = read('views/admin/SettingsView.vue')
    expect(settings.indexOf('<GuideSettingsCard />')).toBeGreaterThan(settings.indexOf('<!-- Custom Menu Items -->'))
    expect(settings.indexOf('<GuideSettingsCard />')).toBeLessThan(settings.indexOf('<!-- /Tab: General -->'))
  })
})
