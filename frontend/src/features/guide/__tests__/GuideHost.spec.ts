import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import GuideHost from '../components/GuideHost.vue'
import { installGuideDOM, keyPage, settle } from './fixture'

const { stores } = vi.hoisted(() => ({ stores: { auth: null as any, app: null as any, route: null as any } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => stores.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => stores.app }))
vi.mock('vue-router', () => ({ useRoute: () => stores.route }))
vi.mock('../driver/driver.mjs', async () => ({ driver: (await import('./fixture')).fakeDriver }))

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
  it('offers four separate chapters, closes the chooser on Escape, and hides replay during playback', async () => {
    wrapper = mount(GuideHost, { attachTo: document.body }); await flushPromises()
    const launcher = document.querySelector<HTMLButtonElement>('#s2o-launcher')!
    expect(launcher).not.toBeNull(); launcher.click(); await flushPromises()
    expect(document.querySelectorAll('[data-s2o-module]')).toHaveLength(4)
    document.querySelector<HTMLButtonElement>('[data-s2o-module]')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises(); expect(document.querySelector('#s2o-replay-picker')).toBeNull()
    launcher.click(); await flushPromises(); document.querySelector<HTMLButtonElement>('[data-s2o-module="keys"]')!.click()
    await settle(); expect(document.querySelector('.s2o-popover')).not.toBeNull(); expect(document.querySelector('#s2o-launcher')).toBeNull()
    document.querySelector<HTMLButtonElement>('#s2o-exit')!.click(); await flushPromises()
    expect(document.querySelector('#s2o-launcher')).not.toBeNull()
    expect(document.querySelector('#s2o-note')?.textContent).toBe('已退出引导。')
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
