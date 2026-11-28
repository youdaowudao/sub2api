import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminGroup } from '@/types'
import GroupsView from '../GroupsView.vue'
import { adminAPI } from '@/api/admin'

const {
  listGroups,
  listAllForDisplay,
  updateSortOrder,
  getAllGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  showError,
  showSuccess,
  isCurrentStep,
  nextStep,
  authState,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  listAllForDisplay: vi.fn(),
  updateSortOrder: vi.fn(),
  getAllGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  authState: { isSimpleMode: false },
}))

const messages: Record<string, string> = {
  "admin.groups.sortMoveUp": "Up {name}",
  "admin.groups.sortMoveDown": "Down {name}",
  'admin.groups.columnSettings': 'Column Settings',
  'admin.groups.columns.name': 'Name',
  'admin.groups.columns.id': 'ID',
  'admin.groups.columns.platform': 'Platform',
  'admin.groups.columns.billingType': 'Billing Type',
  'admin.groups.columns.rateMultiplier': 'Rate Multiplier',
  'admin.groups.columns.type': 'Type',
  'admin.groups.columns.accounts': 'Accounts',
  'admin.groups.columns.capacity': 'Capacity',
  'admin.groups.columns.usage': 'Usage',
  'admin.groups.columns.status': 'Status',
  'admin.groups.columns.actions': 'Actions',
  'admin.groups.usageToday': 'Today',
  'admin.groups.usageYesterday': 'Yesterday',
  'admin.groups.usageTotal': 'Total',
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { getCacheHitRate: vi.fn().mockResolvedValue({ items: [] }) },
    groups: {
      list: listGroups,
      listAllForDisplay,
      getAll: getAllGroups,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder,
    },
    accounts: {
      list: listAccounts,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => (messages[key] ?? key).replace("{name}", params?.name ?? ""),
    }),
  }
})

const createGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 1,
  name: 'Core Anthropic',
  description: null,
  platform: 'anthropic',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 3,
  active_account_count: 2,
  rate_limited_account_count: 1,
  model_allowlist: undefined,
  sort_order: 10,
  ...overrides,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
<div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div>
      <div data-test="rows">{{ data.map((row) => row.name).join(',') }}</div>
      <div v-if="data.length" data-test="usage-cell">
        <slot name="cell-usage" :row="data[0]" />
      </div>
    </div>
  `,
}

const SelectStub = {
  props: ['modelValue', 'options', 'placeholder'],
  emits: ['update:modelValue', 'change', 'open'],
  methods: { select(event: Event) { const value = (event.target as HTMLSelectElement).value; const option = (this as any).options.find((o: any) => String(o.value ?? '') === value); if (option && !option.disabled) { (this as any).$emit('update:modelValue', option.value); (this as any).$emit('change', option.value, option) } } },
  template: `
    <select
      :value="modelValue"
      @click="$emit('open')"
      @change="select($event)"
    >
      <option v-for="option in options" :key="String(option.value)" :value="option.value" :disabled="option.disabled">
        {{ option.label }}
      </option>
    </select>
  `,
}

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show" :data-dialog="title"><slot /><slot name="footer" /></div>',
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: { props: ['show', 'message'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-confirm>{{ message }}<button data-confirm-yes @click="$emit(\'confirm\')">confirm</button><button data-confirm-no @click="$emit(\'cancel\')">cancel</button></div>' },
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: IconStub,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: { template: '<div><slot /></div>' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

const rows = [
  createGroup({ id: 3, name: 'OpenAI', platform: 'openai', sort_order: -100 }),
  createGroup({ id: 2, name: 'Later', sort_order: 10, status: 'inactive', is_exclusive: true }),
  createGroup({ id: 1, name: 'First', sort_order: 0 }),
]
let wrapper: Awaited<ReturnType<typeof mountView>>
afterEach(() => { wrapper?.unmount(); localStorage.clear() })
beforeEach(() => {
  vi.clearAllMocks(); authState.isSimpleMode = false
  listGroups.mockResolvedValue({ items: rows, total: 3, pages: 1, page: 1 })
  listAllForDisplay.mockResolvedValue(rows)
  updateSortOrder.mockResolvedValue({})
  getModelAllowlistCandidates.mockResolvedValue([])
  getUsageSummary.mockResolvedValue([]); getCapacitySummary.mockResolvedValue([])
  getLiveCapability.mockResolvedValue({ supported: false }); listAccounts.mockResolvedValue({ items: [] })
})
const dialog = () => wrapper.get('[data-dialog="admin.groups.sortOrder"]')
async function openSort() {
  await wrapper.get('button[title="admin.groups.sortOrder"]').trigger('click'); await flushPromises()
}
async function clickText(text: string) {
  const button = dialog().findAll('button').find(b => b.text() === text)
  expect(button).toBeDefined(); await button!.trigger('click'); await flushPromises()
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
describe('group order editor and isolated candidates', () => {
  it('moves only inside a platform and persists the full flattened order including inactive/exclusive groups', async () => {
    wrapper = await mountView(); await openSort()
    expect(listAllForDisplay).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })
    expect(dialog().findAll('[data-sort-id]').map(n => n.attributes('data-sort-id'))).toEqual(['3', '1', '2'])
    expect(dialog().get('[aria-label="Up First"]').attributes('disabled')).toBeDefined()
    expect(dialog().get('[aria-label="Down OpenAI"]').attributes('disabled')).toBeDefined()
    await dialog().get('[aria-label="Up Later"]').trigger('click')
    await clickText('common.save')
    expect(updateSortOrder).toHaveBeenCalledTimes(1)
    expect(updateSortOrder).toHaveBeenCalledWith([{ id: 3, sort_order: 0 }, { id: 2, sort_order: 10 }, { id: 1, sort_order: 20 }])
    expect(rows.map(g => g.id)).toEqual([3, 2, 1])
    listAllForDisplay.mockResolvedValueOnce([{ ...rows[1]!, sort_order: 0 }, { ...rows[2]!, sort_order: 10 }, rows[0]!])
    await openSort()
    expect(dialog().findAll('[data-sort-id]').map(n => n.attributes('data-sort-id'))).toEqual(['3', '2', '1'])
    await clickText('common.cancel')
    expect(updateSortOrder).toHaveBeenCalledTimes(1)
  })
  it('guards empty, loading, failed and late snapshots; a closed editor never reopens', async () => {
    listAllForDisplay.mockResolvedValueOnce([])
    wrapper = await mountView(); await openSort()
    expect(dialog().text()).toContain('admin.groups.sortOrderEmpty')
    expect(dialog().findAll('button').find(b => b.text() === 'common.save')!.attributes('disabled')).toBeDefined()
    expect(updateSortOrder).not.toHaveBeenCalled()
    await clickText('common.cancel')
    listAllForDisplay.mockRejectedValueOnce(new Error('offline'))
    await openSort(); expect(dialog().text()).toContain('admin.groups.sortOrderLoadFailed')
    const pending = deferred<typeof rows>(); listAllForDisplay.mockReturnValueOnce(pending.promise)
    await clickText('common.tryAgain')
    await clickText('common.cancel'); pending.resolve(rows); await flushPromises()
    expect(wrapper.find('[data-dialog="admin.groups.sortOrder"]').exists()).toBe(false)
  })
  it('retains drafts on unconfirmed saves, prevents concurrent save/close and confirms reload', async () => {
    const pending = deferred<object>(); updateSortOrder.mockReturnValueOnce(pending.promise)
    wrapper = await mountView(); await openSort()
    await dialog().get('[aria-label="Up Later"]').trigger('click')
    await clickText('common.save')
    expect(dialog().findAll('button').every(b => b.attributes('disabled') !== undefined)).toBe(true)
    expect(updateSortOrder).toHaveBeenCalledTimes(1)
    pending.resolve({}); await flushPromises()
    await openSort(); updateSortOrder.mockRejectedValueOnce(new Error('timeout'))
    await dialog().get('[aria-label="Up Later"]').trigger('click'); await clickText('common.save')
    expect(dialog().text()).toContain('admin.groups.sortOrderUnconfirmed')
    expect(dialog().findAll('[data-sort-id]')[1]!.attributes('data-sort-id')).toBe('2')
    await clickText('common.refresh')
    expect(listAllForDisplay).toHaveBeenCalledTimes(2)
    await wrapper.get('[data-confirm-no]').trigger('click')
    expect(listAllForDisplay).toHaveBeenCalledTimes(2)
    await clickText('common.refresh'); await wrapper.get('[data-confirm-yes]').trigger('click'); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(3)
  })
  it('loads only on demand, retains values on failure and offers exclusive sources independently of the public table', async () => {
    listAllForDisplay.mockRejectedValueOnce(new Error('offline'))
    wrapper = await mountView()
    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click'); await flushPromises()
    const form = wrapper.get('#create-group-form')
    const name = form.findAll('input').find(n => n.attributes('required') !== undefined)!
    await name.setValue('My group')
    expect(listAllForDisplay).not.toHaveBeenCalled()
    expect(wrapper.get('button[form="create-group-form"]').attributes('disabled')).toBeUndefined()
    const select = form.get('#create-copy-source')
    await select.trigger('click'); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(1)
    expect(select.text()).toContain('admin.groups.formCandidatesFailed')
    expect(wrapper.get('button[form="create-group-form"]').attributes('disabled')).toBeUndefined()
    const exclusive = createGroup({ id: 99, name: 'Exclusive source', is_exclusive: true })
    listAllForDisplay.mockResolvedValueOnce([exclusive])
    await select.trigger('click'); await flushPromises()
    expect(name.element.value).toBe('My group')
    expect(select.text()).toContain('Exclusive source')
    await form.get('#create-invalid-fallback').trigger('click'); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(2)
    expect(listGroups.mock.calls[0]![2].is_exclusive).toBe(false)
  })
  it('shares a pending candidate request and allows an unrelated create to save after failure', async () => {
    const pending = deferred<AdminGroup[]>()
    listAllForDisplay.mockReturnValueOnce(pending.promise)
    const group = createGroup({ id: 99, name: 'Created' })
    vi.mocked(adminAPI.groups.create).mockResolvedValueOnce(group)
    wrapper = await mountView()
    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
    const form = wrapper.get('#create-group-form')
    await form.findAll('input').find(n => n.attributes('required') !== undefined)!.setValue('Created')
    ;(wrapper.vm as any).createForm.claude_code_only = true
    await flushPromises()
    await form.get('#create-copy-source').trigger('click')
    await form.get('#create-fallback').trigger('click')
    await form.get('#create-invalid-fallback').trigger('click')
    expect(listAllForDisplay).toHaveBeenCalledTimes(1)
    expect(form.get('#create-fallback').text()).toContain('common.loading')
    expect(form.get('#create-invalid-fallback').text()).toContain('common.loading')
    pending.resolve([]); await flushPromises()
    expect(form.get('#create-fallback').text()).toContain('admin.groups.formCandidatesEmpty')
    expect(form.get('#create-invalid-fallback').text()).toContain('admin.groups.formCandidatesEmpty')
    ;(wrapper.vm as any).refreshGroups(); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(1)
    listAllForDisplay.mockRejectedValueOnce(new Error('offline'))
    await form.get('#create-fallback').trigger('click'); await flushPromises()
    expect(form.get('#create-fallback').text()).toContain('admin.groups.formCandidatesFailed')
    expect(form.get('#create-invalid-fallback').text()).toContain('admin.groups.formCandidatesFailed')
    await form.trigger('submit'); await flushPromises()
    expect(adminAPI.groups.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Created' }))
    await (wrapper.vm as any).handleEdit(rows[2]); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(2)
    listAllForDisplay.mockResolvedValueOnce([group]); await wrapper.get('#edit-copy-source').trigger('click'); await flushPromises()
    expect(listAllForDisplay).toHaveBeenCalledTimes(3)
    expect(wrapper.get('#edit-copy-source').text()).toContain('Created')
  })
  it('saves a normal edit without loading candidates and preserves saved fallback IDs', async () => {
    const original = createGroup({ id: 8, fallback_group_id: 92, fallback_group_id_on_invalid_request: 93, claude_code_only: true })
    vi.mocked(adminAPI.groups.update).mockResolvedValueOnce(original)
    wrapper = await mountView()
    await (wrapper.vm as any).handleEdit(original); await flushPromises()
    expect(listAllForDisplay).not.toHaveBeenCalled()
    expect(wrapper.get('button[form="edit-group-form"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
    expect(adminAPI.groups.update).toHaveBeenCalledWith(8, expect.objectContaining({ fallback_group_id: 92, fallback_group_id_on_invalid_request: 93, copy_accounts_from_group_ids: [] }))
    expect(listAllForDisplay).not.toHaveBeenCalled()
  })
  it('sorts selected copy-source labels while sending the original click order', async () => {
    const first = createGroup({ id: 1, name: 'First', sort_order: 0 }), later = createGroup({ id: 2, name: 'Later', sort_order: 10 })
    listAllForDisplay.mockResolvedValueOnce([later, first])
    vi.mocked(adminAPI.groups.create).mockResolvedValueOnce(first)
    wrapper = await mountView()
    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click'); await flushPromises()
    const form = wrapper.get('#create-group-form')
    await form.findAll('input').find(n => n.attributes('required') !== undefined)!.setValue('Copied group')
    const select = form.get('#create-copy-source')
    await select.trigger('click'); await flushPromises()
    await select.setValue('2'); await select.setValue('1')
    const labels = form.findAll('span.bg-primary-100').filter(n => /^(First|Later)/.test(n.text()))
    expect(labels.map(n => n.text().split(' - ')[0])).toEqual(['First', 'Later'])
    expect((wrapper.vm as any).createForm.copy_accounts_from_group_ids).toEqual([2, 1])
    await form.trigger('submit'); await flushPromises()
    expect(adminAPI.groups.create).toHaveBeenCalledWith(expect.objectContaining({ copy_accounts_from_group_ids: [2, 1] }))
  })

  it('rejects late form candidates after closing and switching the edited group', async () => {
    const old = deferred<typeof rows>(), current = deferred<typeof rows>()
    listAllForDisplay.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    wrapper = await mountView()
    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
    await wrapper.get('#create-copy-source').trigger('click')
    ;(wrapper.vm as any).closeCreateModal()
    await (wrapper.vm as any).handleEdit(rows[2]); await flushPromises()
    await wrapper.get('#edit-copy-source').trigger('click')
    old.resolve(rows); await flushPromises()
    expect(wrapper.get('button[form="edit-group-form"]').attributes('disabled')).toBeUndefined()
    expect((wrapper.vm as any).formCandidateGroups).toEqual([])
    current.resolve([createGroup({ id: 99, name: 'Current exclusive candidate', is_exclusive: true })]); await flushPromises()
    expect(wrapper.get('#edit-group-form').text()).toContain('Current exclusive candidate')
    expect((wrapper.vm as any).formCandidateGroups.map((g: { id: number }) => g.id)).toEqual([99])
    ;(wrapper.vm as any).closeEditModal()
    await (wrapper.vm as any).handleEdit(rows[2]); await flushPromises()
    expect((wrapper.vm as any).formCandidateGroups.map((g: { id: number }) => g.id)).toEqual([99])
    expect(listAllForDisplay).toHaveBeenCalledTimes(2)
  })

  it('does not let a late complete main response overwrite a newer candidate snapshot', async () => {
    wrapper = await mountView()
    await (wrapper.vm as any).handleEdit(rows[2]); await flushPromises()
    const main = deferred<{ items: AdminGroup[]; total: number; pages: number; page: number }>()
    listGroups.mockReturnValueOnce(main.promise)
    ;(wrapper.vm as any).filters.is_exclusive = ''
    void (wrapper.vm as any).loadGroups()
    const fresh = createGroup({ id: 99, name: 'Fresh source' })
    listAllForDisplay.mockResolvedValueOnce([fresh])
    await wrapper.get('#edit-copy-source').trigger('click'); await flushPromises()
    main.resolve({ items: rows, total: rows.length, pages: 1, page: 1 }); await flushPromises()
    expect((wrapper.vm as any).formCandidateGroups.map((g: AdminGroup) => g.id)).toEqual([99])
    expect(wrapper.get('#edit-copy-source').text()).toContain('Fresh source')
  })

  it('reuses a complete unfiltered result obtained during the form without another request', async () => {
    wrapper = await mountView(); await (wrapper.vm as any).handleEdit(rows[2]); await flushPromises()
    ;(wrapper.vm as any).filters.is_exclusive = ''
    await (wrapper.vm as any).loadGroups(); await flushPromises()
    await wrapper.get('#edit-copy-source').trigger('click'); await flushPromises()
    expect(listAllForDisplay).not.toHaveBeenCalled()
    expect(wrapper.get('#edit-copy-source').text()).toContain('Later')
  })

  it('requires valid selected sources after expiry and restores the original fallback on re-read failure', async () => {
    const original = createGroup({ id: 10, claude_code_only: true, fallback_group_id: 1 })
    const fallback = createGroup({ id: 9, name: 'New fallback' })
    const clock = vi.spyOn(Date, 'now').mockReturnValue(1000)
    try {
      wrapper = await mountView(); await (wrapper.vm as any).handleEdit(original); await flushPromises()
      listAllForDisplay.mockResolvedValueOnce([rows[2]!, fallback])
      const select = wrapper.get('#edit-fallback')
      await select.trigger('click'); await flushPromises(); await select.setValue('9')
      expect((wrapper.vm as any).editForm.fallback_group_id).toBe(9)
      clock.mockReturnValue(1000 + 24 * 60 * 60 * 1000 + 1)
      await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
      expect(adminAPI.groups.update).not.toHaveBeenCalled()
      expect(listAllForDisplay).toHaveBeenCalledTimes(1)
      listAllForDisplay.mockRejectedValueOnce(new Error('offline'))
      await select.trigger('click'); await flushPromises()
      expect(select.text()).toContain('admin.groups.formCandidatesFailed')
      await select.setValue('1')
      expect((wrapper.vm as any).editForm.fallback_group_id).toBe(1)
      vi.mocked(adminAPI.groups.update).mockResolvedValueOnce(original)
      await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
      expect(adminAPI.groups.update).toHaveBeenCalledWith(10, expect.objectContaining({ fallback_group_id: 1 }))
    } finally { clock.mockRestore() }
  })

  it('keeps all/public/exclusive requests distinct and resets pagination when changing the type filter', async () => {
    wrapper = await mountView()
    const filter = wrapper.findAll('select').find(n => n.text().includes('admin.groups.allGroups'))!
    ;(wrapper.vm as any).pagination.page = 3
    await filter.setValue(''); await flushPromises()
    expect(listGroups).toHaveBeenLastCalledWith(1, expect.any(Number), expect.objectContaining({ is_exclusive: undefined }), expect.anything())
    ;(wrapper.vm as any).pagination.page = 3
    await filter.setValue('true'); await flushPromises()
    expect(listGroups).toHaveBeenLastCalledWith(1, expect.any(Number), expect.objectContaining({ is_exclusive: true }), expect.anything())
  })

  it('does not request form candidates or block saving in simple mode', async () => {
    authState.isSimpleMode = true; wrapper = await mountView()
    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click'); await flushPromises()
    expect(listAllForDisplay).not.toHaveBeenCalled()
    expect(wrapper.get('button[form="create-group-form"]').attributes('disabled')).toBeUndefined()
    ;(wrapper.vm as any).closeCreateModal()
    await (wrapper.vm as any).handleEdit(rows[0]); await flushPromises()
    expect(listAllForDisplay).not.toHaveBeenCalled()
    expect(wrapper.get('button[form="edit-group-form"]').attributes('disabled')).toBeUndefined()
  })
})

describe('group display settings', () => {
  const createRequest = vi.mocked(adminAPI.groups.create)
  const updateRequest = vi.mocked(adminAPI.groups.update)
  const icon = (form: 'create' | 'edit') => wrapper.get<HTMLSelectElement>(`#${form}-group-display-icon`)
  const category = (form: 'create' | 'edit') => wrapper.get<HTMLSelectElement>(`#${form}-group-display-category`)
  const showList = (group: AdminGroup) =>
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
  const openEditor = async () => {
    const edit = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    expect(edit).toBeDefined()
    await edit!.trigger('click')
    await flushPromises()
  }
  const submit = async (form: 'create' | 'edit') => {
    await wrapper.get(`#${form}-group-form`).trigger('submit')
    await flushPromises()
  }

  it.each([false, true])('creates display settings and edits persisted values (simple mode %s)', async (simple) => {
    authState.isSimpleMode = simple
    const group = createGroup({ platform: 'openai', display_icon: 'gemini', display_category: 'gemini' })
    showList(group)
    createRequest.mockResolvedValueOnce(group)
    updateRequest.mockResolvedValueOnce(group)
    wrapper = await mountView()

    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
    await flushPromises()
    expect(Array.from(icon('create').element.options, option => option.value)).toEqual([
      '', 'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
      'minimax', 'deepseek', 'opencode_go', 'command_code', 'cline',
    ])
    expect(Array.from(category('create').element.options, option => option.value))
      .toEqual(['', 'openai', 'anthropic', 'gemini', 'domestic', 'image'])
    expect(icon('create').element.value).toBe('')
    expect(category('create').element.value).toBe('')
    await wrapper.get('[data-tour="group-form-name"]').setValue('Compatible group')
    await icon('create').setValue('gemini')
    await category('create').setValue('image')
    await submit('create')
    expect(createRequest).toHaveBeenCalledTimes(1)
    expect(createRequest).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Compatible group', display_icon: 'gemini', display_category: 'image',
    }))
    expect(wrapper.find('#create-group-form').exists()).toBe(false)

    await openEditor()
    expect(icon('edit').element.value).toBe('gemini')
    expect(category('edit').element.value).toBe('gemini')
    await icon('edit').setValue('deepseek')
    await category('edit').setValue('domestic')
    await submit('edit')
    expect(updateRequest).toHaveBeenCalledTimes(1)
    expect(updateRequest).toHaveBeenCalledWith(group.id, expect.objectContaining({
      display_icon: 'deepseek', display_category: 'domestic',
    }))
    expect(wrapper.find('#edit-group-form').exists()).toBe(false)
  })

  it('round-trips simple-mode updates and explicit follow-platform selections through reloads', async () => {
    authState.isSimpleMode = true
    const group = createGroup({ platform: 'openai', display_icon: '  gemini  ', display_category: ' gemini ' })
    showList(group)
    updateRequest.mockImplementation(async (_id, data) => {
      const saved = { ...group, display_icon: data.display_icon, display_category: data.display_category }
      showList(saved)
      return saved
    })
    try {
      wrapper = await mountView()
      await openEditor()
      expect(icon('edit').element.value).toBe('gemini')
      expect(category('edit').element.value).toBe('gemini')
      await icon('edit').setValue('deepseek')
      await category('edit').setValue('image')
      await submit('edit')
      expect(listGroups).toHaveBeenCalledTimes(2)
      await openEditor()
      expect(icon('edit').element.value).toBe('deepseek')
      expect(category('edit').element.value).toBe('image')
      await icon('edit').setValue('')
      await category('edit').setValue('')
      await submit('edit')
      expect(updateRequest).toHaveBeenLastCalledWith(group.id, expect.objectContaining({ display_icon: '', display_category: '' }))
      expect(listGroups).toHaveBeenCalledTimes(3)
      await openEditor()
      expect(icon('edit').element.value).toBe('')
      expect(category('edit').element.value).toBe('')
    } finally {
      updateRequest.mockReset()
    }
  })

  it.each([false, true])('shows follow platform for invalid stored strings and keeps values after failure (simple mode %s)', async (simple) => {
    authState.isSimpleMode = simple
    const group = createGroup({ platform: 'openai', display_icon: 'typesafe', display_category: 'xyz' })
    showList(group)
    updateRequest.mockRejectedValueOnce(new Error('Save failed')).mockResolvedValueOnce(group)
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      wrapper = await mountView()
      await openEditor()
      expect(icon('edit').element.selectedOptions[0].textContent).toBe('admin.groups.form.followPlatform')
      expect(category('edit').element.selectedOptions[0].textContent).toBe('admin.groups.form.followPlatform')
      await wrapper.get('[data-tour="edit-group-form-name"]').setValue('Renamed group')
      await submit('edit')
      expect(showError).toHaveBeenCalled()
      expect(wrapper.get<HTMLInputElement>('[data-tour="edit-group-form-name"]').element.value).toBe('Renamed group')
      expect(icon('edit').element.value).toBe('')
      expect(category('edit').element.value).toBe('')
      await submit('edit')
      expect(updateRequest).toHaveBeenLastCalledWith(group.id, expect.objectContaining({
        name: 'Renamed group', display_icon: '', display_category: '',
      }))
      expect(wrapper.find('#edit-group-form').exists()).toBe(false)
    } finally {
      consoleError.mockRestore()
    }
  })
})

vi.mock('@/composables/useCacheHitRateDefaultRange', () => ({ useCacheHitRateDefaultRange: () => ({ dates: { value: { start: '2026-10-26', end: '2026-10-26' } } }) }))
