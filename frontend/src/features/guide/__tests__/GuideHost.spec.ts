import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import GuideHost from '../components/GuideHost.vue'
import { installGuideDOM, keyPage, nextGuideStep, settle } from './fixture'
import { progressKey, recordGuideRegistration, registrationKey, suppressedKey } from '../registration'
import { beginGuideAction, completeGuideAction } from '../businessEvents'

const { stores } = vi.hoisted(() => ({ stores: { auth: null as any, app: null as any, route: null as any } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => stores.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => stores.app }))
vi.mock('vue-router', () => ({ useRoute: () => stores.route }))
vi.mock('../driver/driver.mjs', async () => ({ driver: (await import('./fixture')).fakeDriver }))
// The shipped build turns every chapter off. Host tests keep the shipped bindings with every chapter on.
vi.mock('../config/onboarding.json', async (importOriginal) => {
  const shipped = (await importOriginal<any>()).default
  return { default: { ...shipped, modules: shipped.modules.map((module: any) => ({ ...module, enabled: true })) } }
})

describe('ordinary guide host and replay picker', () => {
  let wrapper: VueWrapper | undefined
  beforeEach(() => {
    installGuideDOM(); sessionStorage.clear(); localStorage.clear(); history.replaceState({}, '', '/keys'); keyPage()
    stores.auth = reactive({ user: { id: 7, role: 'user' }, isAuthenticated: true, isSimpleMode: false })
    localStorage.setItem('auth_token', 'test-session')
    localStorage.setItem('auth_user', JSON.stringify(stores.auth.user))
    stores.app = reactive({ backendModeEnabled: false, cachedPublicSettings: { payment_enabled: false } })
    stores.route = reactive({ path: '/keys', fullPath: '/keys', meta: { requiresAuth: true, requiresAdmin: false } })
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
  async function openPicker() {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    const launcher = document.querySelector<HTMLButtonElement>('#s2o-launcher')!
    launcher.click(); await flushPromises()
    expect(document.querySelector('#s2o-replay-picker')).not.toBeNull()
    return launcher
  }
  it.each(['/register', '/email-verify'])('automatically welcomes B after A exits and logs out in the same host through %s', async registrationPath => {
    const navigate = (path: string, requiresAuth: boolean) => {
      history.replaceState({}, '', path)
      Object.assign(stores.route, { path, fullPath: path, meta: { requiresAuth, requiresAdmin: false } })
    }
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    recordGuideRegistration(stores.auth.user); await settle()
    expect(document.querySelectorAll('[data-s2o-step="welcome"]')).toHaveLength(1)
    const firstRegistration = JSON.parse(sessionStorage.getItem(progressKey)!).registrationId
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await settle()
    expect(document.querySelector('#s2o-launcher')).not.toBeNull()
    stores.auth.user = null; stores.auth.isAuthenticated = false
    localStorage.removeItem('auth_token'); localStorage.removeItem('auth_user')
    navigate(registrationPath, false); await settle()
    stores.auth.user = { id: 8, role: 'user' }; stores.auth.isAuthenticated = true
    localStorage.setItem('auth_token', 'second-test-session')
    localStorage.setItem('auth_user', JSON.stringify(stores.auth.user))
    recordGuideRegistration(stores.auth.user); await settle()
    expect(document.querySelector('[data-s2o-step="welcome"]')).toBeNull()
    expect(JSON.parse(sessionStorage.getItem(registrationKey)!)).toMatchObject({ userId: 8 })
    navigate('/keys', true); await settle()
    expect(document.querySelectorAll('[data-s2o-step="welcome"]')).toHaveLength(1)
    expect(document.querySelector('#s2o-launcher, #s2o-note')).toBeNull()
    const progress = JSON.parse(sessionStorage.getItem(progressKey)!)
    expect(progress.userId).toBe(8); expect(progress.registrationId).not.toBe(firstRegistration)
    expect(progress.modules).toEqual(['keys', 'key-use', 'quick-start', 'recharge', 'more'])
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await settle()
    localStorage.setItem('auth_token', 'renewed-second-test-session')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    document.body.append(document.createElement('span')); await settle()
    expect(document.querySelector('[data-s2o-step="welcome"]')).toBeNull()
    expect(document.querySelector('#s2o-launcher')).not.toBeNull()
  })
  it('keeps manual replay available to B when storage denies the new automatic registration reset', async () => {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    recordGuideRegistration(stores.auth.user); await settle()
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await settle()
    stores.auth.user = null; stores.auth.isAuthenticated = false
    localStorage.removeItem('auth_token'); localStorage.removeItem('auth_user')
    history.replaceState({}, '', '/register')
    Object.assign(stores.route, { path: '/register', fullPath: '/register', meta: { requiresAuth: false, requiresAdmin: false } })
    await settle(); expect(document.querySelector('#s2o-launcher')).toBeNull()
    const remove = Storage.prototype.removeItem
    const denied = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, key: string) {
      if (key === suppressedKey) throw new DOMException('Denied')
      return remove.call(this, key)
    })
    try {
      stores.auth.user = { id: 8, role: 'user' }; stores.auth.isAuthenticated = true
      localStorage.setItem('auth_token', 'second-test-session')
      localStorage.setItem('auth_user', JSON.stringify(stores.auth.user))
      recordGuideRegistration(stores.auth.user); await settle()
      history.replaceState({}, '', '/keys')
      Object.assign(stores.route, { path: '/keys', fullPath: '/keys', meta: { requiresAuth: true, requiresAdmin: false } })
      await settle()
      expect(document.querySelector('[data-s2o-step="welcome"]')).toBeNull()
      expect(JSON.parse(sessionStorage.getItem(registrationKey)!)).toMatchObject({ userId: 8 })
      expect(document.querySelector('#s2o-launcher')).not.toBeNull()
      document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
      document.querySelector<HTMLButtonElement>('[data-s2o-module="more"]')!.click(); await settle()
      expect(document.querySelector('[data-s2o-step="more-unavailable"]')).not.toBeNull()
      expect(sessionStorage.getItem(registrationKey)).toBeNull()
    } finally { denied.mockRestore() }
  })
  it.each(['continue', 'automatic', 'end'])('releases the page at a registration chapter break and supports %s', async action => {
    stores.app.cachedPublicSettings.custom_menu_items = [{ id: 'onboarding-quick-start', visibility: 'user' }]
    document.body.insertAdjacentHTML('beforeend', '<a data-tour="sidebar-custom-onboarding-quick-start"></a><input id="practice" value="draft" />')
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    recordGuideRegistration(stores.auth.user); await settle(); await nextGuideStep()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 71 })
    document.querySelector('[role="dialog"]')!.remove(); await settle(); await nextGuideStep()
    const button = document.querySelector<HTMLButtonElement>('#s2o-continue')!
    expect(button).not.toBeNull(); expect(button.disabled).toBe(false)
    expect(document.querySelector('#s2o-continuation')?.textContent).toContain('下一章：使用密钥')
    expect(document.querySelector('#s2o-continuation')?.textContent).toContain('5 秒后自动继续，也可立即继续。')
    expect(document.querySelector('.s2o-popover, #s2o-exit, #s2o-launcher')).toBeNull()
    expect(document.body.classList.contains('s2o-active')).toBe(false)
    const practice = document.querySelector<HTMLInputElement>('#practice')!
    practice.focus(); practice.value = 'free edit'; await vi.advanceTimersByTimeAsync(1000)
    expect(document.activeElement).toBe(practice); expect(practice.value).toBe('free edit')
    expect(document.querySelector('#s2o-continue')).not.toBeNull()
    if (action !== 'end') {
      if (action === 'automatic') await vi.advanceTimersByTimeAsync(4100)
      else { button.click(); button.click(); await settle() }
      expect(document.querySelector('[data-s2o-step="usage-row"]')).not.toBeNull()
      expect(document.querySelector('#s2o-continuation')).toBeNull()
      document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await flushPromises()
    } else {
      document.querySelector<HTMLButtonElement>('#s2o-continuation button:last-child')!.click(); await flushPromises()
      expect(document.querySelector('.s2o-popover, #s2o-continuation')).toBeNull()
    }
    document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
    expect(document.querySelector('[data-s2o-module="keys"]')?.textContent).toContain('已查看')
    expect(document.querySelector('[data-s2o-module="key-use"]')?.textContent).toContain(action !== 'end' ? '已查看' : '未查看')
    expect(document.querySelector('[data-s2o-module="quick-start"]')?.textContent).toContain('未查看')
  })
  it('remembers opened chapters after exiting the guide, but not after the host is recreated', async () => {
    await openPicker()
    expect(document.querySelector('[data-s2o-module="more"]')?.textContent).toContain('未查看')
    document.querySelector<HTMLButtonElement>('[data-s2o-module="more"]')!.click(); await settle()
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await flushPromises()
    document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
    expect(document.querySelector('[data-s2o-module="more"]')?.classList.contains('s2o-replay-viewed')).toBe(true)
    expect(document.querySelector('[data-s2o-module="more"]')?.textContent).toContain('已查看')
    expect(document.querySelector('[data-s2o-module="keys"]')?.textContent).toContain('未查看')
    wrapper!.unmount(); wrapper = undefined
    await openPicker()
    expect(document.querySelector('[data-s2o-module="more"]')?.textContent).toContain('未查看')
  })
  it.each(['focusin', 'pointerdown'])('keeps the business control focused when %s outside dismisses the picker', async source => {
    const outside = document.createElement('input'); outside.id = 'outside-guide-control'; document.body.append(outside)
    const launcher = await openPicker(), restored = vi.spyOn(launcher, 'focus')
    if (source === 'pointerdown') outside.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    outside.focus(); await flushPromises()
    expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    expect(document.activeElement).toBe(outside)
    expect(restored).not.toHaveBeenCalled()
  })
  it.each(['escape', 'close-button'])('returns focus to the launcher after explicit %s dismissal', async source => {
    const launcher = await openPicker()
    if (source === 'escape') document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    else document.querySelector<HTMLButtonElement>('.s2o-replay-close')!.click()
    await flushPromises()
    expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    expect(document.activeElement).toBe(launcher)
  })
  it.each(['popstate', 'hashchange', 'pagehide', 'storage'])('closes on %s without moving focus back to the launcher', async name => {
    const launcher = await openPicker(), restored = vi.spyOn(launcher, 'focus')
    const event = name === 'storage' ? new StorageEvent(name, { key: 'guide-focus-test' }) : new Event(name)
    window.dispatchEvent(event); await flushPromises()
    expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    expect(restored).not.toHaveBeenCalled()
  })
  it('returns focus synchronously before starting a selected chapter', async () => {
    const launcher = await openPicker(), restored = vi.spyOn(launcher, 'focus')
    document.querySelector<HTMLButtonElement>('[data-s2o-module="key-use"]')!.click()
    expect(restored).toHaveBeenCalledWith({ preventScroll: true })
    await settle()
    expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    expect(document.querySelector('.s2o-popover')).not.toBeNull()
  })
  it('offers five separate chapters, closes the chooser on Escape, and hides the launcher during playback', async () => {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    const launcher = document.querySelector<HTMLButtonElement>('#s2o-launcher')!
    expect(launcher).not.toBeNull(); launcher.click(); await flushPromises()
    expect(document.querySelectorAll('[data-s2o-module]')).toHaveLength(5)
    document.querySelector<HTMLButtonElement>('[data-s2o-module]')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises(); expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    launcher.click(); await flushPromises(); document.querySelector<HTMLButtonElement>('[data-s2o-module="keys"]')!.click()
    await settle(); expect(document.querySelector('.s2o-popover')).not.toBeNull(); expect(document.querySelector('#s2o-launcher')).toBeNull()
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await flushPromises()
    expect(document.querySelector('#s2o-launcher')).not.toBeNull()
    expect(document.querySelector('#s2o-note')?.textContent).toBe('已退出引导。')
  })
  it('opens the overview through 使用引导, completes it and can choose it again', async () => {
    const launcher = await openPicker()
    expect(launcher.textContent).toBe('使用引导')
    expect(document.querySelector('#s2o-replay-title')?.textContent).toBe('选择要查看的内容')
    expect([...document.querySelectorAll('[data-s2o-module]')].at(-1)?.getAttribute('data-s2o-module')).toBe('more')
    document.querySelector<HTMLButtonElement>('[data-s2o-module="more"]')!.click(); await settle()
    expect(document.querySelector('[data-s2o-step="more-unavailable"]')).not.toBeNull()
    expect(document.querySelector('#s2o-launcher, #s2o-replay-picker')).toBeNull()
    await nextGuideStep(); await nextGuideStep()
    expect(document.querySelector('.s2o-popover, #s2o-exit, #s2o-attention-wait')).toBeNull()
    expect(document.querySelector('#s2o-note')?.textContent).toBe('已看完本次内容。')
    expect(document.querySelector('#s2o-note')?.textContent).not.toContain('重播')
    document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-s2o-module="more"]')!.click(); await settle()
    expect(document.querySelector('[data-s2o-step="more-unavailable"]')).not.toBeNull()
  })
  it.each(['admin', 'guest'])('does not render ordinary replay for %s', async role => {
    stores.auth.user.role = role
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    expect(document.querySelector('#s2o-launcher')).toBeNull()
  })
  it('hides the replay entry and chooser after another tab logs out while its store still has the old user', async () => {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
    expect(document.querySelector('#s2o-replay-picker')).not.toBeNull()
    localStorage.removeItem('auth_token')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' })); await flushPromises()
    expect(stores.auth.isAuthenticated).toBe(true)
    expect(document.querySelector('#s2o-launcher, #s2o-replay-picker')).toBeNull()
  })
  it('removes the launcher and active resources when the host unmounts', async () => {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    document.querySelector<HTMLButtonElement>('#s2o-launcher')!.click(); await flushPromises()
    document.querySelector<HTMLButtonElement>('[data-s2o-module="keys"]')!.click(); await settle()
    wrapper.unmount(); wrapper = undefined; await flushPromises()
    expect(document.querySelector('#s2o-launcher, #s2o-exit, #s2o-replay-picker, #s2o-attention-wait, .s2o-popover')).toBeNull()
  })
})
