import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import OpsErrorLogTable from '../OpsErrorLogTable.vue'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import type { OpsErrorLog } from '@/api/admin/ops'
import type { UserAccountBlockItem } from '@/api/admin/settings'

const mock = vi.hoisted(() => {
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  return { create: vi.fn(), success: vi.fn(), error: vi.fn() }
})

vi.mock('@/api/admin/settings', () => ({ createUserAccountBlock: mock.create }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mock.success, showError: mock.error }) }))

const item: UserAccountBlockItem = {
  user_id: 17, account_id: 9, created_at: '2026-11-24T01:00:00Z', expires_at: '2026-11-24T07:00:00Z',
}
const wrappers: VueWrapper[] = []

function mountTable(inputs: Array<Partial<OpsErrorLog>> = [{}], locale = 'zh') {
  const rows = inputs.map((overrides, idx) => ({
    id: idx + 1, phase: 'upstream', severity: 'error', status_code: 503,
    platform: 'anthropic', created_at: item.created_at,
    user_id: 17, user_email: 'alice@example.test', account_id: 9, account_name: 'upstream',
    ...overrides,
  }) as OpsErrorLog)
  const wrapper = mount(OpsErrorLogTable, {
    props: { rows, total: rows.length, page: 1, pageSize: 20, loading: false },
    global: { plugins: [createI18n({ locale, legacy: false, messages: { zh, en } })],
      stubs: { Pagination: true, IpGeoBatchToolbar: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}

const blockButtons = (wrapper: VueWrapper) => wrapper.findAll('button[aria-label^="为用户"],button[aria-label^="Block account"]')

describe('OpsErrorLogTable 账号屏蔽', () => {
  beforeEach(() => { vi.resetAllMocks(); mock.create.mockResolvedValue(item) })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })
  afterAll(() => { vi.unstubAllGlobals() })

  it('submits the block for this row', async () => {
    const wrapper = mountTable([{ user_id: 15, account_id: 8 }, { user_id: 17, account_id: 9 }])
    const [first, second] = blockButtons(wrapper)
    expect(first.attributes('title')).toContain('upstream #8')
    expect(second.attributes('title')).toContain('upstream #9')
    await second.trigger('click')
    await flushPromises()
    expect(mock.create).toHaveBeenCalledTimes(1)
    expect(mock.create).toHaveBeenCalledWith(17, 9)
    expect(wrapper.emitted('openErrorDetail')).toBeUndefined()
    expect(mock.success).toHaveBeenCalledWith(expect.stringMatching(/已屏蔽，.+到期/))
    expect(mock.error).not.toHaveBeenCalled()
  })

  it('requires both IDs and labels the target in English even without names', () => {
    const wrapper = mountTable([{ user_id: undefined }, { account_id: undefined }, { user_email: '', account_name: '' }], 'en')
    const buttons = blockButtons(wrapper)
    expect(buttons).toHaveLength(1)
    expect(buttons[0].attributes('aria-label')).toBe('Block account #9 for user #17')
  })

  it('disables all block actions while a write is pending', async () => {
    let complete!: (value: UserAccountBlockItem) => void
    mock.create.mockImplementation(() => new Promise<UserAccountBlockItem>(resolve => { complete = resolve }))
    const wrapper = mountTable([{}, { account_id: 8 }])
    const [first, second] = blockButtons(wrapper)
    await first.trigger('click')
    expect(first.attributes('disabled')).toBeDefined()
    expect(second.attributes('disabled')).toBeDefined()
    await second.trigger('click')
    expect(mock.create).toHaveBeenCalledTimes(1)
    complete(item)
    await flushPromises()
    expect(first.attributes('disabled')).toBeUndefined()
    expect(second.attributes('disabled')).toBeUndefined()
  })

  it.each([
    [{ code: 'RATE_LIMITED', status: 429 }, '请求过于频繁，操作未执行'],
    [{ status: 429 }, '暂时无法确认操作结果'],
    [{ code: 500, reason: 'USER_ACCOUNT_BLOCK_DURATION_READ_FAILED' }, '屏蔽未执行，请稍后重试'],
    [{ reason: 'USER_ACCOUNT_BLOCK_INVALID_ID' }, '用户或账号编号无效'],
    [{ code: 500, message: 'internal error' }, '暂时无法确认操作结果'],
    [{ reason: 'ADMIN_COMPLIANCE_ACK_REQUIRED' }, ''],
  ])('handles failure %j and re-enables the action', async (error, message) => {
    mock.create.mockRejectedValue(error)
    const [button] = blockButtons(mountTable())
    await button.trigger('click')
    await flushPromises()
    if (message) {
      expect(mock.error).toHaveBeenCalledWith(expect.stringContaining(message))
      expect(mock.error.mock.calls[0][0]).not.toContain('internal error')
    } else {
      expect(mock.error).not.toHaveBeenCalled()
    }
    expect(mock.success).not.toHaveBeenCalled()
    expect(button.attributes('disabled')).toBeUndefined()
  })
})
