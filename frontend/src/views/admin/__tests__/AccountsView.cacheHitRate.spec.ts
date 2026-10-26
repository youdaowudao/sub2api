import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'
import { reactive } from 'vue'
import CacheHitRateCell from '@/components/admin/usage/CacheHitRateCell.vue'

const {
  getCacheHitRate,
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups
} = vi.hoisted(() => ({
  getCacheHitRate: vi.fn(),
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { getCacheHitRate },
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const period = reactive({ start: '2026-10-26', end: '2026-10-26' })
vi.mock('@/composables/useCacheHitRateDefaultRange', async () => {
  const { watch } = await vi.importActual<typeof import('vue')>('vue')
  return { useCacheHitRateDefaultRange: (callback: () => void) => {
    watch(() => period.start, callback)
    return { dates: { get value() { return period } } }
  } }
})

// Render the per-column header slots so we can assert the usage-window header hint.
const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <div data-test="data-table">
      <template v-for="column in columns" :key="column.key">
        <div v-if="column.key === 'usage'" data-test="usage-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
        <div v-if="column.key === 'upstream_billing_rate'" data-test="upstream-billing-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
      </template>
      <div v-for="row in data" :key="row.id" data-test="account-rate">
        <slot name="cell-rate_multiplier" :row="row" />
        <slot v-if="columns.some(c => c.key === 'cache_hit_rate')" name="cell-cache_hit_rate" :row="row" />
      </div>
    </div>
  `
}

// Expose the content passed to HelpTooltip without dealing with its <Teleport>.
const HelpTooltipStub = {
  props: ['content', 'widthClass'],
  template: '<span data-test="usage-windows-hint">{{ content }}</span>'
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: HelpTooltipStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: {
          props: ['groups'],
          template: '<div data-test="account-filters" :data-group-count="groups.length"></div>'
        },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        Icon: true
      }
    }
  })
}


describe('account cache rate column', () => {
  beforeEach(() => {
    localStorage.clear(); vi.clearAllMocks()
    period.start = period.end = '2026-10-26'
    listAccounts.mockResolvedValue({ items: [{ id: 7, name: 'Fixture', platform: 'anthropic', type: 'apikey', status: 'active', credentials: {}, extra: {}, groups: [] }], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} }); getAllProxies.mockResolvedValue([]); getAllGroups.mockResolvedValue([])
    getCacheHitRate.mockResolvedValue({ items: [{ id: 7, requests: 3, cache_hit_rate: 90 }] })
  })
  it('loads visible current-page IDs and stops requests while hidden', async () => {
    const wrapper = mountView(); await flushPromises()
    expect((wrapper.vm as any).allColumns.map((c: { key: string }) => c.key)).toContain('cache_hit_rate')
    expect(getCacheHitRate).toHaveBeenCalledWith(expect.objectContaining({ dimension: 'account', account_ids: '7', start_date: '2026-10-26', end_date: '2026-10-26' }))
    expect(wrapper.findComponent(CacheHitRateCell).props('item').cache_hit_rate).toBe(90)
    ;(wrapper.vm as any).toggleColumn('cache_hit_rate'); getCacheHitRate.mockClear()
    await (wrapper.vm as any).reload(); await flushPromises(); expect(getCacheHitRate).not.toHaveBeenCalled()
    ;(wrapper.vm as any).toggleColumn('cache_hit_rate'); await flushPromises(); expect(getCacheHitRate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('reloads changed dates and rejects late results', async () => {
    let resolve!: (value: unknown) => void
    getCacheHitRate.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const wrapper = mountView(); await flushPromises()
    period.start = period.end = '2026-10-25'; await flushPromises()
    expect(getCacheHitRate).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-25', end_date: '2026-10-25' }))
    resolve({ items: [{ id: 7, requests: 3, cache_hit_rate: 20 }] }); await flushPromises()
    expect(wrapper.findComponent(CacheHitRateCell).props('item').cache_hit_rate).toBe(90)
    wrapper.unmount()
  })
})
