import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ForkEmailSignupFilterSettings from '../ForkEmailSignupFilterSettings.vue'
import type { ForkEmailSignupFilterResponse } from '@/api/admin/forkEmailSignupFilter'
import zhFilter from '@/i18n/locales/zh/admin/forkEmailSignupFilter'
import enFilter from '@/i18n/locales/en/admin/forkEmailSignupFilter'
import zhCommon from '@/i18n/locales/zh/common'
import enCommon from '@/i18n/locales/en/common'

// Match the application Vite flag while retaining the real runtime message compiler.
const mocks = vi.hoisted(() => {
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  return { get: vi.fn(), update: vi.fn(), success: vi.fn(), error: vi.fn() }
})
vi.mock('@/api/admin/forkEmailSignupFilter', () => ({
  getForkEmailSignupFilter: mocks.get,
  updateForkEmailSignupFilter: mocks.update,
}))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error }) }))

const initial: ForkEmailSignupFilterResponse = {
  enabled: true,
  trusted_domains: ['qq.com', 'gmail.com'],
  time_window_enabled: false,
  window_start: '09:00',
  window_end: '18:00',
  server_timezone: 'Asia/Tokyo',
  config_invalid: false,
}
const wrappers: VueWrapper[] = []

function mountPanel(locale = 'zh', outerForm = false) {
  const submit = vi.fn()
  const component = outerForm ? defineComponent({
    components: { ForkEmailSignupFilterSettings },
    setup: () => ({ submit }),
    template: '<form @submit.prevent="submit"><ForkEmailSignupFilterSettings /></form>',
  }) : ForkEmailSignupFilterSettings
  const wrapper = mount(component, {
    global: { plugins: [createI18n({ legacy: false, locale, messages: {
      zh: { ...zhCommon, admin: zhFilter },
      en: { ...enCommon, admin: enFilter },
    } })] },
  })
  wrappers.push(wrapper)
  return { wrapper, submit }
}

function field(wrapper: VueWrapper, suffix: string) {
  return wrapper.get(`[data-testid="fork-email-signup-filter-${suffix}"]`)
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

describe('ForkEmailSignupFilterSettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue({ ...initial })
    mocks.update.mockResolvedValue({ ...initial })
  })
  afterEach(() => { wrappers.splice(0).forEach((wrapper) => wrapper.unmount()) })
  afterAll(() => { vi.unstubAllGlobals() })

  it('loads values and renders the real Chinese messages including @ and server timezone', async () => {
    const { wrapper } = mountPanel()
    await flushPromises()
    expect(field(wrapper, 'enabled').attributes('aria-checked')).toBe('true')
    expect((field(wrapper, 'domains').element as HTMLTextAreaElement).value).toBe('qq.com\ngmail.com')
    expect((field(wrapper, 'start').element as HTMLInputElement).value).toBe('09:00')
    expect((field(wrapper, 'end').element as HTMLInputElement).value).toBe('18:00')
    expect(wrapper.text()).toContain('邮箱 @ 前含 + 的地址（例如 abc+1@qq.com）')
    expect(wrapper.text()).toContain('按服务器时区 Asia/Tokyo 计算')
    expect(wrapper.text()).toContain('重新读取会放弃本区块尚未保存的输入。')
    expect(wrapper.text()).not.toContain('admin.forkEmailSignupFilter')
    expect(wrapper.find('[data-testid="fork-email-signup-filter-invalid-warning"]').exists()).toBe(false)
  })

  it('renders the real English messages without exposing i18n literal syntax', async () => {
    const { wrapper } = mountPanel('en')
    await flushPromises()
    expect(wrapper.text()).toContain('Email signup grant filter')
    expect(wrapper.text()).toContain('before @ (for example abc+1@qq.com)')
    expect(wrapper.text()).not.toContain("{'@'}")
  })

  it('locks every input and button while loading, including both real Toggle buttons', async () => {
    const pending = deferred<ForkEmailSignupFilterResponse>()
    mocks.get.mockReturnValueOnce(pending.promise)
    const { wrapper } = mountPanel()
    expect(wrapper.text()).toContain('正在读取赠送筛选设置…')
    for (const control of wrapper.findAll('input, textarea, button')) expect(control.attributes('disabled')).toBeDefined()
    await field(wrapper, 'enabled').trigger('click')
    expect(field(wrapper, 'enabled').attributes('aria-checked')).toBe('false')
    pending.resolve({ ...initial })
    await flushPromises()
    expect(field(wrapper, 'save').attributes('disabled')).toBeUndefined()
  })

  it('shows invalid-config warning and allows the returned defaults to be corrected', async () => {
    mocks.get.mockResolvedValueOnce({ ...initial, enabled: false, trusted_domains: [], config_invalid: true })
    const { wrapper } = mountPanel()
    await flushPromises()
    expect(field(wrapper, 'invalid-warning').text()).toContain('目前邮箱注册按原有设置赠送，筛选没有生效')
    expect((field(wrapper, 'domains').element as HTMLTextAreaElement).value).toBe('')
    expect(field(wrapper, 'save').attributes('disabled')).toBeUndefined()
  })

  it('shows the translated load reason, permits only reload, and reloads after failure', async () => {
    mocks.get.mockRejectedValueOnce({ status: 500, reason: 'FORK_EMAIL_SIGNUP_FILTER_LOAD_FAILED' })
    const { wrapper } = mountPanel()
    await flushPromises()
    expect(field(wrapper, 'load-error').text()).toContain('服务器读取设置失败')
    for (const control of wrapper.findAll('input, textarea, button')) {
      expect(control.attributes('disabled') !== undefined).toBe(control.attributes('data-testid') !== 'fork-email-signup-filter-reload')
    }
    await field(wrapper, 'reload').trigger('click')
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="fork-email-signup-filter-load-error"]').exists()).toBe(false)
    expect((field(wrapper, 'domains').element as HTMLTextAreaElement).value).toBe('qq.com\ngmail.com')
  })

  it('submits all five fields with domains split by line and refreshes from the server response', async () => {
    const { wrapper } = mountPanel()
    await flushPromises()
    await field(wrapper, 'domains').setValue(' @QQ.COM \n\ngmail.com\nqq.com')
    await field(wrapper, 'time-enabled').trigger('click')
    await field(wrapper, 'start').setValue('10:00')
    mocks.update.mockResolvedValueOnce({ ...initial, time_window_enabled: true, window_start: '10:00' })
    await field(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith({ enabled: true, trusted_domains: [' @QQ.COM ', '', 'gmail.com', 'qq.com'], time_window_enabled: true, window_start: '10:00', window_end: '18:00' })
    expect(mocks.success).toHaveBeenCalledWith('赠送筛选已保存')
    expect((field(wrapper, 'domains').element as HTMLTextAreaElement).value).toBe('qq.com\ngmail.com')
  })

  it('locks every control for a delayed save and prevents further toggling or reload', async () => {
    const pending = deferred<ForkEmailSignupFilterResponse>()
    mocks.update.mockReturnValueOnce(pending.promise)
    const { wrapper } = mountPanel()
    await flushPromises()
    await field(wrapper, 'save').trigger('click')
    for (const control of wrapper.findAll('input, textarea, button')) expect(control.attributes('disabled')).toBeDefined()
    await field(wrapper, 'enabled').trigger('click')
    await field(wrapper, 'time-enabled').trigger('click')
    await field(wrapper, 'reload').trigger('click')
    expect(field(wrapper, 'enabled').attributes('aria-checked')).toBe('true')
    expect(field(wrapper, 'time-enabled').attributes('aria-checked')).toBe('false')
    expect(mocks.get).toHaveBeenCalledTimes(1)
    pending.resolve({ ...initial })
    await flushPromises()
    expect(field(wrapper, 'save').attributes('disabled')).toBeUndefined()
  })

  it.each([
    [{ status: 400, reason: 'FORK_EMAIL_SIGNUP_FILTER_INVALID_DOMAIN', metadata: { domain: 'bad@domain' } }, '请修改后再保存', '未能确认是否保存成功'],
    [{ status: 500, reason: 'FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED' }, '未能确认是否保存成功', '请修改后再保存'],
    [{ status: 0, message: 'Network error' }, '未能确认是否保存成功', '请修改后再保存'],
  ] as const)('retains input and distinguishes invalid input from an unconfirmed result: %o', async (error, expected, unexpected) => {
    mocks.update.mockRejectedValueOnce(error)
    const { wrapper } = mountPanel()
    await flushPromises()
    await field(wrapper, 'domains').setValue('unsaved.example')
    await field(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.error).toHaveBeenCalledTimes(1)
    expect(mocks.error.mock.calls[0][0]).toContain(expected)
    expect(mocks.error.mock.calls[0][0]).not.toContain(unexpected)
    expect((field(wrapper, 'domains').element as HTMLTextAreaElement).value).toBe('unsaved.example')
    expect(mocks.success).not.toHaveBeenCalled()
    if (error.status === 400) expect(mocks.error.mock.calls[0][0]).toContain('不能包含 @、空格、* 或 /')
  })

  it('never submits the outer form when saving or reloading', async () => {
    const { wrapper, submit } = mountPanel('zh', true)
    await flushPromises()
    await field(wrapper, 'save').trigger('click')
    await flushPromises()
    await field(wrapper, 'reload').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(submit).not.toHaveBeenCalled()
    expect(field(wrapper, 'save').attributes('type')).toBe('button')
    expect(field(wrapper, 'reload').attributes('type')).toBe('button')
  })
})
