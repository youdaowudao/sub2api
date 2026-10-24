import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createGuideController, type GuideState } from '../controller'
import config from '../config/onboarding.json'
import { normalizeConfig } from '../config'
import { progressKey, recordGuideRegistration, registrationKey, suppressedKey } from '../registration'
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
  it('waits for the user route, then plays only the two registration chapters with an unchanging deadline', async () => {
    state.path = '/register'; state.requiresAuth = false
    create(); await controller!.update()
    recordGuideRegistration(state.user!)
    const registration = JSON.parse(sessionStorage.getItem(registrationKey)!)
    expect(controller!.status().active).toBe(false)
    state.path = '/keys'; state.requiresAuth = true; await controller!.update(); await settle()
    expect(controller!.status().activeModules).toEqual(['keys', 'quick-start'])
    expect(sessionStorage.getItem(registrationKey)).toBeNull()
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(registration.expiresAt)
    vi.advanceTimersByTime(1000); await nextGuideStep()
    expect(JSON.parse(sessionStorage.getItem(progressKey)!).expiresAt).toBe(registration.expiresAt)
  })
  it('does not automatically play when only usage and recharge are enabled', async () => {
    create({ config: normalizeConfig({ schemaVersion: 1, modules: [{ id: 'key-use', enabled: true }, { id: 'recharge', enabled: true }] }) })
    recordGuideRegistration(state.user!); await controller!.update(); await settle()
    expect(controller!.status().active).toBe(false); expect(sessionStorage.getItem(registrationKey)).toBeNull()
    expect(controller!.available()).toBe(true)
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
    return { version: FLOW_VERSION, modules: ['keys', 'quick-start'], configuration: JSON.stringify(config.modules), moduleId: 'keys', registrationId: 'a'.repeat(32), userId: 7, expiresAt: Date.now() + 10_000, step: 'create', path: '/keys', ...overrides }
  }
  it('preserves the resumed checkpoint while its real page target is still loading', async () => {
    state.path = '/custom/onboarding-quick-start'; history.replaceState({}, '', state.path)
    const saved = progress({ moduleId: 'quick-start', step: 'quick-test', path: state.path })
    sessionStorage.setItem(progressKey, JSON.stringify(saved))
    const observed: any[] = []
    create({
      getPublicSettings: () => ({ custom_menu_items: [{ id: 'onboarding-quick-start', visibility: 'user' }] }),
      onChange: () => {
        if (controller?.status().step === 'welcome') observed.push(JSON.parse(sessionStorage.getItem(progressKey) || 'null'))
      },
    })
    const starting = controller!.update()
    await flushPromises()
    try {
      expect(observed.length).toBeGreaterThan(0)
      for (const checkpoint of observed) expect(checkpoint).toMatchObject({ moduleId: 'quick-start', step: 'quick-test', expiresAt: saved.expiresAt })
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
