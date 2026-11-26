import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DingTalkCallbackView from '../DingTalkCallbackView.vue'
import DingTalkEmailCompletionView from '../DingTalkEmailCompletionView.vue'
import PendingOAuthCreateAccountForm from '@/components/auth/PendingOAuthCreateAccountForm.vue'

const mocks = vi.hoisted(() => ({ post: vi.fn(), replace: vi.fn(), setToken: vi.fn(), exchange: vi.fn(), showInfo: vi.fn(), showError: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }), useRouter: () => ({ replace: mocks.replace }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/api/client', () => ({ apiClient: { post: mocks.post } }))
vi.mock('@/api/auth', async () => ({
  ...await vi.importActual<typeof import('@/api/auth')>('@/api/auth'),
  exchangePendingOAuthCompletion: mocks.exchange,
  getPublicSettings: vi.fn().mockResolvedValue({})
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ setToken: mocks.setToken, clearPendingAuthSession: vi.fn() }),
  useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn(), showInfo: mocks.showInfo })
}))

const global = { stubs: { AuthLayout: { template: '<div><slot /></div>' }, Icon: true, RouterLink: true, PendingOAuthCreateAccountForm: true, transition: false } }
beforeEach(() => {
  vi.clearAllMocks()
  window.location.hash = ''
})
describe('DingTalk registration IP rejection', () => {
  it('does not fall back or log in after the callback redirects a registration rejection', async () => {
    window.location.hash = '#error=session_error&error_message=REGISTRATION_IP_BLOCKED'
    const wrapper = mount(DingTalkCallbackView, { global })
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('auth.errors.REGISTRATION_IP_BLOCKED')
    expect(mocks.exchange).not.toHaveBeenCalled()
    expect(mocks.setToken).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps email completion error visible without redirecting to the global-registration-disabled binding flow', async () => {
    mocks.post.mockRejectedValue({ response: { data: { reason: 'REGISTRATION_IP_BLOCKED', message: 'raw backend' } } })
    const wrapper = mount(DingTalkEmailCompletionView, { global })
    const form = wrapper.getComponent(PendingOAuthCreateAccountForm)
    form.vm.$emit('submit', { email: 'fixture@example.com', password: 'test-password' })
    await flushPromises()
    expect(form.props('errorMessage')).toBe('auth.errors.REGISTRATION_IP_BLOCKED')
    expect(mocks.replace).not.toHaveBeenCalled()
    expect(mocks.setToken).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
