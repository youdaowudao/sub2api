import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

import UsageFilters from '../UsageFilters.vue'
import AdminGroupPicker from '@/components/admin/AdminGroupPicker.vue'
import UsageView from '@/views/admin/UsageView.vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'

// --- i18n messages (only what UsageFilters needs) ---
const messages: Record<string, string> = {
  'admin.usage.userDeletedBadge': 'deleted',
  'admin.usage.userFilter': 'User',
  'admin.usage.searchUserPlaceholder': 'Search user...',
  'usage.apiKeyFilter': 'API Key',
  'admin.usage.searchApiKeyPlaceholder': 'Search API key...',
  'usage.model': 'Model',
  'admin.usage.allModels': 'All Models',
  'admin.usage.account': 'Account',
  'admin.usage.searchAccountPlaceholder': 'Search account...',
  'usage.type': 'Type',
  'admin.usage.allTypes': 'All Types',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.compactionFilter': 'Request Kind',
  'usage.allCompactionTypes': 'All Requests',
  'usage.compactionOnly': 'Compaction Only',
  'admin.usage.billingType': 'Billing Type',
  'admin.usage.allBillingTypes': 'All Billing Types',
  'admin.usage.billingTypeBalance': 'Balance',
  'admin.usage.billingTypeSubscription': 'Subscription',
  'admin.usage.billingMode': 'Billing Mode',
  'admin.usage.allBillingModes': 'All Billing Modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per Request',
  'admin.usage.billingModeImage': 'Image',
	'admin.usage.upstreamModelAudit': 'Upstream model audit',
	'admin.usage.allUpstreamModelAudit': 'All response model states',
	'admin.usage.upstreamModelMismatchOnly': 'Mismatched only',
	'admin.usage.upstreamModelMatchedOnly': 'Matched only',
  'admin.usage.group': 'Group',
  'admin.usage.allGroups': 'All Groups',
  'common.refresh': 'Refresh',
  'common.reset': 'Reset',
  'admin.usage.cleanup.button': 'Cleanup',
  'usage.exportExcel': 'Export',
}

// Mock vue-i18n
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

// Mock the admin API module — we control searchUsers return value per test
const mockSearchUsers = vi.fn()
const mockSearchApiKeys = vi.fn().mockResolvedValue([])
const mockGroupsList = vi.fn().mockResolvedValue({ items: [] })
const mockUsageList = vi.fn().mockResolvedValue({ items: [], total: 0 })
const mockGetStats = vi.fn().mockResolvedValue({})
const mockGetCacheHitRate = vi.fn().mockResolvedValue({ items: [] })
const mockGetSnapshot = vi.fn().mockResolvedValue({ trend: [], groups: [] })
const mockGetModelStats = vi.fn().mockResolvedValue({ models: [] })
const mockAccountsList = vi.fn().mockResolvedValue({ items: [] })

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      list: (...args: any[]) => mockUsageList(...args),
      getStats: (...args: any[]) => mockGetStats(...args),
      getCacheHitRate: (...args: any[]) => mockGetCacheHitRate(...args),
      searchUsers: (...args: any[]) => mockSearchUsers(...args),
      searchApiKeys: (...args: any[]) => mockSearchApiKeys(...args),
    },
    groups: { list: (...args: any[]) => mockGroupsList(...args) },
    dashboard: { getModelStats: (...args: any[]) => mockGetModelStats(...args), getSnapshotV2: (...args: any[]) => mockGetSnapshot(...args) },
    accounts: { list: (...args: any[]) => mockAccountsList(...args) },
  },
}))

// Default props helper
const defaultFilters = () => ({
  user_id: undefined,
  api_key_id: undefined,
  account_id: undefined,
  model: null,
  request_type: null,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
	upstream_model_mismatch: null,
  group_id: null,
  start_date: '',
  end_date: '',
})

function mountFilters(filters = defaultFilters()) {
  return mount(UsageFilters, {
    props: {
      modelValue: filters,
      exporting: false,
      startDate: '2026-05-01',
      endDate: '2026-05-28',
      showActions: false,
      modelOptions: [],
    },
    global: {
      stubs: {
        Select: true,
        Teleport: true,
      },
    },
  })
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

describe('UsageFilters — user search dropdown', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockSearchUsers.mockReset()
    mockSearchApiKeys.mockResolvedValue([])
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens ranked account choices without a lookup and supports keyboard selection', async () => {
    mockAccountsList.mockClear()
    const filters = defaultFilters()
    const wrapper = mount(UsageFilters,{props:{modelValue:filters,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28',accountCandidates:[{id:5,name:'Popular',requests:20},{id:2,name:'Other',requests:3}]},global:{stubs:{Select:true,Teleport:true}}})
    const input = wrapper.findAll('input[type="text"]')[2]
    await input.trigger('focus')
    expect(wrapper.text()).toContain('Popular')
    expect(mockAccountsList).not.toHaveBeenCalled()
    await input.trigger('keydown',{key:'ArrowDown'})
    await input.trigger('keydown',{key:'Enter'})
    expect(filters.account_id).toBe(5)
    expect(wrapper.emitted('change')).toHaveLength(1)
    wrapper.unmount()
  })

  it('clears stale user and key choices while typing, and ignores replies after Escape', async () => {
    const userPending = deferred<Array<{ id:number; email:string; deleted:boolean }>>()
    const keyPending = deferred<Array<{ id:number; name:string; user_id:number }>>()
    mockSearchUsers.mockResolvedValueOnce([{id:1,email:'old@example.test',deleted:false}]).mockReturnValueOnce(userPending.promise)
    mockSearchApiKeys.mockReset().mockResolvedValueOnce([{id:1,name:'old-key',user_id:1}]).mockReturnValueOnce(keyPending.promise)
    const w = mount(UsageFilters,{props:{modelValue:defaultFilters(),showActions:true,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28'},global:{stubs:{Select:true,Teleport:true}}})
    try {
      const [user,key] = w.findAll('input[type="text"]')
      await user.setValue('old'); await vi.advanceTimersByTimeAsync(300)
      expect(w.text()).toContain('old@example.test')
      await user.setValue('new')
      expect(w.text()).not.toContain('old@example.test')
      await vi.advanceTimersByTimeAsync(300)
      await user.trigger('keydown',{key:'Escape'})
      userPending.resolve([{id:2,email:'late@example.test',deleted:false}]); await flushPromises()
      expect((w.vm as any).userResults).toEqual([])
      await key.setValue('old'); await vi.advanceTimersByTimeAsync(300)
      expect(w.text()).toContain('old-key')
      await key.setValue('new')
      expect(w.text()).not.toContain('old-key')
      await vi.advanceTimersByTimeAsync(300)
      await key.trigger('keydown',{key:'Escape'})
      keyPending.resolve([{id:2,name:'late-key',user_id:1}]); await flushPromises()
      expect((w.vm as any).apiKeyResults).toEqual([])
    } finally {w.unmount()}
  })

  it('reuses a completed identical user search after Escape and cancels a pending search on Tab away', async () => {
    const pending = deferred<Array<{id:number;email:string;deleted:boolean}>>()
    mockSearchUsers.mockResolvedValueOnce([{id:1,email:'same@example.test',deleted:false}]).mockReturnValueOnce(pending.promise)
    const w=mount(UsageFilters,{props:{modelValue:defaultFilters(),showActions:true,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28'},global:{stubs:{Select:true,Teleport:true}}})
    try {
      const [user,key]=w.findAll('input[type="text"]')
      await user.setValue('same');await vi.advanceTimersByTimeAsync(300)
      expect(w.text()).toContain('same@example.test')
      await user.trigger('keydown',{key:'Escape'})
      await user.trigger('focus');await vi.advanceTimersByTimeAsync(300)
      expect(w.text()).toContain('same@example.test')
      expect(mockSearchUsers).toHaveBeenCalledTimes(1)
      await user.setValue('pending');await vi.advanceTimersByTimeAsync(300)
      await user.trigger('focusout',{relatedTarget:key.element})
      pending.resolve([{id:2,email:'late@example.test',deleted:false}]);await flushPromises()
      expect((w.vm as any).userResults).toEqual([])
      expect(w.text()).not.toContain('late@example.test')
    } finally {w.unmount()}
  })

  it('keeps a missing historical group label and leaves cleanup account search unchanged', async () => {
    const filters = {...defaultFilters(),group_id:42}
    const w = mount(UsageFilters,{props:{modelValue:filters,showActions:true,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28'},global:{stubs:{Select:true,Teleport:true}}})
    try {
      (w.vm as any).setGroupLabel('Old group')
      await flushPromises()
      expect((w.vm as any).groupOptions).toContainEqual({value:42,label:'Old group'})
      await w.setProps({showActions:false})
      await w.findAll('input[type="text"]')[2].trigger('focus')
      expect(w.text()).not.toContain('admin.usage.noAccountRanking')
      expect(w.findAll('[data-filter-choice]')).toHaveLength(0)
    } finally {w.unmount()}
  })

  it('selects a saved user without a user search and rejects an old key result', async () => {
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id:919019,role:'admin' } as User
    const historyKey = 'admin-user-query-history:919019'
    localStorage.setItem(historyKey, JSON.stringify([{ id:2,email:'saved@example.test',lastQueriedAt:1,pinned:true }]))
    const old = deferred<Array<{ id:number; name:string; user_id:number }>>()
    mockSearchApiKeys.mockImplementation((uid:number|undefined) => uid === 2 ? Promise.resolve([{id:22,name:'new-key',user_id:2}]) : old.promise)
    const filters = { ...defaultFilters(), user_id: 1 as number|undefined, api_key_id: 11 as number|undefined }
    const wrapper = mount(UsageFilters, { props:{modelValue:filters,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28',showActions:true},global:{plugins:[pinia],stubs:{Select:true,Teleport:true}} })
    try {
      const [userInput,keyInput] = wrapper.findAll('input[type="text"]')
      await keyInput.setValue('old')
      await vi.advanceTimersByTimeAsync(300)
      await userInput.trigger('focus')
      const saved = wrapper.findAll('button').find(b=>b.text().includes('saved@example.test'))
      expect(saved).toBeDefined();await saved!.trigger('click');await flushPromises()
      expect(filters.user_id).toBe(2);expect(filters.api_key_id).toBeUndefined()
      expect(mockSearchUsers).not.toHaveBeenCalled()
      expect(mockSearchApiKeys).not.toHaveBeenCalledWith(2, '')
      await keyInput.trigger('focus'); await vi.advanceTimersByTimeAsync(300)
      old.resolve([{id:11,name:'old-key',user_id:1}]);await flushPromises()
      expect((wrapper.vm as any).apiKeyResults).toEqual([{id:22,name:'new-key',user_id:2}])
      expect(wrapper.emitted('change')?.length).toBe(1)
    } finally {wrapper.unmount();localStorage.removeItem(historyKey)}
  })

  it.each(['success','failure'])('ignores a previous user key-search reply after selecting history (%s)', async outcome => {
    const pinia=createPinia(), auth=useAuthStore(pinia)
    auth.user={id:919020,role:'admin'} as User
    const historyKey='admin-user-query-history:919020'
    localStorage.setItem(historyKey,JSON.stringify([{id:2,email:'saved@example.test',lastQueriedAt:1,pinned:true}]))
    const old=deferred<Array<{id:number;name:string;user_id:number}>>()
    mockSearchUsers.mockResolvedValue([{id:1,email:'first@example.test',deleted:false}])
    mockSearchApiKeys.mockImplementation((uid:number|undefined)=>uid===2?Promise.resolve([{id:22,name:'new-key',user_id:2}]):old.promise)
    const filters={...defaultFilters(),user_id:undefined as number|undefined,api_key_id:undefined as number|undefined}
    const w=mount(UsageFilters,{props:{modelValue:filters,exporting:false,startDate:'2026-05-01',endDate:'2026-05-28',showActions:true},global:{plugins:[pinia],stubs:{Select:true,Teleport:true}}})
    try {
      const input=w.find('input[type="text"]')
      await input.trigger('focus');await input.setValue('first');await vi.advanceTimersByTimeAsync(300)
      await w.findAll('button').find(b=>b.text().includes('first@example.test'))!.trigger('click')
      expect(filters.user_id).toBe(1)
      const keyInput = w.findAll('input[type="text"]')[1]
      await keyInput.trigger('focus'); await vi.advanceTimersByTimeAsync(300)
      await input.setValue('');await input.trigger('focus')
      await w.findAll('button').find(b=>b.text().includes('saved@example.test'))!.trigger('click');await flushPromises()
      const count=w.emitted('change')?.length
      await keyInput.trigger('focus'); await vi.advanceTimersByTimeAsync(300)
      if(outcome==='success') old.resolve([{id:11,name:'old-key',user_id:1}]);else old.reject(new Error('old query failed'))
      await flushPromises()
      expect(filters.user_id).toBe(2)
      expect((w.vm as any).apiKeyResults).toEqual([{id:22,name:'new-key',user_id:2}])
      expect(w.emitted('change')?.length).toBe(count)
      await w.setProps({showActions:false});await input.setValue('');await input.trigger('focus')
      expect(w.findAll('button').some(b=>b.text().includes('saved@example.test'))).toBe(false)
    } finally {w.unmount();localStorage.removeItem(historyKey)}
  })

  it('cancels a queued key search on parent filter reset', async () => {
    mockSearchApiKeys.mockClear()
    const w=mount(UsageFilters,{props:{modelValue:{...defaultFilters(),user_id:1},exporting:false,startDate:'2026-05-01',endDate:'2026-05-28',showActions:false},global:{stubs:{Select:true,Teleport:true}}})
    try {
      await w.findAll('input[type="text"]')[1].setValue('old')
      await w.setProps({modelValue:defaultFilters()})
      await vi.advanceTimersByTimeAsync(350)
      expect(mockSearchApiKeys).not.toHaveBeenCalled()
    } finally {w.unmount()}
  })

  it('resets a global key search which has no selected user or key', async () => {
    mockSearchApiKeys.mockClear()
    const w=mountFilters()
    try {
      await w.setProps({showActions:true})
      const keyInput=w.findAll('input[type="text"]')[1]
      await keyInput.setValue('old global key')
      await w.findAll('button').find(b=>b.text()==='Reset')!.trigger('click')
      await vi.advanceTimersByTimeAsync(350)
      expect(mockSearchApiKeys).not.toHaveBeenCalled()
      expect((keyInput.element as HTMLInputElement).value).toBe('')
      expect(w.emitted('reset')?.length).toBe(1)
    } finally {w.unmount()}
  })

  it('(a) labels deleted users with the i18n badge and (b) sorts active users before deleted ones, (c) selection sets user_id', async () => {
    // Arrange: mock returns deleted FIRST (proves sorting re-orders to active-first)
    mockSearchUsers.mockResolvedValue([
      { id: 2, email: 'gone@test.com', deleted: true },
      { id: 1, email: 'active@test.com', deleted: false },
    ])

    const wrapper = mountFilters()

    // Trigger focus (sets showUserDropdown = true) then input (fires debounceUserSearch)
    const input = wrapper.find('input[type="text"]')
    await input.trigger('focus')
    await input.setValue('test')
    await input.trigger('input')

    // Advance debounce timer (300ms) then flush the resolved promise
    vi.advanceTimersByTime(300)
    await flushPromises()

    // --- (b) Sort: active user should appear BEFORE deleted user ---
    // Check the underlying component state via rendered DOM order
    const buttons = wrapper.findAll('.usage-filter-dropdown button[type="button"]')
    const emailTexts = buttons.map((b) => b.text())

    // active@test.com should be listed first
    const activeIdx = emailTexts.findIndex((t) => t.includes('active@test.com'))
    const deletedIdx = emailTexts.findIndex((t) => t.includes('gone@test.com'))
    expect(activeIdx).toBeGreaterThanOrEqual(0)
    expect(deletedIdx).toBeGreaterThanOrEqual(0)
    expect(activeIdx).toBeLessThan(deletedIdx)

    // --- (a) Label: deleted user's button shows the badge text ---
    const deletedButton = buttons[deletedIdx]
    expect(deletedButton.text()).toContain('deleted')

    // active user's button does NOT show the badge text
    const activeButton = buttons[activeIdx]
    expect(activeButton.text()).not.toContain('deleted')

    // --- (c) Selection: clicking active user button sets filters.user_id ---
    await activeButton.trigger('click')
    await flushPromises()

    // The component emits 'update:modelValue' or modifies filters.user_id via toRef
    // selectUser sets filters.value.user_id = u.id and emits 'change'
    const changeEmits = wrapper.emitted('change')
    expect(changeEmits).toBeTruthy()
    expect(changeEmits!.length).toBeGreaterThan(0)

    // Also confirm user_id was set by checking the emitted change came through
    // (the component uses toRef so modelValue is mutated in place and 'change' is emitted)
    expect(wrapper.props('modelValue').user_id).toBe(1)
  })

  it('keeps results from the latest user search when responses arrive out of order', async () => {
    const firstSearch = deferred<Array<{ id: number; email: string; deleted: boolean }>>()
    const secondSearch = deferred<Array<{ id: number; email: string; deleted: boolean }>>()
    mockSearchUsers
      .mockImplementationOnce(() => firstSearch.promise)
      .mockImplementationOnce(() => secondSearch.promise)

    const wrapper = mountFilters()
    const input = wrapper.find('input[type="text"]')
    await input.trigger('focus')

    await input.setValue('a')
    vi.advanceTimersByTime(300)
    await flushPromises()

    await input.setValue('ab')
    vi.advanceTimersByTime(300)
    await flushPromises()

    secondSearch.resolve([{ id: 2, email: 'ab@test.com', deleted: false }])
    await flushPromises()
    expect(wrapper.text()).toContain('ab@test.com')

    firstSearch.resolve([{ id: 1, email: 'a@test.com', deleted: false }])
    await flushPromises()
    expect(wrapper.text()).toContain('ab@test.com')
    expect(wrapper.text()).not.toContain('a@test.com')
  })

  it('does not restore stale user results after the search is cleared', async () => {
    const pendingSearch = deferred<Array<{ id: number; email: string; deleted: boolean }>>()
    mockSearchUsers.mockImplementationOnce(() => pendingSearch.promise)

    const wrapper = mountFilters()
    const input = wrapper.find('input[type="text"]')
    await input.trigger('focus')

    await input.setValue('stale')
    vi.advanceTimersByTime(300)
    await flushPromises()

    await input.setValue('')
    vi.advanceTimersByTime(300)
    await flushPromises()

    pendingSearch.resolve([{ id: 3, email: 'stale@test.com', deleted: false }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale@test.com')
  })
})

describe('UsageFilters — model options come from prop (no dup request)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockGetModelStats.mockClear()
    mockGroupsList.mockClear()
  })
  afterEach(() => { vi.useRealTimers() })

  it('does not call dashboard.getModelStats on mount and renders model options from prop', async () => {
    const wrapper = mount(UsageFilters, {
      props: {
        modelValue: defaultFilters(),
        exporting: false,
        startDate: '2026-05-01',
        endDate: '2026-05-28',
        showActions: false,
        modelOptions: ['claude-3', 'gpt-4o'],
      },
      global: { stubs: { Select: true, Teleport: true } },
    })
    await flushPromises()

    expect(mockGetModelStats).not.toHaveBeenCalled()

    const opts = (wrapper.vm as any).modelOptions as Array<{ value: string | null; label: string }>
    expect(opts.map((o) => o.value)).toEqual([null, 'claude-3', 'gpt-4o'])
  })
})

describe('UsageFilters — native compaction filter', () => {
  it('offers only All/Compaction and emits the independent boolean filter', async () => {
    const SelectStub = {
      name: 'Select',
      props: ['modelValue', 'options'],
      emits: ['update:modelValue', 'change'],
      template: '<div />',
    }
    const filters = defaultFilters()
    const wrapper = mount(UsageFilters, {
      props: {
        modelValue: filters,
        exporting: false,
        startDate: '2026-05-01',
        endDate: '2026-05-28',
        showActions: false,
        modelOptions: [],
      },
      global: { stubs: { Select: SelectStub, Teleport: true } },
    })

    const compactionSelect = wrapper.findAllComponents(SelectStub).find((select: any) =>
      (select.props('options') as Array<{ value: unknown }>).some((option) => option.value === true)
    )
    expect(compactionSelect).toBeDefined()
    expect(compactionSelect!.props('options')).toEqual([
      { value: null, label: 'All Requests' },
      { value: true, label: 'Compaction Only' },
    ])
    expect(compactionSelect!.props('options')).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ value: false })])
    )

    compactionSelect!.vm.$emit('update:modelValue', true)
    compactionSelect!.vm.$emit('change')
    await wrapper.vm.$nextTick()

    expect(filters.native_compaction_v2).toBe(true)
    expect(wrapper.emitted('change')).toBeTruthy()
  })
})


describe('UsageFilters — all applicable filters are visible', () => {
  it.each([
    ['usage', ['API Key', 'Type', 'Request Kind', 'Billing Type', 'Billing Mode', 'Upstream model audit']],
    ['ranking', ['API Key', 'Type', 'Request Kind', 'Billing Type']],
    ['errors', ['API Key', 'admin.ops.errorLog.type', 'usage.errors.category', 'admin.ops.errorLog.status']],
  ] as const)('shows all %s fields without a more-filters toggle', async (mode, labels) => {
    const wrapper = mount(UsageFilters, {
      props: { modelValue: defaultFilters(), exporting: false, startDate: '2026-05-01', endDate: '2026-05-28', showActions: true, flat: true, mode },
      global: { stubs: { Select: true, Teleport: true } },
    })
    try {
      await flushPromises()
      for (const label of labels) {
        const field = wrapper.findAll('label').find(element => element.text() === label)
        expect(field?.isVisible()).toBe(true)
      }
      expect(wrapper.text()).not.toContain('admin.ops.records.more')
      expect(wrapper.findComponent(AdminGroupPicker).exists()).toBe(true)
      expect(wrapper.findComponent(AdminGroupPicker).props('specialOptions')).toEqual([{ value: null, label: 'All Groups' }])
    } finally { wrapper.unmount() }
  })
})

describe('UsageView — account default group on initial entry', () => {
  const storageKey = 'account-filter-values'
  const group = { id: 42, name: 'Default group', platform: 'openai', rate_multiplier: 1 }
  beforeEach(() => {
    vi.useFakeTimers()
    mockGroupsList.mockReset().mockResolvedValue({ items: [group] })
    mockUsageList.mockReset().mockResolvedValue({ items: [], total: 0 })
    mockGetStats.mockReset().mockResolvedValue({})
    mockGetCacheHitRate.mockReset().mockResolvedValue({ items: [] })
    mockGetSnapshot.mockReset().mockResolvedValue({ trend: [], groups: [] })
    mockGetModelStats.mockReset().mockResolvedValue({ models: [] })
    localStorage.removeItem(storageKey)
  })
  afterEach(() => {
    mockGroupsList.mockReset().mockResolvedValue({ items: [] })
    localStorage.removeItem(storageKey)
    vi.mocked(Storage.prototype.setItem).mockRestore?.()
    vi.useRealTimers()
  })

  async function mountUsage(query: Record<string, string> = {}) {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push({ path: '/', query })
    return mount(UsageView, {
      global: {
        plugins: [createPinia(), router],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          UsageStatsCards: true, UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
          UserBalanceHistoryModal: true, Pagination: true, Select: true, DateRangePicker: true,
          Icon: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true,
          EndpointDistributionChart: true, UserTokenRanking: true, AccountPerformanceTable: true,
        },
      },
    })
  }

  it.each([
    [{ user_id: '7' }],
    [{ user_id: '7', start_date: '2026-05-01', end_date: '2026-05-28' }],
    [{ model: 'gpt-5' }],
    [{ account_id: '3' }],
  ])('does not apply the default group when entering with route filters %o', async query => {
    localStorage.setItem(storageKey, JSON.stringify({ default_group: '42' }))
    const wrapper = await mountUsage(query)
    try {
      await flushPromises()
      expect(wrapper.findComponent(UsageFilters).props('modelValue').group_id).toBeUndefined()
      expect(mockUsageList).toHaveBeenCalledWith(expect.not.objectContaining({ group_id: 42 }), expect.anything())
    } finally { wrapper.unmount() }
  })

  it('still applies the default group when only a date range is in the URL', async () => {
    localStorage.setItem(storageKey, JSON.stringify({ default_group: '42' }))
    const wrapper = await mountUsage({ start_date: '2026-05-01', end_date: '2026-05-28' })
    try {
      await flushPromises()
      expect(wrapper.findComponent(UsageFilters).props('modelValue').group_id).toBe(42)
    } finally { wrapper.unmount() }
  })

  it('preserves the route group even when a valid account default exists', async () => {
    localStorage.setItem(storageKey, JSON.stringify({ default_group: '42' }))
    const wrapper = await mountUsage({ group_id: '9' })
    try {
      await flushPromises()
      expect(wrapper.findComponent(UsageFilters).props('modelValue').group_id).toBe(9)
      expect(mockUsageList).toHaveBeenCalledWith(expect.objectContaining({ group_id: 9 }), expect.anything())
    } finally { wrapper.unmount() }
  })

  it('waits for the existing group fetch and applies the default exactly once; reset and clear still clear it', async () => {
    const pending = deferred<{ items: typeof group[] }>()
    mockGroupsList.mockReturnValueOnce(pending.promise)
    localStorage.setItem(storageKey, JSON.stringify({ default_group: '42' }))
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    const wrapper = await mountUsage()
    try {
      const filters = wrapper.findComponent(UsageFilters)
      expect(filters.props('modelValue').group_id).toBeUndefined()
      expect(mockUsageList).not.toHaveBeenCalled()
      pending.resolve({ items: [group] })
      await flushPromises()
      expect(filters.props('modelValue').group_id).toBe(42)
      expect(mockGroupsList).toHaveBeenCalledTimes(1)
      expect(mockUsageList).toHaveBeenCalledTimes(1)
      expect(mockUsageList).toHaveBeenCalledWith(expect.objectContaining({ group_id: 42 }), expect.anything())
      const reset = filters.findAll('button').find(button => button.text() === 'Reset')!
      await reset.trigger('click')
      await flushPromises()
      expect(filters.props('modelValue').group_id).toBeUndefined()
      const picker = filters.findComponent(AdminGroupPicker)
      picker.vm.$emit('update:modelValue', 42)
      picker.vm.$emit('change', 42, { value: 42, label: 'Default group' })
      await flushPromises()
      await filters.findAll('button').find(button => button.text() === 'admin.usage.clearKeepTime')!.trigger('click')
      await flushPromises()
      expect(filters.props('modelValue').group_id).toBeUndefined()
      expect(storageWrite.mock.calls.some(([key]) => key === storageKey)).toBe(false)
    } finally { wrapper.unmount() }
  })

  it('does not reapply the default after a reset while groups are still loading', async () => {
    const pending = deferred<{ items: typeof group[] }>()
    mockGroupsList.mockReturnValueOnce(pending.promise)
    localStorage.setItem(storageKey, JSON.stringify({ default_group: '42' }))
    const wrapper = await mountUsage()
    try {
      const filters = wrapper.findComponent(UsageFilters)
      await filters.findAll('button').find(button => button.text() === 'Reset')!.trigger('click')
      pending.resolve({ items: [group] })
      await flushPromises()
      expect(filters.props('modelValue').group_id).toBeUndefined()
      expect(mockUsageList).not.toHaveBeenCalledWith(expect.objectContaining({ group_id: 42 }), expect.anything())
    } finally { wrapper.unmount() }
  })

  it.each(['', 'ungrouped', '0', '01', '-1', '999', 42, null])('ignores invalid or missing default %s without a write', async value => {
    const raw = JSON.stringify({ default_group: value })
    localStorage.setItem(storageKey, raw)
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    const wrapper = await mountUsage()
    try {
      await flushPromises()
      expect(wrapper.findComponent(UsageFilters).props('modelValue').group_id).toBeUndefined()
      expect(mockUsageList).toHaveBeenCalledWith(expect.not.objectContaining({ group_id: expect.any(Number) }), expect.anything())
      expect(localStorage.getItem(storageKey)).toBe(raw)
      expect(storageWrite.mock.calls.some(([key]) => key === storageKey)).toBe(false)
    } finally { wrapper.unmount() }
  })

  it('ignores malformed JSON without a write', async () => {
    localStorage.setItem(storageKey, '{invalid')
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    const wrapper = await mountUsage()
    try {
      await flushPromises()
      expect(wrapper.findComponent(UsageFilters).props('modelValue').group_id).toBeUndefined()
      expect(localStorage.getItem(storageKey)).toBe('{invalid')
      expect(storageWrite.mock.calls.some(([key]) => key === storageKey)).toBe(false)
    } finally { wrapper.unmount() }
  })
})
