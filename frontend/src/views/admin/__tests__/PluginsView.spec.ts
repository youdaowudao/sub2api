import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'

import PluginsView from '../PluginsView.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PluginUISession } from '@/api/admin'
import { createBridgeDriver } from '@/components/plugins/__tests__/bridgeDriver'

enableAutoUnmount(afterEach)

const {
  listPlugins,
  uploadPlugin,
  enablePlugin,
  savePluginConfig,
  createUISession,
  getPluginStatus,
  getPluginConfig,
  stepUpRun,
  stepUpCancel,
  testPlugin,
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  savePluginConfig: vi.fn(),
  createUISession: vi.fn(),
  getPluginStatus: vi.fn(),
  getPluginConfig: vi.fn().mockResolvedValue({}),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
  stepUpCancel: vi.fn(),
  testPlugin: vi.fn().mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: getPluginConfig,
      saveConfig: savePluginConfig,
      test: testPlugin,
      createUISession,
      status: getPluginStatus,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

const auth = reactive({ token: 'fixture-admin', user: { id: 1, role: 'admin', status: 'active' } })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun, onCancel: stepUpCancel }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: (error: { code?: string }) => error?.code === 'STEP_UP_CANCELLED',
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      rollout_percent: 100,
    },
  ],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Icon: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.token = 'fixture-admin'
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    getPluginConfig.mockResolvedValue({})
    testPlugin.mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 })
    listPlugins.mockResolvedValue([plugin])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, 100, false)
  })

  it.each([7, 8])('keeps the latest session when reopening configuration for plugin %s', async (id) => {
    let resolveOld!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveOld = resolve }))
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.findAll('button').filter(item => item.text() === 'admin.plugins.configure')
    await buttons[0].trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await buttons[id === 7 ? 0 : 1].trigger('click')
    await flushPromises()
    const currentURL = wrapper.get('iframe').attributes('src')

    resolveOld({ url: '/obsolete-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe(currentURL)
  })

  it('ignores an old session error while the reopened configuration is loading', async () => {
    let rejectOld!: (error: Error) => void
    let resolveCurrent!: (session: PluginUISession) => void
    createUISession
      .mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
      .mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveCurrent = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const button = wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!
    await button.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await button.trigger('click')
    rejectOld(new Error('obsolete failure'))
    await flushPromises()

    expect(wrapper.text()).not.toContain('obsolete failure')
    expect(wrapper.text()).toContain('admin.plugins.loadingUI')
    resolveCurrent({ url: '/current-session', bridge_token: 'current', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    await wrapper.get('iframe').trigger('load')
    expect(wrapper.get('iframe').attributes('src')).toBe('/current-session')
    expect(wrapper.text()).not.toContain('admin.plugins.loadingUI')
  })

  it('does not restore a session after the configuration dialog closes', async () => {
    let resolveSession!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveSession = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    resolveSession({ url: '/closed-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

  it('上传插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], 'transport.s2plugin', { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })

  it('does not execute a 33rd concurrent Bridge request', async () => {
    getPluginStatus.mockImplementation(() => new Promise(() => {}))
    const wrapper = mountView()
    document.body.appendChild(wrapper.element)
    try {
      await flushPromises()
      await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
      await flushPromises()
      const frame = wrapper.get('iframe').element as HTMLIFrameElement
      expect(frame.contentWindow).not.toBeNull()
      for (let index = 0; index < 33; index++) {
        window.dispatchEvent(new MessageEvent('message', {
          origin: 'null',
          source: frame.contentWindow,
          data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'plugin.status', request_id: `pending-${index}` },
        }))
      }
      await flushPromises()
      expect(getPluginStatus).toHaveBeenCalledTimes(32)
    } finally {
      wrapper.unmount()
      wrapper.element.remove()
    }
  })

  it('drops an old configuration result when a reloaded document reuses its request ID', async () => {
    let resolveOld!: (value: unknown) => void
    let resolveNew!: (value: unknown) => void
    getPluginStatus
      .mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
      .mockReturnValueOnce(new Promise(resolve => { resolveNew = resolve }))
    const wrapper = mountView()
    document.body.appendChild(wrapper.element)
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    await flushPromises()
    const iframe = wrapper.get('iframe')
    const frame = iframe.element as HTMLIFrameElement
    const post = vi.spyOn(frame.contentWindow!, 'postMessage')
    const request = () => window.dispatchEvent(new MessageEvent('message', { origin: 'null', source: frame.contentWindow, data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'plugin.status', request_id: 'same-id' } }))
    await iframe.trigger('load')
    request()
    const oldSignal = getPluginStatus.mock.calls[0][1] as AbortSignal
    await iframe.trigger('load')
    request()
    expect(oldSignal.aborted).toBe(true)
    resolveOld({ healthy: true, message: 'obsolete' })
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    resolveNew({ healthy: true, message: 'current' })
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ result: { healthy: true, message: 'current' } }), '*')
    wrapper.unmount()
    wrapper.element.remove()
  })

  it('keeps configuration initialization requests made before the first frame load', async () => {
    let resolve!: (value: unknown) => void
    getPluginStatus.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = mountView()
    document.body.appendChild(wrapper.element)
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    await flushPromises()
    const iframe = wrapper.get('iframe')
    const frame = iframe.element as HTMLIFrameElement
    const post = vi.spyOn(frame.contentWindow!, 'postMessage')
    window.dispatchEvent(new MessageEvent('message', { origin: 'null', source: frame.contentWindow, data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'plugin.status', request_id: 'initialize' } }))
    const signal = getPluginStatus.mock.calls[0][1] as AbortSignal
    await iframe.trigger('load')
    expect(signal.aborted).toBe(false)
    resolve({ healthy: true, message: 'initialized' })
    await flushPromises()
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'initialize', result: { healthy: true, message: 'initialized' } }), '*')
    wrapper.unmount()
    wrapper.element.remove()
  })

  it('edits backend-only configuration as an object without an iframe or rollout control', async () => {
    getPluginConfig.mockResolvedValue({ existing: true })
    listPlugins.mockResolvedValue([{ ...plugin, state: 'enabled', bindings: [], manifest: { ...plugin.manifest, schema_version: 2, ui: {}, runtimes: { 'linux-amd64': { path: 'bin/backend' } } } }])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('input[type="range"]').exists()).toBe(false)
    expect(wrapper.findAll('button').some(item => item.text() === 'admin.plugins.disable')).toBe(true)
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(createUISession).not.toHaveBeenCalled()
    const textarea = wrapper.get('textarea')
    expect(JSON.parse((textarea.element as HTMLTextAreaElement).value)).toEqual({ existing: true })
    await textarea.setValue('[]')
    await wrapper.findAll('button').find(item => item.text() === 'common.save')!.trigger('click')
    expect(wrapper.text()).toContain('admin.plugins.jsonObjectRequired')
    expect(savePluginConfig).not.toHaveBeenCalled()
    savePluginConfig.mockRejectedValueOnce(new Error('validation failed'))
    await textarea.setValue('{"changed":true}')
    await wrapper.findAll('button').find(item => item.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(savePluginConfig).toHaveBeenCalledWith(7, { changed: true }, expect.any(AbortSignal))
    expect((textarea.element as HTMLTextAreaElement).value).toBe('{"changed":true}')
    expect(wrapper.text()).toContain('validation failed')
  })

  it('keeps JSON editing and saving disabled until the existing configuration is loaded', async () => {
    listPlugins.mockResolvedValue([{ ...plugin, manifest: { ...plugin.manifest, schema_version: 2, ui: {} } }])
    getPluginConfig.mockRejectedValueOnce(new Error('configuration unavailable'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('textarea').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').find(item => item.text() === 'common.save')!.attributes('disabled')).toBeDefined()
    expect(savePluginConfig).not.toHaveBeenCalled()
    getPluginConfig.mockResolvedValueOnce({ previous: 'preserved' })
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.retry')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('textarea').attributes('disabled')).toBeUndefined()
    expect(JSON.parse((wrapper.get('textarea').element as HTMLTextAreaElement).value)).toEqual({ previous: 'preserved' })
  })

  async function mountBridge2(connect = true) {
    listPlugins.mockResolvedValue([{ ...plugin, plugin_key: 'example.generic', bindings: [], manifest: { ...plugin.manifest, id: 'example.generic', schema_version: 2, requires: { sub2api: '>=0.1.0', plugin_protocol: 0, transport_api: 0, backend_api: 0, host_service_api: 0, ui_bridge: 2 }, ui: { entrypoint: 'ui/config.html' } }, compatibility: { ...plugin.compatibility, plugin_protocol: 0, transport_api: 0, ui_bridge: 2 } }])
    createUISession.mockResolvedValue({ url: '/generic-config.html#bridge_token=bridge2', bridge_token: 'bridge2', ui_bridge_version: 2, expires_at: '' })
    const wrapper = mountView()
    document.body.appendChild(wrapper.element)
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    await flushPromises()
    const iframe = wrapper.get('iframe')
    const driver = connect ? createBridgeDriver(iframe.element as HTMLIFrameElement, 'bridge2') : null
    return { wrapper, iframe, driver }
  }

  it('loads Bridge2 configuration only after its actual page handshake and gates saves and tests', async () => {
    getPluginConfig.mockResolvedValue({ previous: 'kept' })
    const { wrapper, iframe, driver } = await mountBridge2()
    driver!.message('config.load', 'early-config')
    expect(getPluginConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.plugins.loadingUI')
    await iframe.trigger('load')
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.plugins.loadingUI')
    expect(getPluginConfig).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(driver!.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'early-config', config: { previous: 'kept' } }))
    let approve!: () => void
    stepUpRun.mockImplementationOnce(action => new Promise(resolve => { approve = () => resolve(action()) }))
    driver!.message('config.save', 'save-config', { config: { changed: true } })
    expect(savePluginConfig).not.toHaveBeenCalled()
    approve()
    await flushPromises()
    expect(savePluginConfig).toHaveBeenCalledWith(7, { changed: true }, expect.any(AbortSignal))
    expect(driver!.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'save-config', ok: true }))
    let approveTest!: () => void
    stepUpRun.mockImplementationOnce(action => new Promise(resolve => { approveTest = () => resolve(action()) }))
    driver!.message('config.test', 'test-config')
    expect(testPlugin).not.toHaveBeenCalled()
    approveTest()
    await flushPromises()
    expect(testPlugin).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(stepUpRun).toHaveBeenCalledTimes(2)
    stepUpRun.mockRejectedValueOnce({ code: 'STEP_UP_CANCELLED' })
    driver!.message('config.save', 'cancelled-config', { config: { forbidden: true } })
    await flushPromises()
    expect(savePluginConfig).toHaveBeenCalledTimes(1)
    expect(driver!.post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'cancelled-config', ok: false, error: 'common.cancel' }))
    wrapper.unmount(); wrapper.element.remove()
  })

  it('does not fall back to window configuration calls or accept business and shell messages on the config port', async () => {
    const { wrapper, iframe, driver } = await mountBridge2()
    await iframe.trigger('load')
    driver!.post.mockClear()
    window.dispatchEvent(new MessageEvent('message', { source: (iframe.element as HTMLIFrameElement).contentWindow, origin: 'null', data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge2', type: 'config.save', request_id: 'window-fallback', config: { forbidden: true } } }))
    for (const type of ['plugin.call', 'context.get', 'ui.close', 'ui.navigate', 'ui.anchor']) driver!.message(type, type, { method: 'echo', payload: {}, target: 'dashboard' })
    await flushPromises()
    expect(getPluginConfig).not.toHaveBeenCalled()
    expect(savePluginConfig).not.toHaveBeenCalled()
    expect(testPlugin).not.toHaveBeenCalled()
    expect(getPluginStatus).not.toHaveBeenCalled()
    expect(stepUpRun).not.toHaveBeenCalled()
    expect(driver!.post).not.toHaveBeenCalled()
    expect(wrapper.find('iframe').exists()).toBe(true)
    wrapper.unmount(); wrapper.element.remove()
  })

  it('revokes Bridge2 configuration reads on reload and keeps same-ID old results out of the new port', async () => {
    let resolveOld!: (value: unknown) => void
    let resolveNew!: (value: unknown) => void
    getPluginConfig.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockReturnValueOnce(new Promise(resolve => { resolveNew = resolve }))
    const { wrapper, iframe, driver } = await mountBridge2()
    await iframe.trigger('load')
    driver!.message('config.load', 'same-id')
    const signal = getPluginConfig.mock.calls[0][1] as AbortSignal
    await iframe.trigger('load')
    driver!.post.mockClear()
    expect(signal.aborted).toBe(true)
    driver!.message('config.load', 'same-id')
    resolveOld({ obsolete: true })
    await flushPromises()
    expect(driver!.post).not.toHaveBeenCalled()
    resolveNew({ current: true })
    await flushPromises()
    expect(driver!.post).toHaveBeenCalledOnce()
    expect(driver!.post).toHaveBeenLastCalledWith(expect.objectContaining({ config: { current: true } }))
    wrapper.unmount(); wrapper.element.remove()
  })

  it.each(['close', 'identity'])('cancels Bridge2 reads and ignores late replies after %s', async reason => {
    let resolve!: (value: unknown) => void
    getPluginConfig.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const { wrapper, iframe, driver } = await mountBridge2()
    await iframe.trigger('load')
    driver!.message('config.load', 'obsolete')
    const signal = getPluginConfig.mock.calls[0][1] as AbortSignal
    driver!.post.mockClear()
    if (reason === 'close') wrapper.getComponent(BaseDialog).vm.$emit('close')
    else auth.token = 'renewed-admin'
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.find('iframe').exists()).toBe(false)
    resolve({ obsolete: true })
    await flushPromises()
    expect(driver!.post).not.toHaveBeenCalled()
    wrapper.unmount(); wrapper.element.remove()
  })

  it.each(['close', 'identity'])('cannot execute a pending Bridge2 write after %s even if verification later succeeds', async reason => {
    let approve!: () => void
    stepUpRun.mockImplementationOnce(action => new Promise((resolve, reject) => { approve = () => { try { resolve(action()) } catch (error) { reject(error) } } }))
    const { wrapper, iframe, driver } = await mountBridge2()
    await iframe.trigger('load')
    driver!.message('config.save', 'waiting-for-proof', { config: { forbidden: true } })
    driver!.post.mockClear()
    stepUpCancel.mockClear()
    if (reason === 'close') wrapper.getComponent(BaseDialog).vm.$emit('close')
    else auth.token = 'renewed-admin'
    await flushPromises()
    expect(stepUpCancel).toHaveBeenCalled()
    approve()
    await flushPromises()
    expect(savePluginConfig).not.toHaveBeenCalled()
    expect(driver!.post).not.toHaveBeenCalled()
    expect(wrapper.find('iframe').exists()).toBe(false)
    wrapper.unmount(); wrapper.element.remove()
  })

  it('removes an unproved Bridge2 configuration document after the bounded load timeout', async () => {
    vi.useFakeTimers()
    try {
      const { wrapper, iframe } = await mountBridge2(false)
      vi.spyOn((iframe.element as HTMLIFrameElement).contentWindow!, 'postMessage').mockImplementation(() => {})
      await iframe.trigger('load')
      await vi.advanceTimersByTimeAsync(30_001)
      expect(wrapper.find('iframe').exists()).toBe(false)
      expect(wrapper.text()).toContain('admin.plugins.pageLoadFailed')
      expect(getPluginConfig).not.toHaveBeenCalled()
      wrapper.unmount(); wrapper.element.remove()
    } finally { vi.useRealTimers() }
  })

  it('still cancels a Bridge2 write waiting for step-up after its 30-second timeout', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      let approve!: () => void
      stepUpRun.mockImplementationOnce(action => new Promise((resolve, reject) => { approve = () => { try { resolve(action()) } catch (error) { reject(error) } } }))
      const { wrapper, iframe, driver } = await mountBridge2()
      await iframe.trigger('load')
      driver!.message('config.save', 'expired-proof', { config: { forbidden: true } })
      stepUpCancel.mockClear()
      await vi.advanceTimersByTimeAsync(30_001)
      expect(stepUpCancel).toHaveBeenCalled()
      approve(); await flushPromises()
      expect(savePluginConfig).not.toHaveBeenCalled()
      expect(driver!.post.mock.calls.some(([data]) => data.request_id === 'expired-proof')).toBe(false)
      wrapper.unmount(); wrapper.element.remove()
    } finally { vi.useRealTimers() }
  })

  it('still aborts an already dispatched Bridge2 configuration save at 30 seconds', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      let finish!: (value: unknown) => void
      savePluginConfig.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      const { wrapper, iframe, driver } = await mountBridge2()
      await iframe.trigger('load')
      driver!.message('config.save', 'expired-save', { config: { changed: true } })
      await flushPromises()
      const signal = savePluginConfig.mock.calls[0][2] as AbortSignal
      await vi.advanceTimersByTimeAsync(30_001)
      expect(signal.aborted).toBe(true)
      finish({ changed: true }); await flushPromises()
      expect(driver!.post.mock.calls.some(([data]) => data.request_id === 'expired-save')).toBe(false)
      wrapper.unmount(); wrapper.element.remove()
    } finally { vi.useRealTimers() }
  })
})
