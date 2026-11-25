import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UserAccountBlockSettings from '../UserAccountBlockSettings.vue'
import Select from '@/components/common/Select.vue'
import type { UserAccountBlockItem } from '@/api/admin/settings'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const mock = vi.hoisted(() => {
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  return { list: vi.fn(), remove: vi.fn(), success: vi.fn(), error: vi.fn() }
})

vi.mock('@/api/admin/settings', () => ({
  listUserAccountBlocks: mock.list,
  deleteUserAccountBlock: mock.remove,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mock.success, showError: mock.error }) }))

const entry: UserAccountBlockItem = {
  user_id: 17, account_id: 9, created_at: '2026-11-24T01:00:00Z', expires_at: '2026-11-24T07:00:00Z',
}
const wrappers: VueWrapper[] = []

function mountCard(locale = 'zh', visible = true) {
  const duration = ref(12)
  const active = ref(visible)
  const submit = vi.fn()
  const component = defineComponent({
    components: { UserAccountBlockSettings },
    setup: () => ({ duration, active, submit }),
    template: '<form @submit.prevent="submit"><UserAccountBlockSettings v-model="duration" :active="active" /></form>',
  })
  const wrapper = mount(component, {
    attachTo: document.body,
    global: { plugins: [createI18n({ locale, legacy: false, messages: { zh, en } })] },
  })
  wrappers.push(wrapper)
  return { wrapper, duration, active, submit }
}

function button(wrapper: VueWrapper, name: string) {
  const match = wrapper.findAll('button').find(node => node.text().trim() === name)
  if (!match) throw new Error(`Could not find button ${name}`)
  return match
}

describe('UserAccountBlockSettings', () => {
  beforeEach(() => { vi.resetAllMocks(); mock.list.mockResolvedValue([entry]); mock.remove.mockResolvedValue(undefined) })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })
  afterAll(() => { vi.unstubAllGlobals() })

  it('loads the block list when the gateway tab opens', async () => {
    const { wrapper, active, duration } = mountCard('zh', false)
    expect(mock.list).not.toHaveBeenCalled()
    active.value = true
    await flushPromises()
    expect(mock.list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('用户账号屏蔽')
    expect(wrapper.text()).toContain('#17')
    expect(wrapper.text()).toContain('#9')
    expect(wrapper.text()).toContain('到期时间')
    const select = wrapper.getComponent(Select)
    expect(select.props('modelValue')).toBe(12)
    select.vm.$emit('update:modelValue', 6)
    expect(duration.value).toBe(6)
  })

  it('keeps a row on failure and removes only that row on success', async () => {
    mock.list.mockResolvedValue([entry, { ...entry, user_id: 18 }])
    mock.remove.mockRejectedValueOnce({ code: 'RATE_LIMITED', status: 429 })
    const { wrapper, submit } = mountCard()
    await flushPromises()
    expect(button(wrapper, '解除').attributes('type')).toBe('button')
    await button(wrapper, '解除').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('#17')
    expect(mock.error).toHaveBeenCalledWith(expect.stringContaining('操作未执行'))
    expect(mock.success).not.toHaveBeenCalled()
    await button(wrapper, '解除').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('#17')
    expect(wrapper.text()).toContain('#18')
    expect(mock.remove).toHaveBeenLastCalledWith(17, 9)
    expect(mock.success).toHaveBeenCalledWith('已解除')
    expect(submit).not.toHaveBeenCalled()
  })

  it.each([
    [{ status: 429 }, '暂时无法读取屏蔽名单，请重试。'],
    [{ status: 429, code: 'RATE_LIMITED' }, '请求过于频繁，暂时无法读取屏蔽名单，请稍后重试。'],
  ])('uses the error reason for a list failure %j', async (error, message) => {
    mock.list.mockRejectedValueOnce(error).mockResolvedValueOnce([])
    const { wrapper, submit } = mountCard()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(message)
    expect(button(wrapper, '重试').attributes('type')).toBe('button')
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('当前没有账号屏蔽记录')
    expect(submit).not.toHaveBeenCalled()
  })

  it('disables refresh and removal while either request is pending', async () => {
    const { wrapper, submit } = mountCard('en')
    await flushPromises()
    let finishList!: (items: UserAccountBlockItem[]) => void
    mock.list.mockImplementationOnce(() => new Promise(resolve => { finishList = resolve }))
    expect(button(wrapper, 'Refresh').attributes('type')).toBe('button')
    await button(wrapper, 'Refresh').trigger('click')
    expect(button(wrapper, 'Refresh').attributes('disabled')).toBeDefined()
    await button(wrapper, 'Refresh').trigger('click')
    expect(mock.list).toHaveBeenCalledTimes(2)
    finishList([entry])
    await flushPromises()
    let finishRemove!: () => void
    mock.remove.mockImplementationOnce(() => new Promise<void>(resolve => { finishRemove = resolve }))
    await button(wrapper, 'Remove').trigger('click')
    expect(button(wrapper, 'Remove').attributes('disabled')).toBeDefined()
    await button(wrapper, 'Refresh').trigger('click')
    expect(mock.list).toHaveBeenCalledTimes(2)
    finishRemove()
    await flushPromises()
    expect(wrapper.text()).toContain('No account blocks right now')
    expect(submit).not.toHaveBeenCalled()
  })

  it('loads the latest list after returning to the gateway tab during removal', async () => {
    const { wrapper, active } = mountCard('en')
    await flushPromises()
    let finishRemove!: () => void
    mock.remove.mockImplementationOnce(() => new Promise<void>(resolve => { finishRemove = resolve }))
    mock.list.mockResolvedValueOnce([{ ...entry, user_id: 18, account_id: 10 }])
    await button(wrapper, 'Remove').trigger('click')
    active.value = false
    await flushPromises()
    active.value = true
    await flushPromises()
    expect(mock.list).toHaveBeenCalledTimes(1)
    finishRemove()
    await flushPromises()
    expect(mock.list).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('#17')
    expect(wrapper.text()).toContain('#18')
    expect(wrapper.text()).toContain('#10')
  })
})
