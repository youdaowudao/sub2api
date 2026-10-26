import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createGuideController, type GuideState } from '../controller'
import config from '../config/onboarding.json'
import { normalizeConfig } from '../config'
import { historyKey, progressKey, recordGuideRegistration, registrationKey, suppressedKey } from '../registration'
import { FLOW_VERSION } from '../steps'
import { beginGuideAction, completeGuideAction } from '../businessEvents'
import { fakeDriver, installGuideDOM, keyPage, nextGuideStep, settle } from './fixture'

describe('guide controller authentication and lifetime', () => {
  let controller: ReturnType<typeof createGuideController> | undefined
  let state: GuideState
  beforeEach(() => {
    installGuideDOM(); sessionStorage.clear(); localStorage.clear(); history.replaceState({}, '', '/keys'); keyPage()
    state = { user: { id: 7, role: 'user' }, authenticated: true, simpleMode: false, backendMode: false, path: '/keys', requiresAuth: true, requiresAdmin: false }
    localStorage.setItem('auth_token', 'test-session')
    localStorage.setItem('auth_user', JSON.stringify(state.user))
  })
  afterEach(() => { controller?.destroy(); controller = undefined; vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
  function create(overrides: any = {}) {
    controller = createGuideController({ getState: () => state, getPublicSettings: () => ({ payment_enabled: false }), driverFactory: fakeDriver as any, ...overrides })
    return controller
  }
  it('preserves B when its successful registration arrives before A account cleanup', async () => {
    create(); recordGuideRegistration(state.user!); await settle()
    const first = JSON.parse(sessionStorage.getItem(progressKey)!)
    state.user = { id: 8, role: 'user' }; localStorage.setItem('auth_user', JSON.stringify(state.user))
    recordGuideRegistration(state.user!); await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(controller!.status().note).toBe(''); expect(controller!.status().reason).toBeNull()
    const next = JSON.parse(sessionStorage.getItem(progressKey)!)
    expect(next.userId).toBe(8); expect(next.registrationId).not.toBe(first.registrationId)
    expect(JSON.parse(sessionStorage.getItem(historyKey)!).at(-1)).toMatchObject({ registrationId: first.registrationId, reason: 'cancelled:account-changed' })
  })
  it('adopts a legacy B registration with old suppression and stale A progress after rebuilding the controller', async () => {
    create(); recordGuideRegistration(state.user!); await settle(); controller!.exit(); controller!.destroy()
    state.user = { id: 8, role: 'user' }; localStorage.setItem('auth_user', JSON.stringify(state.user))
    const next = { version: FLOW_VERSION, userId: 8, registrationId: 'b'.repeat(32), expiresAt: Date.now() + 10_000 }
    sessionStorage.setItem(registrationKey, JSON.stringify(next)); sessionStorage.setItem(suppressedKey, '1')
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ path: '/old-route' })))
    create(); await controller!.update(); await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(JSON.parse(sessionStorage.getItem(progressKey)!)).toMatchObject(next)
    expect(sessionStorage.getItem(suppressedKey)).toBeNull()
    controller!.exit(); controller!.destroy(); create(); await controller!.update(); await settle()
    expect(controller!.status().active).toBe(false)
    expect(document.querySelector('[data-s2o-step="welcome"]')).toBeNull()
  })
  it('waits for B auth to match local storage without deleting B through stale A state', async () => {
    create(); await controller!.update()
    const next = { version: FLOW_VERSION, userId: 8, registrationId: 'b'.repeat(32), expiresAt: Date.now() + 10_000 }
    sessionStorage.setItem(registrationKey, JSON.stringify(next))
    localStorage.setItem('auth_user', JSON.stringify({ id: 8, role: 'user' }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user' }))
    await controller!.update()
    expect(JSON.parse(sessionStorage.getItem(registrationKey)!)).toEqual(next)
    expect(controller!.status().active).toBe(false)
    state.user = { id: 8, role: 'user' }; await controller!.update(); await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(JSON.parse(sessionStorage.getItem(progressKey)!)).toMatchObject({ userId: 8 })
  })
  it.each(['launch', 'continue'])('hands a new registration to B after the old pending %s settles without a DOM wakeup', async kind => {
    vi.stubGlobal('MutationObserver', class { observe() {} disconnect() {} takeRecords() { return [] } })
    let pending: Promise<unknown>
    if (kind === 'launch') {
      state.path = '/custom/onboarding-quick-start'; history.replaceState({}, '', state.path)
      sessionStorage.setItem(progressKey, JSON.stringify(progress({ moduleId: 'quick-start', step: 'quick-test', path: state.path })))
      create({ getPublicSettings: () => ({ custom_menu_items: [{ id: 'onboarding-quick-start', visibility: 'user' }] }) })
      pending = controller!.update(); await flushPromises()
    } else {
      document.querySelector('[role="dialog"]')!.remove()
      create({ config: normalizeConfig({ schemaVersion: 1, modules: [{ id: 'more', enabled: true }, { id: 'keys', enabled: true }] }) })
      recordGuideRegistration(state.user!); await settle()
      await nextGuideStep(); await nextGuideStep(); await nextGuideStep()
      expect(controller!.status().paused).toBe(true)
      document.querySelector('[data-tour="keys-create-btn"]')!.remove()
      pending = controller!.continue(); await flushPromises()
    }
    expect(controller!.status().launching).toBe(true)
    state.user = { id: 8, role: 'user' }; localStorage.setItem('auth_user', JSON.stringify(state.user))
    recordGuideRegistration(state.user!)
    await pending; await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(controller!.status().launching).toBe(false)
    expect(JSON.parse(sessionStorage.getItem(progressKey)!)).toMatchObject({ userId: 8 })
    expect(document.querySelectorAll('[data-s2o-step="welcome"]')).toHaveLength(1)
  })
  it('does not reopen an accepted registration when removing its record fails and the user exits', async () => {
    create()
    const remove = Storage.prototype.removeItem
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, key: string) {
      if (key === registrationKey) throw new DOMException('Denied')
      return remove.call(this, key)
    })
    recordGuideRegistration(state.user!); await settle()
    expect(controller!.status().step).toBe('welcome'); controller!.exit()
    expect(sessionStorage.getItem(registrationKey)).not.toBeNull()
    for (let i = 0; i < 3; i++) { await controller!.update(); await settle() }
    expect(controller!.status().active).toBe(false)
    expect(document.querySelector('[data-s2o-step="welcome"]')).toBeNull()
    expect(sessionStorage.getItem(suppressedKey)).toBe('1')
  })
  it('retains B registration until its old suppression can be cleared safely', async () => {
    create(); await controller!.replay('more'); controller!.exit()
    state.user = { id: 8, role: 'user' }; localStorage.setItem('auth_user', JSON.stringify(state.user))
    const remove = Storage.prototype.removeItem
    const denied = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(function (this: Storage, key: string) {
      if (key === suppressedKey) throw new DOMException('Denied')
      return remove.call(this, key)
    })
    try {
      recordGuideRegistration(state.user!); await controller!.update(); await settle()
      expect(controller!.status().active).toBe(false)
      expect(JSON.parse(sessionStorage.getItem(registrationKey)!)).toMatchObject({ userId: 8 })
    } finally { denied.mockRestore() }
    await controller!.update(); await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(JSON.parse(sessionStorage.getItem(progressKey)!)).toMatchObject({ userId: 8 })
  })
  it('keeps viewed markers on guide exit and same-account refresh but clears them on account boundaries', async () => {
    create(); await controller!.replay('more'); controller!.exit()
    expect(controller!.status().viewedModules).toEqual(['more'])
    localStorage.setItem('auth_token', 'updated-test-session')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    expect(controller!.status().viewedModules).toEqual(['more'])
    state.user = { id: 8, role: 'user' }; localStorage.setItem('auth_user', JSON.stringify(state.user))
    await controller!.update(); expect(controller!.status().viewedModules).toEqual([])
    await controller!.replay('more'); expect(controller!.status().viewedModules).toEqual(['more'])
    localStorage.removeItem('auth_token'); window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    expect(controller!.status().viewedModules).toEqual([])
    expect(controller!.status().clean).toBe(true)
  })
  it('saves a chapter break without viewing the next chapter and permits immediate continuation', async () => {
    document.body.insertAdjacentHTML('beforeend', '<a data-tour="sidebar-custom-onboarding-quick-start"></a>')
    create({ getPublicSettings: () => ({ custom_menu_items: [{ id: 'onboarding-quick-start', visibility: 'user' }] }) })
    recordGuideRegistration(state.user!); await controller!.update(); await settle()
    await nextGuideStep()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 71 })
    document.querySelector('[role="dialog"]')!.remove(); await settle(); await nextGuideStep()
    expect(controller!.status().paused).toBe(true)
    expect(controller!.status().viewedModules).toEqual(['keys'])
    const saved = JSON.parse(sessionStorage.getItem(progressKey)!)
    expect(saved).toMatchObject({ moduleId: 'key-use', step: 'chapter-break' })
    expect(sessionStorage.getItem(suppressedKey)).toBeNull()
    expect(sessionStorage.getItem(historyKey)).toBeNull()
    document.body.insertAdjacentHTML('beforeend', '<div role="dialog" aria-modal="true"><div class="modal-content"></div></div>')
    expect(await controller!.continue()).toBe(false)
    expect(controller!.status().note).toBe('请先关闭当前窗口，再继续引导。')
    expect(controller!.status().paused).toBe(true)
    document.querySelector('[role="dialog"]')!.remove()
    const first = controller!.continue(), repeated = controller!.continue()
    expect(await repeated).toBe(false); expect(await first).toBe(true)
    await settle()
    expect(controller!.status().step).toBe('usage-row')
    expect(controller!.status().viewedModules).toEqual(['keys', 'key-use'])
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(saved.expiresAt)
    controller!.exit(); expect(controller!.status().viewedModules).toEqual(['keys', 'key-use'])
    expect(controller!.status().clean).toBe(true)
  })
  it('restores a valid registration chapter break without a welcome or next chapter popup', async () => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ moduleId: 'quick-start', step: 'chapter-break' })))
    create(); await controller!.update(); await settle()
    expect(controller!.status().paused).toBe(true)
    expect(controller!.status().viewedModules).toEqual([])
    expect(document.querySelector('.s2o-popover, #s2o-exit')).toBeNull()
    window.dispatchEvent(new Event('pagehide')); expect(controller!.status().clean).toBe(true)
    window.dispatchEvent(new Event('pageshow')); await settle()
    expect(controller!.status().paused).toBe(true)
    localStorage.removeItem('auth_token'); window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    expect(controller!.status().paused).toBe(false)
    expect(await controller!.continue()).toBe(false)
    expect(controller!.status().clean).toBe(true)
  })
  it.each([
    { authenticated: false }, { user: null }, { user: { id: 7, role: 'admin' } },
    { simpleMode: true }, { backendMode: true }, { requiresAuth: false }, { requiresAdmin: true },
    { path: '/login' }, { path: '/register' }, { path: '/email-verify' }, { path: '/auth/wechat/callback' }, { path: '/setup' },
  ])('is unavailable under %j', async override => {
    Object.assign(state, override); create(); await controller!.update()
    expect(controller!.available()).toBe(false)
    expect(await controller!.replay('keys')).toBe(false)
    expect(document.querySelector('.s2o-popover')).toBeNull()
  })
  it('does not invent a signup on login, restored auth or same-account token refresh', async () => {
    create(); await controller!.update(); await settle()
    expect(controller!.status().active).toBe(false)
    localStorage.setItem('auth_token', 'test-session'); localStorage.setItem('auth_user', JSON.stringify(state.user))
    window.dispatchEvent(new StorageEvent('storage', { key: 'refresh_token' })); await settle()
    expect(controller!.status().active).toBe(false)
  })
  it('waits for the user route, then plays every enabled chapter with an unchanging registration deadline', async () => {
    state.path = '/register'; state.requiresAuth = false
    create(); await controller!.update()
    recordGuideRegistration(state.user!)
    const registration = JSON.parse(sessionStorage.getItem(registrationKey)!)
    expect(controller!.status().active).toBe(false)
    state.path = '/keys'; state.requiresAuth = true; await controller!.update(); await settle()
    expect(controller!.status().activeModules).toEqual(['keys', 'key-use', 'quick-start', 'recharge', 'more'])
    expect(sessionStorage.getItem(registrationKey)).toBeNull()
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(registration.expiresAt)
    vi.advanceTimersByTime(1000); await nextGuideStep()
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(registration.expiresAt)
  })
  it('automatically plays the enabled subset after registration', async () => {
    create({ config: normalizeConfig({ schemaVersion: 1, modules: [{ id: 'key-use', enabled: true }, { id: 'recharge', enabled: true }] }) })
    recordGuideRegistration(state.user!); await controller!.update(); await settle()
    expect(controller!.status().activeModules).toEqual(['key-use', 'recharge']); expect(sessionStorage.getItem(registrationKey)).toBeNull()
    expect(controller!.available()).toBe(true)
  })
  it('plays an overview-only registration without false confirmations and still permits manual viewing', async () => {
    const onlyMore = normalizeConfig({ schemaVersion: 1, modules: [{ id: 'more', enabled: true }] })
    create({ config: onlyMore })
    recordGuideRegistration(state.user!); await controller!.update(); await settle()
    expect(controller!.status().step).toBe('welcome')
    expect(sessionStorage.getItem(registrationKey)).toBeNull()
    await nextGuideStep(); expect(controller!.status().step).toBe('more-help')
    await nextGuideStep(); await nextGuideStep(); await nextGuideStep()
    expect(JSON.parse(sessionStorage.getItem(historyKey)!).at(-1)).toMatchObject({ trigger: 'registration', reason: 'completed:tour_viewed' })
    expect(await controller!.replay('more')).toBe(true)
    expect(controller!.status().step).toBe('more-help')
    await nextGuideStep(); await nextGuideStep(); await nextGuideStep()
    const result = JSON.parse(sessionStorage.getItem(historyKey)!).at(-1)
    expect(result).toMatchObject({ trigger: 'manual', reason: 'completed:tour_viewed', result: 'completed' })
    expect(result).not.toHaveProperty('confirmation')
    expect(sessionStorage.getItem(progressKey)).toBeNull()
    expect(controller!.status().note).toBe('已看完本次内容。')
    expect(controller!.status().clean).toBe(true)
    controller!.destroy(); create({ config: onlyMore }); await controller!.update()
    expect(controller!.status().active).toBe(false)
    expect(sessionStorage.getItem(progressKey)).toBeNull()
  })
  it('hides every entry with no enabled chapters, no sidebar marker, or a current opt-out query', async () => {
    create({ config: normalizeConfig({ schemaVersion: 1, modules: [] }) }); expect(controller!.available()).toBe(false); controller!.destroy()
    create(); document.querySelector('[data-tour="sidebar-my-keys"]')!.remove(); expect(controller!.available()).toBe(false)
    keyPage(); history.replaceState({}, '', '/keys?onboarding=off'); expect(controller!.available()).toBe(false)
    expect(await controller!.replay('keys')).toBe(false)
  })
  it('manual replay clears automatic progress, suppresses this visit, and never writes progress', async () => {
    sessionStorage.setItem(progressKey, '{}')
    create(); expect(await controller!.replay('keys')).toBe(true)
    expect(sessionStorage.getItem(progressKey)).toBeNull(); expect(sessionStorage.getItem(suppressedKey)).toBe('1')
    await nextGuideStep(); expect(sessionStorage.getItem(progressKey)).toBeNull()
    controller!.exit(); expect(controller!.status().clean).toBe(true)
    expect(await controller!.replay('keys')).toBe(true) // Manual remains available after suppression.
  })
  it('continues on same-account storage writes, but stops on logout, changed identity or unparseable user data', async () => {
    create(); await controller!.replay('keys')
    localStorage.setItem('auth_token', 'test-session'); localStorage.setItem('auth_user', JSON.stringify(state.user))
    for (const key of ['auth_user', 'auth_token', 'refresh_token']) window.dispatchEvent(new StorageEvent('storage', { key }))
    await settle(); expect(controller!.status().active).toBe(true)
    localStorage.setItem('auth_user', '{broken'); window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user' }))
    expect(controller!.status().reason).toBe('cancelled:account-changed'); expect(controller!.status().clean).toBe(true)
    expect(await controller!.replay('keys')).toBe(false)
    localStorage.setItem('auth_user', JSON.stringify(state.user))
    await controller!.replay('keys')
    localStorage.setItem('auth_user', JSON.stringify({ id: 8, role: 'user' })); window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user' }))
    expect(controller!.status().active).toBe(false)
    expect(await controller!.replay('keys')).toBe(false)
    localStorage.setItem('auth_user', JSON.stringify(state.user))
    await controller!.replay('keys')
    localStorage.removeItem('auth_token'); window.dispatchEvent(new StorageEvent('storage', { key: null }))
    expect(controller!.status().active).toBe(false)
    expect(await controller!.replay('keys')).toBe(false)
  })
  it.each(['logout', 'id', 'role', 'malformed', 'storage-unavailable'])('cannot replay with stale in-memory auth after %s in another tab', async change => {
    create(); expect(await controller!.replay('keys')).toBe(true)
    if (change === 'logout') localStorage.removeItem('auth_token')
    if (change === 'id') localStorage.setItem('auth_user', JSON.stringify({ id: 8, role: 'user' }))
    if (change === 'role') localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin' }))
    if (change === 'malformed') localStorage.setItem('auth_user', '{bad')
    if (change === 'storage-unavailable') vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Storage unavailable') })
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user' }))
    expect(controller!.status().active).toBe(false)
    expect(controller!.available()).toBe(false)
    expect(await controller!.replay('keys')).toBe(false)
    expect(document.querySelector('.s2o-popover')).toBeNull()
  })
  it.each([{ user: { id: 8, role: 'user' } }, { user: { id: 7, role: 'admin' } }, { authenticated: false }, { simpleMode: true }, { backendMode: true }])('stops when account eligibility changes %j', async override => {
    create(); await controller!.update(); await controller!.replay('keys')
    Object.assign(state, override); await controller!.update()
    expect(controller!.status().reason).toBe('cancelled:account-changed'); expect(controller!.status().clean).toBe(true)
  })
  it('rejects a successful result immediately after an account switch, before the host watcher runs', async () => {
    create(); await controller!.replay('keys'); await nextGuideStep()
    expect(controller!.status().step).toBe('name')
    const ticket = beginGuideAction('key-create')
    state.user = { id: 8, role: 'user' }
    completeGuideAction(ticket, { keyId: 72 })
    expect(controller!.status().step).toBe('name')
    await settle()
    expect(controller!.status().step).not.toBe('review')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('密钥已创建')
  })
  function progress(overrides: Record<string, unknown> = {}) {
    return { version: FLOW_VERSION, modules: ['keys', 'key-use', 'quick-start', 'recharge', 'more'], configuration: JSON.stringify(config.modules), moduleId: 'keys', registrationId: 'a'.repeat(32), userId: 7, expiresAt: Date.now() + 10_000, step: 'create', path: '/keys', ...overrides }
  }
  it('restores a pending chapter with a fresh five-second wait without extending registration expiry', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    const saved = progress({ moduleId: 'more', step: 'chapter-break' })
    sessionStorage.setItem(progressKey, JSON.stringify(saved))
    create(); await controller!.update(); await settle()
    expect(controller!.status().paused).toBe(true); expect(controller!.status().viewedModules).toEqual([])
    await vi.advanceTimersByTimeAsync(4700)
    expect(controller!.status().paused).toBe(true)
    await vi.advanceTimersByTimeAsync(300)
    expect(controller!.status().step).toBe('more-help')
    expect(controller!.status().viewedModules).toEqual(['more'])
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(saved.expiresAt)
  })
  it('blocks an automatic transition once while a native dialog is open and allows a later manual retry', async () => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ moduleId: 'more', step: 'chapter-break' })))
    create(); await controller!.update(); await settle()
    await vi.advanceTimersByTimeAsync(6000)
    expect(controller!.status().paused).toBe(true)
    expect(controller!.status().note).toBe('请先关闭当前窗口，再继续引导。')
    document.querySelector('[role="dialog"]')!.remove()
    await vi.advanceTimersByTimeAsync(6000)
    expect(controller!.status().paused).toBe(true)
    expect(await controller!.continue()).toBe(true)
    expect(controller!.status().step).toBe('more-help')
  })
  it.each(['exit', 'destroy', 'logout', 'pagehide'])('cancels a pending automatic transition on %s', async action => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ moduleId: 'more', step: 'chapter-break' })))
    create(); await controller!.update(); await settle()
    if (action === 'exit') controller!.exit()
    if (action === 'destroy') controller!.destroy()
    if (action === 'logout') { localStorage.removeItem('auth_token'); window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' })) }
    if (action === 'pagehide') window.dispatchEvent(new Event('pagehide'))
    await vi.advanceTimersByTimeAsync(6000)
    expect(controller!.status().active).toBe(false); expect(controller!.status().clean).toBe(true)
    expect(document.querySelector('.s2o-popover')).toBeNull()
  })
  it.each([
    ['key-use', 'usage-pick', 'usage-row'], ['recharge', 'balance', 'balance'],
    ['more', 'more-help', 'more-help'], ['more', 'more-tools', 'more-tools'], ['more', 'finish', 'finish'],
    ['more', 'balance', 'more-help'],
  ])('restores %s checkpoint %s to %s without marking earlier chapters', async (moduleId, step, expected) => {
    document.querySelector('[role="dialog"]')!.remove()
    document.body.insertAdjacentHTML('beforeend', '<div data-tour="header-balance"></div>')
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ moduleId, step })))
    create(); await controller!.update(); await settle()
    expect(controller!.status().step).toBe(expected)
    expect(controller!.status().viewedModules).toEqual([moduleId])
  })
  it('discards the previous two-chapter checkpoint without inventing a new registration', async () => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress({ modules: ['keys', 'quick-start'] })))
    create(); await controller!.update(); await settle()
    expect(controller!.status().active).toBe(false)
    expect(sessionStorage.getItem(progressKey)).toBeNull()
    expect(controller!.status().viewedModules).toEqual([])
  })
  it('marks only the resumed chapter after an active registration checkpoint is restored and exited', async () => {
    document.body.insertAdjacentHTML('beforeend', '<a data-tour="sidebar-custom-onboarding-quick-start"></a>')
    const saved = progress({ moduleId: 'quick-start', step: 'quick-nav' })
    sessionStorage.setItem(progressKey, JSON.stringify(saved))
    create({ getPublicSettings: () => ({ custom_menu_items: [{ id: 'onboarding-quick-start', visibility: 'user' }] }) })
    await controller!.update(); await settle()
    expect(controller!.status().step).toBe('quick-nav')
    expect(controller!.status().viewedModules).toEqual(['quick-start'])
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(saved.expiresAt)
    controller!.exit()
    expect(controller!.status().viewedModules).toEqual(['quick-start'])
    expect(controller!.status().clean).toBe(true)
  })
  it('preserves the resumed checkpoint while its real page target is still loading', async () => {
    state.path = '/custom/onboarding-quick-start'; history.replaceState({}, '', state.path)
    const saved = progress({ moduleId: 'quick-start', step: 'quick-test', path: state.path })
    sessionStorage.setItem(progressKey, JSON.stringify(saved))
    const observed: any[] = []
    const viewedDuringWelcome: any[] = []
    create({
      getPublicSettings: () => ({ custom_menu_items: [{ id: 'onboarding-quick-start', visibility: 'user' }] }),
      onChange: () => {
        if (controller?.status().step === 'welcome') {
          observed.push(JSON.parse(sessionStorage.getItem(progressKey) || 'null'))
          viewedDuringWelcome.push(controller.status().viewedModules)
        }
      },
    })
    const starting = controller!.update()
    await flushPromises()
    try {
      expect(observed.length).toBeGreaterThan(0)
      for (const checkpoint of observed) expect(checkpoint).toMatchObject({ moduleId: 'quick-start', step: 'quick-test', expiresAt: saved.expiresAt })
      for (const viewed of viewedDuringWelcome) expect(viewed).toEqual([])
    } finally { controller!.destroy(); await starting }
  })
  it('resumes only unexpired signup progress and records mismatched routes as cancellation', async () => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress()))
    create(); await controller!.update(); expect(controller!.status().active).toBe(true)
    window.dispatchEvent(new Event('pagehide')); expect(controller!.status().active).toBe(false)
    expect(sessionStorage.getItem(progressKey)).not.toBeNull()
    controller!.destroy(); sessionStorage.setItem(progressKey, JSON.stringify(progress({ path: '/custom/other', step: 'review' })))
    create(); await controller!.update(); expect(controller!.status().reason).toBe('cancelled:route-changed')
    expect(sessionStorage.getItem(progressKey)).toBeNull()
  })
  it.each([{ expiresAt: 0 }, { userId: 8 }, { version: 'old-version' }, { configuration: 'old-config' }, { step: 'missing' }])('discards incompatible progress without starting %j', async override => {
    sessionStorage.setItem(progressKey, JSON.stringify(progress(override)))
    create(); await controller!.update()
    expect(controller!.status().active).toBe(false); expect(sessionStorage.getItem(progressKey)).toBeNull()
  })
  it('reserves a session synchronously while awaiting a missing target and releases only after cleanup', async () => {
    create(); await controller!.replay('keys')
    expect(await controller!.replay('keys')).toBe(false)
    document.querySelector('#key-form')!.remove()
    await nextGuideStep()
    expect(await controller!.replay('recharge')).toBe(false)
    controller!.exit(); expect(controller!.status().clean).toBe(true)
  })
})
