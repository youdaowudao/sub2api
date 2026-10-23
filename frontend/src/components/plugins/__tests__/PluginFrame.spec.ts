import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import PluginFrame from '../PluginFrame.vue'
import type { PluginEntry } from '@/api/plugins'
import { createBridgeDriver } from './bridgeDriver'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({
  createSession: vi.fn(), call: vi.fn(), action: vi.fn(), push: vi.fn(),
  run: vi.fn((action: () => Promise<unknown>) => action()), cancel: vi.fn(),
  error: vi.fn(), success: vi.fn(), info: vi.fn(),
}))
const route = reactive({ path: '/keys', fullPath: '/keys?secret=hidden#fragment', name: 'Keys' })
const auth = reactive({ token: 'private-auth-token', isAdmin: false, user: { id: 42, role: 'user', email: 'private@example.test', status: 'active' } })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success, showInfo: mocks.info }) }))
vi.mock('@/api/plugins', () => ({ default: { createSession: mocks.createSession, call: mocks.call, action: mocks.action } }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: mocks.run, onCancel: mocks.cancel }),
  isStepUpBlocked: () => false, isStepUpCancelled: (error: { code?: string }) => error?.code === 'STEP_UP_CANCELLED', stepUpBlockReason: () => '',
}))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const entry: PluginEntry = {
  plugin_id: 7, plugin_key: 'example.combined', revision: 'package-a', entry_id: 'home', label: 'Example', mount: 'user', roles: ['user', 'admin'],
  methods: [{ name: 'read', role: 'user', access: 'read' }, { name: 'write', role: 'user', access: 'write' }],
}

async function mountFrame(mountKind: PluginEntry['mount'] = 'user', anchors?: { navigation: HTMLElement; content: HTMLElement }, initialLoad = true) {
  const current = { ...entry, mount: mountKind }
  mocks.createSession.mockResolvedValue({ url: '/api/v1/plugin-ui/resource-token/index.html#bridge_token=bridge', bridge_token: 'bridge', ui_bridge_version: 2, expires_at: '', entry: current })
  const wrapper = mount(PluginFrame, { props: { entry: current, anchors }, attachTo: document.body, global: { stubs: { TotpStepUpDialog: true } } })
  await flushPromises()
  const frame = wrapper.get('iframe')
  const { post, message } = createBridgeDriver(frame.element as HTMLIFrameElement, 'bridge')
  if (initialLoad) {
    await frame.trigger('load')
    post.mockClear()
  }
  return { wrapper, frame, post, message }
}

describe('business plugin frame', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    route.path = '/keys'; route.fullPath = '/keys?secret=hidden#fragment'; route.name = 'Keys'
    auth.token = 'private-auth-token'; auth.isAdmin = false; auth.user.role = 'user'
    mocks.call.mockResolvedValue({ request_id: 'host-read', payload: { value: 1 } })
    mocks.action.mockResolvedValue({ request_id: 'host-write', payload: { saved: true } })
    mocks.run.mockImplementation(action => action())
  })

  it('uses trusted session methods, ignores caller/access claims, and gates writes', async () => {
    const { wrapper, message, post } = await mountFrame()
    expect(wrapper.get('iframe').attributes('sandbox')).toBe('allow-scripts')
    expect(wrapper.get('iframe').attributes('src')).not.toContain(auth.token)
    message('plugin.call', 'read-id', { method: 'read', payload: { test: 1 }, access: 'write', user_id: 1, role: 'admin', plugin_id: 99 })
    message('plugin.call', 'write-id', { method: 'write', payload: {} })
    message('plugin.call', 'unknown-id', { method: 'other', payload: {} })
    message('config.load', 'config-id')
    await flushPromises()
    expect(mocks.call).toHaveBeenCalledTimes(1)
    expect(mocks.call).toHaveBeenCalledWith(7, 'read', { test: 1 }, expect.any(AbortSignal))
    expect(mocks.action).toHaveBeenCalledTimes(1)
    expect(mocks.action).toHaveBeenCalledWith(7, 'write', {}, expect.any(AbortSignal))
    expect(mocks.run).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'unknown-id', ok: false, error: expect.objectContaining({ code: 'PLUGIN_INVALID_REQUEST' }) }))
    expect(post.mock.calls.some(([data]) => data.request_id === 'config-id')).toBe(false)
  })

  it('only returns the limited context and publishes it when the route changes', async () => {
    const { message, post } = await mountFrame()
    message('context.get', 'context-id')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ context: { path: '/keys', route_name: 'Keys', user_id: 42, role: 'user' } }))
    route.path = '/profile'; route.fullPath = '/profile?hidden=1#secret'; route.name = 'Profile'
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'context.changed', context: { path: '/profile', route_name: 'Profile', user_id: 42, role: 'user' } }))
    expect(JSON.stringify(post.mock.calls)).not.toContain('private@')
    expect(JSON.stringify(post.mock.calls)).not.toContain('private-auth')
    expect(JSON.stringify(post.mock.calls)).not.toContain('hidden=')
  })

  it('rejects another window, a non-opaque origin, a wrong token and missing IDs', async () => {
    const { message, post } = await mountFrame()
    message('plugin.call', 'origin', { method: 'read', payload: {} }, { origin: window.location.origin })
    message('plugin.call', 'source', { method: 'read', payload: {} }, { source: window })
    message('plugin.call', 'token', { method: 'read', payload: {}, bridge_token: 'wrong' })
    message('plugin.call', '', { method: 'read', payload: {} })
    await flushPromises()
    expect(mocks.call).not.toHaveBeenCalled()
    expect(post).not.toHaveBeenCalled()
  })

  it('never sends an old result to a reloaded document reusing the same request ID', async () => {
    let resolveOld!: (value: unknown) => void
    let resolveNew!: (value: unknown) => void
    mocks.call.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockReturnValueOnce(new Promise(resolve => { resolveNew = resolve }))
    const { frame, message, post } = await mountFrame()
    message('plugin.call', 'same', { method: 'read', payload: {} })
    const oldSignal = mocks.call.mock.calls[0][3] as AbortSignal
    await frame.trigger('load')
    post.mockClear()
    expect(oldSignal.aborted).toBe(true)
    message('plugin.call', 'same', { method: 'read', payload: {} })
    resolveOld({ request_id: 'old', payload: { secret: 'obsolete' } })
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    resolveNew({ request_id: 'new', payload: { current: true } })
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ result: { request_id: 'new', payload: { current: true } } }))
  })

  it.each(['user', 'shell'] as const)('keeps the %s document and its pending call when polling returns the same entry', async mountKind => {
    let resolve!: (value: unknown) => void
    mocks.call.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const { wrapper, frame, message, post } = await mountFrame(mountKind)
    const originalFrame = frame.element
    message('plugin.call', 'across-poll', { method: 'read', payload: {} })
    const signal = mocks.call.mock.calls[0][3] as AbortSignal
    const refreshed = JSON.parse(JSON.stringify(wrapper.props('entry'))) as PluginEntry
    await wrapper.setProps({ entry: refreshed })
    await flushPromises()
    expect(mocks.createSession).toHaveBeenCalledTimes(1)
    expect(wrapper.get('iframe').element).toBe(originalFrame)
    expect(signal.aborted).toBe(false)
    resolve({ request_id: 'retained', payload: { finished: true } })
    await flushPromises()
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'across-poll', ok: true, result: { request_id: 'retained', payload: { finished: true } } }))
  })

  it('preserves initialization requests sent before the first document load', async () => {
    let resolve!: (value: unknown) => void
    mocks.call.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const { frame, message, post } = await mountFrame('user', undefined, false)
    message('plugin.call', 'initialize', { method: 'read', payload: {} })
    expect(mocks.call).not.toHaveBeenCalled()
    await frame.trigger('load')
    await flushPromises()
    const signal = mocks.call.mock.calls[0][3] as AbortSignal
    expect(signal.aborted).toBe(false)
    resolve({ request_id: 'host-init', payload: { initialized: true } })
    await flushPromises()
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ type: 'context.changed' }))
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'initialize', ok: true, result: { request_id: 'host-init', payload: { initialized: true } } }))
  })

  it.each(['user', 'shell'] as const)('cancels the old %s frame on token renewal without reporting a permission failure', async mountKind => {
    let finish!: (value: unknown) => void
    mocks.call.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { wrapper, message, post } = await mountFrame(mountKind)
    message('plugin.call', 'before-renewal', { method: 'read', payload: {} })
    await flushPromises()
    const signal = mocks.call.mock.calls[0][3] as AbortSignal
    auth.token = 'renewed-private-auth-token'
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.emitted('unavailable')).toBeUndefined()
    finish({ request_id: 'obsolete', payload: { stale: true } })
    await flushPromises()
    expect(post.mock.calls.some(([data]) => data.request_id === 'before-renewal')).toBe(false)
    message('plugin.call', 'after-renewal', { method: 'read', payload: {} })
    await flushPromises()
    expect(mocks.call).toHaveBeenCalledTimes(1)
  })

  it('does not execute a pending write after route cancellation or unmount', async () => {
    let continueStepUp!: () => void
    mocks.run.mockImplementation(action => new Promise(resolve => { continueStepUp = () => { Promise.resolve().then(action).then(resolve, resolve) } }))
    const { wrapper, message, post } = await mountFrame()
    message('plugin.call', 'write', { method: 'write', payload: {} })
    route.fullPath = '/profile'
    await flushPromises()
    continueStepUp()
    await flushPromises()
    expect(mocks.action).not.toHaveBeenCalled()
    expect(post.mock.calls.some(([data]) => data.request_id === 'write')).toBe(false)
    wrapper.unmount()
    message('plugin.call', 'after', { method: 'read', payload: {} })
    await flushPromises()
    expect(mocks.call).not.toHaveBeenCalled()
  })

  it('reports step-up cancellation without running a business write', async () => {
    mocks.run.mockRejectedValue({ code: 'STEP_UP_CANCELLED' })
    const { message, post } = await mountFrame()
    message('plugin.call', 'cancel', { method: 'write', payload: {} })
    await flushPromises()
    expect(mocks.action).not.toHaveBeenCalled()
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ ok: false, error: { code: 'CANCELLED', message: 'common.cancel', retryable: false, outcome_unknown: false } }))
  })

  it('restricts shell navigation and anchors, then clears the outline on route change and close', async () => {
    const element = document.createElement('div')
    vi.spyOn(element, 'getBoundingClientRect').mockReturnValue({ x: -10, y: 20, right: 200, bottom: 300, width: 210, height: 280 } as DOMRect)
    const { wrapper, message, post } = await mountFrame('shell', { navigation: element, content: element })
    message('ui.navigate', 'bad-url', { target: 'https://example.test' })
    message('ui.navigate', 'bad-path', { target: '/keys' })
    message('ui.anchor', 'bad-anchor', { target: '#private' })
    message('ui.anchor', 'anchor', { target: 'navigation' })
    await flushPromises()
    expect(mocks.push).not.toHaveBeenCalled()
    expect(document.querySelector('[data-plugin-anchor-outline]')).not.toBeNull()
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ rect: { x: 0, y: 20, width: 200, height: 280 } }))
    route.fullPath = '/profile'; route.path = '/profile'
    await flushPromises()
    expect(document.querySelector('[data-plugin-anchor-outline]')).toBeNull()
    message('ui.navigate', 'keys', { target: 'keys' })
    await flushPromises()
    expect(mocks.push).toHaveBeenCalledTimes(1)
    expect(mocks.push).toHaveBeenCalledWith('/keys')
    message('ui.close', 'close')
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'ui.close.result', ok: true }))
  })

  it('does not expose shell messages on an ordinary business page and clamps height', async () => {
    const { wrapper, message, post } = await mountFrame()
    message('ui.close', 'close')
    message('ui.navigate', 'nav', { target: 'keys' })
    message('ui.resize', 'resize', { height: 1 })
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(mocks.push).not.toHaveBeenCalled()
    expect(wrapper.attributes('style')).toContain('height: 160px')
    expect(post.mock.calls.some(([data]) => data.request_id === 'close' || data.request_id === 'nav')).toBe(false)
  })

  it('limits concurrent business requests, rejects duplicate IDs, and cancels all timers on unmount', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      mocks.call.mockImplementation(() => new Promise(() => {}))
      const { wrapper, message, post } = await mountFrame()
      for (let index = 0; index < 33; index++) message('plugin.call', `pending-${index}`, { method: 'read', payload: {} })
      message('plugin.call', 'pending-0', { method: 'read', payload: {} })
      expect(mocks.call).toHaveBeenCalledTimes(32)
      expect(vi.getTimerCount()).toBe(32)
      const signal = mocks.call.mock.calls[0][3] as AbortSignal
      await vi.advanceTimersByTimeAsync(30_000)
      expect(signal.aborted).toBe(true)
      expect(post).toHaveBeenCalledTimes(32)
      message('plugin.call', 'pending-0', { method: 'read', payload: {} })
      expect(mocks.call).toHaveBeenCalledTimes(33)
      wrapper.unmount()
      expect(vi.getTimerCount()).toBe(0)
    } finally { vi.useRealTimers() }
  })
})
