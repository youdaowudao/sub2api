import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'

import AccountsView from '../AccountsView.vue'
import AccountActionMenu from '@/components/admin/account/AccountActionMenu.vue'
import AccountScheduleTimer from '@/components/account/AccountScheduleTimer.vue'

const {
  listAccounts,
  listWithEtag,
  getById,
  getBatchTodayStats,
  getBatchUsage,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  refreshCredentials,
  startScheduleTimer,
  showSuccess,
  showError,
  showWarning
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getById: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getBatchUsage: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  refreshCredentials: vi.fn(),
  startScheduleTimer: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { getCacheHitRate: vi.fn().mockResolvedValue({ items: [] }) },
    accounts: {
      list: listAccounts,
      getById,
      listWithEtag,
      getBatchTodayStats,
      getBatchUsage,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
      refreshCredentials,
      startScheduleTimer
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] }, loading: Boolean },
  setup: () => ({ visible: ref(true) }),
  template: `
    <div>
      <button data-test="virtual-window" @click="visible = !visible">toggle visible rows</button>
      <template v-if="!loading && visible"><div v-for="row in data" :key="row.id" :data-account-name="row.name">
        <slot name="cell-schedulable" :row="row" />
        <slot name="cell-groups" :row="row" />
        <slot name="cell-usage" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div></template>
    </div>
  `
})

const AccountGroupsCellStub = defineComponent({
  props: { groups: { type: Array, default: () => [] } },
  template: '<span data-test="account-groups">{{ groups.map(group => group.name).join(",") }}</span>'
})

const EditAccountModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="edit-account">{{ show ? account?.name : "" }}</div>'
})

const AccountTestModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="test-account">{{ show ? account?.name : "" }}</div>'
})

const AccountStatsModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="stats-account">{{ show ? account?.name : "" }}</div>'
})

function mountView(stubActionMenu = true, realUsageCell = false) {
  return mount(AccountsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        AccountTableActions: { template: `<div><button data-test="manual-refresh" @click="$emit('refresh')">refresh</button><slot name="after" /></div>` },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: stubActionMenu,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: AccountTestModalStub,
        AccountStatsModal: AccountStatsModalStub,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: EditAccountModalStub,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: AccountGroupsCellStub,
        AccountUsageCell: !realUsageCell,
        UpstreamBillingRateCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: stubActionMenu
      }
    }
  })
}

const listRow = {
  id: 42,
  name: 'compact row',
  platform: 'openai',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  concurrency: 2,
  priority: 1,
  group_ids: [7],
  extra: {},
  credentials: {}
}

const fullAccount = {
  ...listRow,
  groups: [{ id: 7, name: 'codex', platform: 'openai' }],
  account_groups: [{ account_id: 42, group_id: 7 }],
  credentials: { api_key: 'redacted' },
  extra: { detail_only: true }
}

async function chooseScheduleTimer(wrapper: ReturnType<typeof mountView>, hours: number) {
  await wrapper.getComponent(AccountScheduleTimer).get('button').trigger('click')
  const options = document.querySelectorAll<HTMLElement>('[role="option"]')
  options[[6, 12, 24].indexOf(hours)]!.click()
  await flushPromises()
}

describe('admin AccountsView lite account list', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({ items: [listRow], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: 'compact-etag', data: null })
    getById.mockReset().mockResolvedValue(fullAccount)
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getBatchUsage.mockReset().mockResolvedValue({ usage: { '42': { five_hour: { utilization: 21 }, seven_day: { utilization: 31 } } }, errors: {} })
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: true })
    getAllProxies.mockReset().mockResolvedValue([])
    getAllGroups.mockReset().mockResolvedValue([{ id: 7, name: 'codex', platform: 'openai' }])
    refreshCredentials.mockReset()
    startScheduleTimer.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    showWarning.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it.each([
    ['anthropic', 'oauth'], ['anthropic', 'setup-token'], ['gemini', 'oauth']
  ])('updates %s %s usage on auto-refresh without force or unrelated probes', async (platform, type) => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    vi.spyOn(window, 'matchMedia').mockImplementation(() => ({
      matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn()
    }) as unknown as MediaQueryList)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const account = { ...listRow, platform, type }
    listAccounts.mockResolvedValue({ items: [account], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView(true, true)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(getBatchUsage).toHaveBeenCalledTimes(1)
    expect(getBatchUsage).toHaveBeenCalledWith([42], false)

    const next = { ...account, extra: { session_window_utilization: 0.44, passive_usage_7d_utilization: 0.54 }, last_used_at: platform === 'gemini' ? '2026-11-16T10:00:00Z' : null }
    getBatchUsage.mockResolvedValue({ usage: { '42': { five_hour: { utilization: 44 }, seven_day: { utilization: 54 } } }, errors: {} })
    listWithEtag.mockResolvedValue({ notModified: false, etag: 'new', data: { items: [next], total: 1, pages: 1 } })
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(getBatchUsage).toHaveBeenCalledTimes(2)
    expect(getBatchUsage).toHaveBeenLastCalledWith([42], false)
    const cell = wrapper.findComponent({ name: 'AccountUsageCell' })
    expect(cell.props('account').last_used_at).toBe(next.last_used_at)
    expect(cell.props('batchedUsage').seven_day.utilization).toBe(54)
    cell.props('requestBatchedUsage')(cell.props('account'))
    await vi.advanceTimersByTimeAsync(1)
    expect(getBatchUsage).toHaveBeenCalledTimes(2)

    listWithEtag.mockResolvedValue({ notModified: false, etag: 'unrelated', data: { items: [{ ...next, updated_at: 'new', current_concurrency: 2, extra: { ...next.extra, unrelated: true } }], total: 1, pages: 1 } })
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(getBatchUsage).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('keeps lite=1 on the initial list request', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    wrapper.unmount()
  })

  it('keeps timer pending state and saved results when virtual rows unmount', async () => {
    let finish!: (value: unknown) => void
    startScheduleTimer.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    const result = { id: 42, schedulable: false, schedule_resume_at: '2026-11-21T06:00:00Z' }
    try {
      await flushPromises()
      await chooseScheduleTimer(wrapper, 6)
      expect(startScheduleTimer).toHaveBeenCalledWith(42, 6)
      await wrapper.get('[data-test="virtual-window"]').trigger('click')
      expect(wrapper.findComponent(AccountScheduleTimer).exists()).toBe(false)
      await wrapper.get('[data-test="virtual-window"]').trigger('click')
      expect(wrapper.getComponent(AccountScheduleTimer).get('button').attributes('disabled')).toBeDefined()
      wrapper.getComponent(AccountScheduleTimer).vm.$emit('start', 12)
      expect(startScheduleTimer).toHaveBeenCalledTimes(1)
      await wrapper.get('[data-test="virtual-window"]').trigger('click')
      finish(result)
      await flushPromises()
      await wrapper.get('[data-test="virtual-window"]').trigger('click')
      expect(wrapper.getComponent(AccountScheduleTimer).props('account')).toMatchObject(result)
      expect(startScheduleTimer).toHaveBeenCalledTimes(1)
    } finally {
      finish(result)
      await flushPromises()
      wrapper.unmount()
    }
  })

  it.each([6, 12, 24])('saves %s hours and applies only the returned backend state', async hours => {
    const result = { id: 42, schedulable: false, schedule_resume_at: '2026-11-21T06:00:00Z' }
    startScheduleTimer.mockResolvedValue(result)
    const wrapper = mountView()
    try {
      await flushPromises()
      await chooseScheduleTimer(wrapper, hours)
      expect(startScheduleTimer).toHaveBeenCalledWith(42, hours)
      expect(wrapper.getComponent(AccountScheduleTimer).props('account')).toMatchObject(result)
      expect(showSuccess).toHaveBeenCalledWith('admin.accounts.scheduleTimer.saved')
    } finally {
      wrapper.unmount()
    }
  })

  it.each([
    new Error('network timeout'),
    { status: 0, message: 'Network error' },
    { status: 500, message: 'Internal error' }
  ])('refreshes unknown timer results without claiming a save or retrying: %j', async failure => {
    startScheduleTimer.mockRejectedValue(failure)
    const wrapper = mountView()
    try {
      await flushPromises()
      await chooseScheduleTimer(wrapper, 12)
      expect(showError).toHaveBeenCalledWith('admin.accounts.scheduleTimer.unknown')
      expect(showSuccess).not.toHaveBeenCalled()
      expect(startScheduleTimer).toHaveBeenCalledTimes(1)
      expect(listAccounts).toHaveBeenCalledTimes(2)
      expect(wrapper.getComponent(AccountScheduleTimer).props('account').schedulable).toBe(true)
    } finally {
      wrapper.unmount()
    }
  })

  it.each([
    { status: 409, message: '账号已设置自动开启时间，请刷新后查看' },
    { status: 503, reason: 'ACCOUNT_SCHEDULE_TIMER_NOT_SAVED', message: '数据库操作失败；本次没有更改调度或计时，请稍后重试' }
  ])('shows a confirmed failure reason and refreshes without changing the switch: %j', async failure => {
    startScheduleTimer.mockRejectedValue(failure)
    const wrapper = mountView()
    try {
      await flushPromises()
      await chooseScheduleTimer(wrapper, 24)
      expect(showError).toHaveBeenCalledWith(failure.message)
      expect(showSuccess).not.toHaveBeenCalled()
      expect(startScheduleTimer).toHaveBeenCalledTimes(1)
      expect(listAccounts).toHaveBeenCalledTimes(2)
      expect(wrapper.getComponent(AccountScheduleTimer).props('account').schedulable).toBe(true)
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps existing rows mounted and busy while a manual list refresh is pending', async () => {
    const wrapper = mountView()
    await flushPromises()
    const row = wrapper.get('[data-account-name="compact row"]').element
    let finish: (value: unknown) => void = () => {}
    listAccounts.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('[data-test="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(DataTableStub).props('loading')).toBe(false)
    expect(wrapper.get('[data-account-name="compact row"]').element).toBe(row)
    expect(wrapper.find('[aria-busy="true"]').attributes()).toHaveProperty('inert')
    finish({ items: [{ ...listRow, name: 'updated row' }], total: 1, page: 1, page_size: 20, pages: 1 })
    await flushPromises()
    expect(wrapper.get('[data-account-name="updated row"]').element).toBe(row)
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('still shows initial loading when no rows are available', async () => {
    let finish: (value: unknown) => void = () => {}
    listAccounts.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent(DataTableStub).props('loading')).toBe(true)
    finish({ items: [listRow], total: 1, page: 1, page_size: 20, pages: 1 })
    await flushPromises()
    wrapper.unmount()
  })

  it('maps group_ids through the group catalog for the table cell', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="account-groups"]').text()).toBe('codex')
    wrapper.unmount()
  })

  it('keeps the action menu open during internal scrolling but closes it on table scrolling', async () => {
    const wrapper = mountView(false)
    await flushPromises()

    const trigger = wrapper.findAll('button').find(button => button.text() === 'common.more')!
    await trigger.trigger('click')
    const menu = new DOMWrapper(document.body.querySelector('.action-menu-content')!)
    menu.element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    menu.get('button').element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    wrapper.getComponent(DataTableStub).element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('keeps lite=1 on automatic ETag refreshes', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const wrapper = mountView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()

    expect(listWithEtag).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ etag: null })
    )
    wrapper.unmount()
  })

  it('loads the full account by id before opening edit, test, and stats actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    expect(getById).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-test="edit-account"]').text()).toBe('compact row')

    const menu = wrapper.findComponent(AccountActionMenu)
    menu.vm.$emit('test', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="test-account"]').text()).toBe('compact row')

    menu.vm.$emit('stats', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-test="stats-account"]').text()).toBe('compact row')
    wrapper.unmount()
  })

  it('shows the warning and patches the account after a partial Antigravity refresh', async () => {
    refreshCredentials.mockResolvedValue({
      account: { ...fullAccount, name: 'refreshed account' },
      message: 'Token refreshed, but project_id is temporarily unavailable',
      warning: 'missing_project_id_temporary'
    })
    const wrapper = mountView(false)
    await flushPromises()

    wrapper.findComponent(AccountActionMenu).vm.$emit('refresh-token', listRow)
    await flushPromises()

    expect(refreshCredentials).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-account-name]').attributes('data-account-name')).toBe('refreshed account')
    expect(showWarning).toHaveBeenCalledWith('Token refreshed, but project_id is temporarily unavailable')
    wrapper.unmount()
  })

  it('shows an error and keeps the modal closed when detail loading fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getById.mockRejectedValueOnce(new Error('detail failed'))
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    await editButton!.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('detail failed')
    expect(wrapper.get('[data-test="edit-account"]').text()).toBe('')
    consoleError.mockRestore()
    wrapper.unmount()
  })
})

vi.mock('@/composables/useCacheHitRateDefaultRange', () => ({ useCacheHitRateDefaultRange: () => ({ dates: { value: { start: '2026-10-26', end: '2026-10-26' } } }) }))
