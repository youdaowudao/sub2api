import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import PluginFrame from '../PluginFrame.vue'
import type { PluginEntry } from '@/api/plugins'

enableAutoUnmount(afterEach)
const mocks = vi.hoisted(() => ({ session: vi.fn(), call: vi.fn(), action: vi.fn() }))
vi.mock('@/api/plugins', () => ({ default: { createSession: mocks.session, call: mocks.call, action: mocks.action } }))
vi.mock('vue-router', () => ({ useRoute: () => reactive({ path: '/keys', fullPath: '/keys', name: 'Keys' }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => reactive({ token: 'fixture-panel', isAdmin: false, user: { id: 42, role: 'user', status: 'active' } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('@/composables/useStepUp', () => ({ useStepUp: () => ({ run: (action: () => Promise<unknown>) => action(), onCancel: vi.fn() }), isStepUpCancelled: () => false, isStepUpBlocked: () => false, stepUpBlockReason: () => '' }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

describe('business document authority', () => {
  it('never gives the session token to a document merely because the iframe loads or navigates', async () => {
    const entry: PluginEntry = { plugin_id: 7, plugin_key: 'example.ui', revision: 'one', entry_id: 'home', label: 'Plugin', mount: 'user', roles: ['user'], methods: [{ name: 'read', role: 'user', access: 'read' }] }
    mocks.session.mockResolvedValue({ url: '/fixture.html#bridge_token=fixture-document-proof', bridge_token: 'fixture-document-proof', ui_bridge_version: 2, entry })
    const wrapper = mount(PluginFrame, { props: { entry }, attachTo: document.body, global: { stubs: { TotpStepUpDialog: true } } })
    await flushPromises()
    const frame = wrapper.get('iframe')
    const target = (frame.element as HTMLIFrameElement).contentWindow!
    const outgoing = vi.spyOn(target, 'postMessage').mockImplementation(() => {})
    await frame.trigger('load')
    await frame.trigger('load')
    window.dispatchEvent(new MessageEvent('message', { source: target, origin: 'null', data: { source: 'sub2api-plugin-ui', type: 'plugin.call', request_id: 'foreign', method: 'read', payload: {} } }))
    await flushPromises()
    // A public document challenge may cross the window boundary. Authority,
    // user context and business results must remain on the proved document port.
    for (const [data] of outgoing.mock.calls) {
      expect(data).not.toHaveProperty('bridge_token')
      expect(data).not.toHaveProperty('context')
      expect(data).not.toHaveProperty('result')
    }
    expect(mocks.call).not.toHaveBeenCalled()
    expect(mocks.action).not.toHaveBeenCalled()
  })
})
