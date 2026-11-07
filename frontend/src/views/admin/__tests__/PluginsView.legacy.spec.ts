import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, reactive } from 'vue'
import PluginsView from '../PluginsView.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { StepUpController } from '@/composables/useStepUp'

const mocks = vi.hoisted(() => ({ list: vi.fn(), session: vi.fn(), get: vi.fn(), save: vi.fn(), test: vi.fn(), success: vi.fn(), error: vi.fn() }))
const auth = reactive({ token: 'fixture-admin', user: { id: 1, role: 'admin', status: 'active' } })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error, showInfo() {} }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { plugins: { list: mocks.list, createUISession: mocks.session, getConfig: mocks.get, saveConfig: mocks.save, test: mocks.test } } }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

let granted = false
const Prompt = defineComponent({
  props: { controller: { type: Object, required: true } },
  setup(props) {
    return () => {
      const controller = props.controller as StepUpController
      return controller.visible.value ? h('div', { role: 'dialog', 'aria-label': 'Step-up fixture' }, [
        h('button', { onClick: () => { granted = true; controller.onVerified() } }, 'Verify fixture'),
      ]) : null
    }
  },
})
const installation = {
  id: 7, plugin_key: 'example.legacy', name: 'Legacy', version: '1.0.0', state: 'disabled', bindings: [],
  manifest: { schema_version: 1, id: 'example.legacy', capabilities: [], runtimes: {}, ui: { entrypoint: 'ui/index.html' }, requires: { plugin_protocol: 1, transport_api: 1, ui_bridge: 1 } },
  compatibility: { compatible: true, tested: true, status: 'compatible' },
}
let wrapper: VueWrapper | undefined

async function mountLegacy() {
  wrapper = mount(PluginsView, { attachTo: document.body, global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { template: '<div><slot /></div>' }, Icon: true, TotpStepUpDialog: Prompt,
  } } })
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.plugins.configure')!.trigger('click')
  await flushPromises()
  const iframe = wrapper.get('iframe')
  await iframe.trigger('load')
  const target = (iframe.element as HTMLIFrameElement).contentWindow!
  const post = vi.spyOn(target, 'postMessage').mockImplementation(() => {})
  function message(type: string, id = 'request', extra: Record<string, unknown> = {}) {
    window.dispatchEvent(new MessageEvent('message', { source: target, origin: 'null', data: { source: 'sub2api-plugin-ui', bridge_token: 'fixture-legacy', type, request_id: id, ...extra } }))
  }
  return { view: wrapper, iframe, post, message }
}

describe('Bridge1 keeps the original reply-expiration behavior with the real step-up controller', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.clearAllMocks(); granted = false
    mocks.list.mockResolvedValue([installation])
    mocks.session.mockResolvedValue({ url: '/legacy-config-fixture', bridge_token: 'fixture-legacy', ui_bridge_version: 1, expires_at: '' })
    mocks.get.mockResolvedValue({})
    mocks.save.mockImplementation(async (_id, config) => {
      if (!granted) throw { reason: 'STEP_UP_REQUIRED' }
      return config
    })
    mocks.test.mockResolvedValue({ success: true })
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks() })

  it('keeps TOTP open past 30 seconds and can then save without sending an expired reply', async () => {
    const { view, message, post } = await mountLegacy()
    message('config.save', 'slow-verification', { config: { value: 'kept' } })
    await flushPromises()
    expect(view.find('[role="dialog"]').exists()).toBe(true)
    const signal = mocks.save.mock.calls[0][2] as AbortSignal
    await vi.advanceTimersByTimeAsync(30_001)
    expect(view.find('[role="dialog"]').exists()).toBe(true)
    expect(signal.aborted).toBe(false)
    await view.get('[role="dialog"] button').trigger('click'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.save).toHaveBeenLastCalledWith(7, { value: 'kept' }, signal)
    expect(post).not.toHaveBeenCalled()
    expect(mocks.success).toHaveBeenCalledWith('common.saved')
  })

  it.each(['config.save', 'config.test'])('does not abort an already dispatched %s when its reply expires', async type => {
    granted = true
    let finish!: (value: unknown) => void
    const operation = type === 'config.save' ? mocks.save : mocks.test
    operation.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { view, message, post } = await mountLegacy()
    message(type, 'slow-operation', { config: { value: 'kept' } }); await flushPromises()
    const signal = operation.mock.calls[0][type === 'config.save' ? 2 : 1] as AbortSignal
    await vi.advanceTimersByTimeAsync(30_001)
    expect(signal.aborted).toBe(false)
    finish(type === 'config.save' ? { value: 'kept' } : { success: false, message: 'known failure' })
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    if (type === 'config.save') expect(mocks.success).toHaveBeenCalledWith('common.saved')
    else expect(mocks.error).toHaveBeenCalledWith('known failure')
    expect(view.find('iframe').exists()).toBe(true)
  })

  it.each(['close', 'reload'])('invalidates an expired operation on %s and prevents a later verification callback from saving', async reason => {
    const { view, iframe, message, post } = await mountLegacy()
    message('config.save', 'waiting', { config: { forbidden: true } }); await flushPromises()
    const controller = view.getComponent(Prompt).props('controller') as StepUpController
    const signal = mocks.save.mock.calls[0][2] as AbortSignal
    await vi.advanceTimersByTimeAsync(30_001)
    expect(controller.visible.value).toBe(true)
    if (reason === 'close') view.getComponent(BaseDialog).vm.$emit('close')
    else await iframe.trigger('load')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(controller.visible.value).toBe(false)
    controller.onVerified(); await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(post).not.toHaveBeenCalled()
    expect(mocks.success).not.toHaveBeenCalled()
  })

  it('does not deliver an expired result to a new request using the same ID', async () => {
    let oldResult!: (value: unknown) => void
    let newResult!: (value: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(resolve => { oldResult = resolve })).mockReturnValueOnce(new Promise(resolve => { newResult = resolve }))
    const { view, message, post } = await mountLegacy()
    message('config.load', 'same'); await vi.advanceTimersByTimeAsync(30_001)
    const signal = mocks.get.mock.calls[0][1] as AbortSignal
    expect(signal.aborted).toBe(false)
    message('config.load', 'same')
    oldResult({ stale: true }); await flushPromises()
    expect(post).not.toHaveBeenCalled()
    newResult({ current: true }); await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'same', config: { current: true } }), '*')
    view.getComponent(BaseDialog).vm.$emit('close'); await flushPromises()
    expect(signal.aborted).toBe(true)
  })
})
