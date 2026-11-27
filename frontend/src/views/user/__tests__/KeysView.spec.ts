import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import { bindGuideBusinessSession } from '@/features/guide/businessEvents'
import type { ApiKey, Group } from '@/types'
import { keysAPI } from '@/api'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { sortGroupsForDisplay } from '@/utils/groupDisplayOrder'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  updateKey,
  getKeyById,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
  routeState,
  routerReplace,
} = vi.hoisted(() => ({
  routeState: { query: {} as Record<string, string> },
  routerReplace: vi.fn(),
  listKeys: vi.fn(),
  updateKey: vi.fn(),
  getKeyById: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.providers.openai': 'OpenAI',
  'keys.providers.anthropic': 'Anthropic',
  'keys.providers.gemini': 'Gemini',
  'keys.providers.domestic': '国产模型',
  'keys.providers.image': '画图',
  'keys.providers.other': 'Grok',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
  'keys.namePlaceholder': '测试',
  'keys.importToCodexLine1': '一键导入',
  'keys.importImageApp': '生图工具',
  'keys.quickCreateHint': '直接点创建',
  'keys.advancedSettings': '高级设置（新手不用点开）',
  'keys.createNextStepHint': '创建后，还需要按接入教程，将密钥配置到你使用的软件中。',
}

vi.mock('@/api/auth', () => ({ getPublicSettings }))
vi.mock('@/api/keys', () => ({ keysAPI: { getById: getKeyById } }))

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => routeState, useRouter: () => ({ replace: routerReplace, push: vi.fn() }) }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: { columns: Array, data: Array, selectedKeys: Array, selectable: Boolean, loading: Boolean },
  emits: ['sort', 'update:selectedKeys'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <slot name="cell-key" :value="row.key" :row="row" />
        <slot name="cell-group" :row="row" />
        <slot name="cell-actions" :row="row" />
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div data-test="usage">
          <slot name="cell-usage" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
      <button data-test="page-2" @click="$emit('update:page', 2)">Page 2</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon" :data-icon="name">{{ $attrs["aria-hidden"] ? "" : name }}</span>',
}

const mountView = async (realDialog = false, realBadge = false) => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: realDialog ? BaseDialog : {
          props: ['show', 'title'],
          emits: ['close'],
          template: '<div v-if="show" role="dialog"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        BulkEditKeysModal: true,
        EndpointPopover: true,
        GroupBadge: !realBadge,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

const groupPicker = (wrapper: VueWrapper) => wrapper.get('[data-tour="key-group-dropdown"]')
const groupPickerTabs = (wrapper: VueWrapper) => groupPicker(wrapper).findAll('button[aria-pressed]')
const pickerTab = (wrapper: VueWrapper, label: string) => {
  const tab = groupPickerTabs(wrapper).find(button => button.text().replace(/ \d+$/, '') === label)
  expect(tab).toBeDefined()
  return tab!
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    updateKey.mockReset()
    vi.mocked(keysAPI.create).mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({ quick_import_image_group_id: -1 })
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
    routeState.query = {}
    routerReplace.mockReset()
  })

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
    wrapper.unmount()
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('opens bulk editing with only selected visible keys', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('selectable')).toBe(true)
    table.vm.$emit('update:selectedKeys', [1, 99])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'BulkEditKeysModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('selectedKeys').map((key: ApiKey) => key.id)).toEqual([1])
    wrapper.unmount()
  })

  it.each(['filter', 'page size', 'sort'])('clears selection on %s changes', async (change) => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    if (change === 'filter') {
      wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    } else if (change === 'page size') {
      await wrapper.get('[data-test="page-size-50"]').trigger('click')
    } else {
      table.vm.$emit('sort', 'created_at', 'asc')
    }
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    expect(wrapper.find('[data-test="bulk-edit-keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes successful keys from the selection and refreshes the table', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, name: 'Second' }],
      total: 2, pages: 1
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    wrapper.findComponent({ name: 'BulkEditKeysModal' }).vm.$emit('updated', [1])
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(table.props('selectedKeys')).toEqual([2])
    wrapper.unmount()
  })

  it('drops keys that are no longer visible after a refresh', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    listKeys.mockResolvedValue({ items: [], total: 0, pages: 0 })
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    wrapper.unmount()
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('shows the real destination and launches the right protocol for all supported group types', async () => {
    const row = (id: number, platform: string | null, status = 'active', allowImage = false) => ({
      ...createApiKey(),
      id,
      name: `k${id}`,
      key: `sk-fake-${id}`,
      group_id: platform ? id : null,
      status,
      group: platform ? {
        id, name: `g${id}`, platform, status: 'active', allow_image_generation: allowImage,
        display_icon: platform === 'gemini' ? 'kimi' : 'gemini',
        display_category: platform === 'gemini' ? 'domestic' : 'gemini',
      } : undefined,
    })
    const items = [
      row(1, 'anthropic'), row(2, 'openai'), row(3, 'openai', 'active', true),
      row(4, 'openai', 'active', true), row(5, 'gemini'), row(6, null), row(7, 'openai', 'inactive'),
      row(8, 'antigravity'), row(9, 'composite'), row(10, 'gemini', 'inactive')
    ]
    getPublicSettings.mockResolvedValue({ quick_import_image_group_id: 4 })
    getKeyById.mockImplementation(async (id: number) => items.find((item) => item.id === id))
    listKeys.mockResolvedValueOnce({ items, total: items.length, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView()
    const badges = wrapper.findAll('[data-tour="key-group"]').map(button => button.findComponent({ name: 'GroupBadge' }))
    expect(badges.map(badge => badge.exists() ? badge.props('platform') : null))
      .toEqual(['gemini', 'gemini', 'gemini', 'gemini', 'kimi', null, 'gemini', 'gemini', 'gemini', 'kimi'])
    const buttons = wrapper.findAll('[data-testid="codex-import"]')
    expect(buttons.map((b) => b.attributes('data-app'))).toEqual(['claude', 'codex', 'codex', 'image', 'antigravity'])
    const targets = ['Claude', 'Codex', 'Codex', '生图工具', 'Antigravity']
    const location = { href: '' }
    vi.stubGlobal('location', location)
    try {
      for (const [index, app] of ['claude', 'codex', 'codex', 'image', 'antigravity'].entries()) {
        const button = buttons[index]
        expect(button.text()).toBe(`一键导入${targets[index]}`)
        await button.trigger('click')
        await flushPromises()
        const id = index + 1
        expect(location.href).toBe(`usegoodai://import?v=${app === 'claude' ? 1 : 2}&app=${app}&key=sk-fake-${id}&name=k${id}`)
      }
      location.href = ''
      getPublicSettings.mockResolvedValue({ quick_import_image_group_id: 3 })
      await buttons[2].trigger('click')
      await flushPromises()
      expect(location.href).toBe('')
      expect(showError).toHaveBeenCalled()

      getKeyById.mockResolvedValueOnce({ ...items[4], group: { ...items[4].group, status: 'disabled' } })
      showError.mockClear()
      await buttons[4].trigger('click')
      await flushPromises()
      expect(location.href).toBe('')
      expect(showError).toHaveBeenCalledWith('keys.importChanged')
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('passes the real platform to the key tutorial with a different display icon and category', async () => {
    const group = { id: 2, name: 'Compatible group', platform: 'openai', display_icon: 'gemini', display_category: 'gemini' } as Group
    const key = { ...createApiKey(), group_id: group.id, group }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView()
    expect(wrapper.get('[data-tour="key-group"]').findComponent({ name: 'GroupBadge' }).props('platform')).toBe('gemini')
    await getButtonByText(wrapper, 'keys.useKey').trigger('click')
    const modal = wrapper.getComponent({ name: 'UseKeyModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('platform')).toBe('openai')
    wrapper.unmount()
  })

  it('switches the row import button between Codex and Claude after changing the key group', async () => {
    const openai = { id: 21, name: 'GPT 组', platform: 'openai', rate_multiplier: 1, status: 'active', subscription_type: 'standard', is_exclusive: false }
    const anthropic = { id: 22, name: 'Claude 组', platform: 'anthropic', rate_multiplier: 1, status: 'active', subscription_type: 'standard', is_exclusive: false }
    getAvailableGroups.mockResolvedValue([openai, anthropic])
    const before = { ...createApiKey(), id: 7, key: 'sk-7', name: 'k7', group_id: 21, group: openai }
    const page = (item: unknown) => ({ items: [item], total: 1, page: 1, page_size: 20, pages: 1 })
    listKeys.mockResolvedValueOnce(page(before))
    const wrapper = await mountView()
    expect(wrapper.get('[data-testid="codex-import"]').attributes('data-app')).toBe('codex')

    // 和真实页面一样：改分组后重新拉列表，新数据里带新分组
    updateKey.mockResolvedValueOnce({ ...before, group_id: 22, group: anthropic })
    listKeys.mockResolvedValueOnce(page({ ...before, group_id: 22, group: anthropic }))
    await wrapper.get('[data-tour="key-group"]').trigger('click')
    await nextTick()
    expect(pickerTab(wrapper, 'OpenAI').attributes('aria-pressed')).toBe('true')
    await pickerTab(wrapper, 'Anthropic').trigger('click')
    expect(updateKey).not.toHaveBeenCalled()
    const option = wrapper
      .findAll('[data-tour="key-group-option"]')
      .find((o) => o.findComponent({ name: 'GroupOptionItem' }).exists() && o.findComponent({ name: 'GroupOptionItem' }).props('name') === 'Claude 组')
    expect(option).toBeTruthy()
    await option!.trigger('click')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(updateKey).toHaveBeenCalledWith(7, { group_id: 22 })
    expect(wrapper.get('[data-testid="codex-import"]').attributes('data-app')).toBe('claude')
    expect(wrapper.get('[data-testid="codex-import"]').text()).toContain('Claude')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it.each([
    { key: 'current_concurrency', order: 'asc' },
    { key: 'group', order: 'asc' },
    { key: 'group', order: 'desc' },
  ] as const)('keeps filters and resets pagination and selection when sorting $key $order', async ({ key, order }) => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    expect(table.props('selectedKeys')).toEqual([1])
    expect(visibleColumnMeta(wrapper).find((column) => column.key === key)?.sortable).toBe(true)
    listKeys.mockClear()

    table.vm.$emit('sort', key, order)
    await flushPromises()

    expect(table.props('selectedKeys')).toEqual([])
    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: key,
        sort_order: order,
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  describe('create provider selection', () => {
    const platforms = ['anthropic', 'openai', 'kimi', 'zhipu', 'deepseek', 'minimax', 'gemini', 'grok', 'antigravity', 'composite', 'opencode_go', 'typesafe']
    const availableGroups = platforms.map((platform, index) => ({
      id: index + 1,
      // Deliberately ambiguous names: classification must follow the platform.
      name: `Shared group ${index + 1}`,
      platform,
      rate_multiplier: 1,
      subscription_type: 'standard',
    }))
    const groupSelect = (wrapper: VueWrapper) => wrapper.findComponent('[data-tour="key-form-group"]')
    const optionIds = (wrapper: VueWrapper, categoryLabels?: string[]) => {
      const options = groupSelect(wrapper).props('options') as { value: string | number; kind?: string; disabled?: boolean; label: string }[]
      const headings = options.filter(o => o.kind === 'group')
      const real = options.filter(o => o.kind !== 'group')
      expect(headings.every(o => o.disabled === true && typeof o.value === 'string')).toBe(true)
      if (categoryLabels) expect(headings.map(o => o.label)).toEqual(categoryLabels)
      return real.map(o => o.value)
    }
    const chooseProvider = (wrapper: VueWrapper, value: string) => wrapper.get(`input[name="key-provider"][value="${value}"]`).setValue()
    const openCreate = async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      return wrapper
    }

    beforeEach(() => {
      getAvailableGroups.mockResolvedValue(availableGroups)
    })

    it('collapses optional settings on create and submits their original defaults', async () => {
      const wrapper = await openCreate()
      expect(wrapper.get('summary').text()).toContain('高级设置（新手不用点开）')
      expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(false)
      expect(wrapper.get('[data-tour="key-form-quota"]').isVisible()).toBe(false)
      expect(wrapper.get('[data-tour="key-form-name"]').isVisible()).toBe(true)
      expect(wrapper.text()).toContain('创建后，还需要按接入教程，将密钥配置到你使用的软件中。')

      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), group_id: 2 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(keysAPI.create).toHaveBeenCalledWith('测试', 2, undefined, [], [], 0, undefined, {
        rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0,
      })
      wrapper.unmount()
    })

    it('puts the setup hint above the action row inside the real dialog footer', async () => {
      const wrapper = await mountView(true)
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      const footer = wrapper.get('.modal-footer')
      expect(footer.element.children).toHaveLength(1)
      const content = footer.get('.w-full')
      expect(content.element.children[0].textContent).toBe('创建后，还需要按接入教程，将密钥配置到你使用的软件中。')
      expect(content.element.children[1].querySelector('[data-tour="key-form-submit"]')).not.toBeNull()
      wrapper.unmount()
    })

    it('retains advanced settings after folding and retrying a failed creation', async () => {
      const wrapper = await openCreate()
      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      await wrapper.get('summary').trigger('click')
      const details = wrapper.get<HTMLDetailsElement>('details')
      expect(details.element.open).toBe(true)
      expect(wrapper.get('[data-tour="key-form-quota"]').isVisible()).toBe(true)
      await details.findAll('button')[0].trigger('click')
      const customKey = details.get<HTMLInputElement>('input[type="text"]')
      await customKey.setValue('custom-key-for-test')
      await details.findAll('button')[1].trigger('click')
      await details.findAll('textarea')[0].setValue('192.0.2.1')
      await details.findAll('textarea')[1].setValue('198.51.100.1')
      await details.get('[data-tour="key-form-quota"] input').setValue('12.5')
      await details.findAll('button')[2].trigger('click')
      const limits = details.findAll('input[type="number"]').slice(1)
      await limits[0].setValue('2')
      await limits[1].setValue('5')
      await limits[2].setValue('10')
      await details.findAll('button')[3].trigger('click')
      await details.get('input[type="datetime-local"]').setValue('2099-01-01T12:00')
      await wrapper.get('summary').trigger('click')
      expect(details.element.open).toBe(false)

      vi.mocked(keysAPI.create).mockRejectedValueOnce(new Error('creation failed'))
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(wrapper.find('#key-form').exists()).toBe(true)
      expect(details.element.open).toBe(false)
      expect(keysAPI.create).toHaveBeenLastCalledWith('测试', 2, 'custom-key-for-test', ['192.0.2.1'], ['198.51.100.1'], 12.5, expect.any(Number), {
        rate_limit_5h: 2, rate_limit_1d: 5, rate_limit_7d: 10,
      })
      const firstRequest = vi.mocked(keysAPI.create).mock.calls[0]
      await wrapper.get('summary').trigger('click')
      expect(customKey.element.value).toBe('custom-key-for-test')
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-quota"] input').element.value).toBe('12.5')
      await wrapper.get('summary').trigger('click')
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), group_id: 2 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(keysAPI.create).toHaveBeenLastCalledWith(...firstRequest)
      expect(wrapper.find('#key-form').exists()).toBe(false)
      wrapper.unmount()
    })

    it.each(['', 'too-short'])('reveals a folded invalid custom key (%s) without creating', async (value) => {
      const wrapper = await openCreate()
      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      await wrapper.get('summary').trigger('click')
      await wrapper.get('details button').trigger('click')
      await wrapper.get('details input[type="text"]').setValue(value)
      await wrapper.get('summary').trigger('click')
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(true)
      expect(wrapper.get('details input[type="text"]').isVisible()).toBe(true)
      expect(showError).toHaveBeenCalledWith(value ? 'keys.customKeyTooShort' : 'keys.customKeyRequired')
      expect(keysAPI.create).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('reveals a folded limit when native input validation fails', async () => {
      const wrapper = await openCreate()
      await wrapper.get('summary').trigger('click')
      await wrapper.get('[data-tour="key-form-quota"] input').setValue('-1')
      await wrapper.get('summary').trigger('click')
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-quota"] input').element.checkValidity()).toBe(false)
      expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(true)
      expect(wrapper.get('[data-tour="key-form-quota"]').isVisible()).toBe(true)
      expect(keysAPI.create).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('reopens a fresh create folded and keeps existing settings visible when editing', async () => {
      const key = { ...createApiKey(), group_id: 2, quota: 12.5, ip_whitelist: ['192.0.2.1'], rate_limit_1d: 5, expires_at: '2099-01-01T12:00:00.000Z' }
      listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
      const wrapper = await openCreate()
      await wrapper.get('summary').trigger('click')
      await wrapper.get('[data-tour="key-form-quota"] input').setValue('99')
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(false)
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-quota"] input').element.value).toBe('')
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.get<HTMLDetailsElement>('details').element.open).toBe(true)
      expect(wrapper.get('summary').isVisible()).toBe(false)
      expect(wrapper.text()).not.toContain('创建后，还需要按接入教程，将密钥配置到你使用的软件中。')
      expect(wrapper.get('[data-tour="key-form-quota"]').isVisible()).toBe(true)
      expect(wrapper.get<HTMLInputElement>('[data-tour="key-form-quota"] input').element.value).toBe('12.5')
      updateKey.mockResolvedValue(key)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledWith(key.id, expect.objectContaining({
        quota: 12.5, ip_whitelist: ['192.0.2.1'], rate_limit_1d: 5, expires_at: key.expires_at,
      }))
      expect(keysAPI.create).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('shows a Gemini card with the Gemini group', async () => {
      const wrapper = await openCreate()
      expect(wrapper.find('input[name="key-provider"][value="gemini"]').exists()).toBe(true)
      await chooseProvider(wrapper, 'gemini')
      expect(optionIds(wrapper)).toEqual([7])
      wrapper.unmount()
    })

    it('classifies all configured platforms and retains the complete table filter', async () => {
      const wrapper = await openCreate()
      expect(wrapper.findAll('input[name="key-provider"]').map(input => input.attributes('value'))).toEqual(['openai', 'anthropic', 'gemini', 'domestic', 'other'])
      expect(wrapper.get('[data-tour="key-form-provider"] .grid').classes()).toContain('sm:grid-cols-5')
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.checked).toBe(true)
      expect(optionIds(wrapper, ['OpenAI'])).toEqual([2])
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await chooseProvider(wrapper, 'anthropic')
      expect(optionIds(wrapper, ['Anthropic'])).toEqual([1])
      await chooseProvider(wrapper, 'domestic')
      expect(optionIds(wrapper, ['国产模型'])).toEqual([3, 4, 5, 6])
      await chooseProvider(wrapper, 'other')
      expect(optionIds(wrapper, ['Grok'])).toEqual([9, 8, 11, 12, 10])
      const tableOptions = wrapper.findAllComponents({ name: 'Select' })[0].props('options') as { kind?: string; disabled?: boolean; label: string }[]
      expect(tableOptions.filter(o => o.kind !== 'group')).toHaveLength(14)
      expect(tableOptions.filter(o => o.kind === 'group').map(o => o.label)).toEqual(['OpenAI', 'Anthropic', 'Gemini', '国产模型', 'Grok'])
      expect(tableOptions.filter(o => o.kind === 'group').every(o => o.disabled)).toBe(true)
    })

    it.each([
      { display: 'gemini', category: 'Gemini', color: 'text-sky-700', unrelated: 'openai' },
      { display: '', category: 'OpenAI', color: 'text-green-700', unrelated: 'gemini' },
    ])('uses the display overrides or the default in each key group control ($category)', async ({ display, category, color, unrelated }) => {
      const group = { ...availableGroups[1], display_icon: display, display_category: display } as Group
      const key = { ...createApiKey(), group_id: group.id, group }
      getAvailableGroups.mockResolvedValue([group])
      listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
      const wrapper = await mountView(false, true)
      const platform = display || 'openai'
      const badge = wrapper.get('[data-tour="key-group"]').getComponent({ name: 'GroupBadge' })
      expect(badge.props('platform')).toBe(platform)
      expect(badge.classes()).toContain(color)

      const options = [
        { kind: 'group', disabled: true, label: category },
        { value: group.id, label: group.name },
      ]
      const filter = wrapper.findAllComponents({ name: 'Select' })[0].props('options')
      expect(filter).toEqual([
        { value: '', label: 'All Groups' },
        { value: 0, label: 'keys.noGroup' },
        { ...options[0], value: `__group_category:${platform}` },
        options[1],
      ])

      await wrapper.get('[data-tour="key-group"]').trigger('click')
      const picker = groupPicker(wrapper)
      expect(groupPickerTabs(wrapper).map(tab => tab.text())).toEqual([`${category} 1`, '画图 0'])
      expect(picker.getComponent({ name: 'GroupOptionItem' }).props('platform')).toBe(platform)
      await picker.get('[data-tour="key-group-option"]').trigger('click')

      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(wrapper.get<HTMLInputElement>(`input[value="${platform}"]`).element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>(`input[value="${unrelated}"]`).element.disabled).toBe(true)
      expect(wrapper.findAll('input[name="key-provider"]')).toHaveLength(4)
      expect(wrapper.get('[data-tour="key-form-provider"] .grid').classes()).toContain('sm:grid-cols-4')
      expect(optionIds(wrapper, [category])).toEqual([group.id])
      expect(groupSelect(wrapper).props('options')[1].platform).toBe(platform)
      await wrapper.get('[data-test="close-dialog"]').trigger('click')

      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(optionIds(wrapper, [category])).toEqual([group.id])
      expect(groupSelect(wrapper).props('options')[1].platform).toBe(platform)
      expect(updateKey).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('removes the Grok label across controls after its last fallback group is reclassified on reload', async () => {
      const grok = availableGroups[7]
      const image = { ...availableGroups[1], allow_image_generation: true }
      const checkHeadings = async (expected: string[]) => {
        const wrapper = await openCreate()
        expect(wrapper.find('input[value="other"]').exists()).toBe(expected.includes('Grok'))
        await chooseProvider(wrapper, 'openai')
        expect(optionIds(wrapper, ['OpenAI'])).toEqual([2])
        await wrapper.get('[data-test="close-dialog"]').trigger('click')
        await wrapper.get('[data-tour="key-group"]').trigger('click')
        expect(groupPickerTabs(wrapper).map(tab => tab.text())).toEqual([...expected.filter(category => category !== 'Grok').map(category => `${category} 1`), '画图 0', ...expected.filter(category => category === 'Grok').map(category => `${category} 1`)])
        const filter = wrapper.findAllComponents({ name: 'Select' })[0].props('options') as { kind?: string; label: string }[]
        expect(filter.filter(option => option.kind === 'group').map(option => option.label)).toEqual(expected)
        await getButtonByText(wrapper, 'common.edit').trigger('click')
        expect(optionIds(wrapper, expected)).toEqual([image.id, grok.id])
        wrapper.unmount()
      }
      getAvailableGroups.mockResolvedValue([image, grok])
      await checkHeadings(['OpenAI', 'Grok'])
      getAvailableGroups.mockResolvedValue([image, { ...grok, display_category: 'domestic' }])
      await checkHeadings(['OpenAI', '国产模型'])
    })

    it('puts an OpenAI image group in Domestic when manually classified there', async () => {
      const image = { ...availableGroups[1], allow_image_generation: true, display_category: 'domestic' } as Group
      const key = { ...createApiKey(), group_id: image.id, group: image }
      listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
      getAvailableGroups.mockResolvedValue([image])
      const wrapper = await openCreate()
      expect(wrapper.get<HTMLInputElement>('input[value="domestic"]').element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.disabled).toBe(true)
      expect(optionIds(wrapper, ['国产模型'])).toEqual([image.id])
      expect(groupSelect(wrapper).props('options')[1].platform).toBe('openai')
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      const groupButton = wrapper.get('[data-tour="key-group"]')
      expect(groupButton.getComponent({ name: 'GroupBadge' }).props('platform')).toBe('openai')
      await groupButton.trigger('click')
      expect(groupPickerTabs(wrapper).map(tab => tab.text())).toEqual(['国产模型 1', '画图 0'])
      expect(groupPicker(wrapper).getComponent({ name: 'GroupOptionItem' }).props('platform')).toBe('openai')
      const filter = wrapper.findAllComponents({ name: 'Select' })[0].props('options') as { kind?: string; label: string }[]
      expect(filter.filter(option => option.kind === 'group').map(option => option.label)).toEqual(['国产模型'])
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(optionIds(wrapper, ['国产模型'])).toEqual([image.id])
      expect(groupSelect(wrapper).props('options')[1].platform).toBe('openai')
      wrapper.unmount()
    })

    it('shows a manually assigned image group in the new provider and the existing group controls', async () => {
      const image = { ...availableGroups[1], id: 100, name: 'Picture group', display_category: 'image' } as Group
      getAvailableGroups.mockResolvedValue([...availableGroups, image])
      listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), group_id: image.id, group: image }], total: 1, page: 1, page_size: 20, pages: 1 })
      const wrapper = await openCreate()
      expect(wrapper.findAll('input[name="key-provider"]').map(input => input.attributes('value')))
        .toEqual(['openai', 'anthropic', 'gemini', 'domestic', 'image', 'other'])
      expect(wrapper.get('[data-tour="key-form-provider"] .grid').classes()).toContain('grid-cols-3')
      expect(optionIds(wrapper, ['OpenAI'])).toEqual([2])
      await chooseProvider(wrapper, 'image')
      expect(optionIds(wrapper, ['画图'])).toEqual([100])
      expect(groupSelect(wrapper).props('options')[1].platform).toBe('openai')
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await wrapper.get('[data-tour="key-group"]').trigger('click')
      expect(pickerTab(wrapper, '画图').attributes('aria-pressed')).toBe('true')
      await pickerTab(wrapper, 'OpenAI').trigger('click')
      await pickerTab(wrapper, '画图').trigger('click')
      expect(groupPicker(wrapper).findAllComponents({ name: 'GroupOptionItem' }).map(item => item.props('name'))).toEqual(['Picture group'])
      await groupPicker(wrapper).get('input').setValue('Picture')
      expect(groupPicker(wrapper).findAllComponents({ name: 'GroupOptionItem' })).toHaveLength(1)
      await groupPicker(wrapper).get('input').setValue('')
      expect(updateKey).not.toHaveBeenCalled()
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      const categories = ['OpenAI', 'Anthropic', 'Gemini', '国产模型', '画图', 'Grok']
      expect(optionIds(wrapper, categories).filter(id => id === 100)).toEqual([100])
      const editOptions = groupSelect(wrapper).props('options')
      expect(editOptions[editOptions.findIndex((option: { label: string }) => option.label === '画图') + 1].value).toBe(100)
      const filter = wrapper.findAllComponents({ name: 'Select' })[0].props('options') as { kind?: string; label: string }[]
      expect(filter.filter(option => option.kind === 'group').map(option => option.label)).toEqual(categories)
      wrapper.unmount()
    })

    it('clears the previous group on provider change and submits only the newly selected group', async () => {
      const wrapper = await openCreate()
      await wrapper.get('[data-tour="key-form-name"]').setValue('My key')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      await chooseProvider(wrapper, 'domestic')
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('#key-form').trigger('submit')
      expect(keysAPI.create).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('keys.groupRequired')

      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), group_id: 5 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(keysAPI.create).toHaveBeenCalledOnce()
      expect(vi.mocked(keysAPI.create).mock.calls[0].slice(0, 2)).toEqual(['My key', 5])
    })

    it('copies the new key right after creating it and tells the user to connect with the helper', async () => {
      const wrapper = await openCreate()
      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), key: 'sk-new-key', group_id: 2 })
      copyToClipboard.mockResolvedValue(true)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(copyToClipboard).toHaveBeenCalledOnce()
      expect(copyToClipboard).toHaveBeenCalledWith('sk-new-key', 'keys.keyCreatedCopied')
      // 复制成功时只提示「已复制，请通过助手接入」，不再重复弹「创建成功」
      expect(showSuccess).not.toHaveBeenCalledWith('keys.keyCreatedSuccess')
    })

    it('still says the key was created when the browser refuses to copy', async () => {
      const wrapper = await openCreate()
      await groupSelect(wrapper).vm.$emit('update:modelValue', 2)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), key: 'sk-new-key', group_id: 2 })
      copyToClipboard.mockResolvedValue(false)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(copyToClipboard).toHaveBeenCalledWith('sk-new-key', 'keys.keyCreatedCopied')
      expect(showSuccess).toHaveBeenCalledWith('keys.keyCreatedSuccess')
    })

    it('does not copy anything when only editing a key', async () => {
      const key = { ...createApiKey(), group_id: 2 }
      listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
      updateKey.mockResolvedValue(key)
      const wrapper = await mountView()
      ;(wrapper.vm as any).editKey(key)
      await flushPromises()
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(updateKey).toHaveBeenCalled()
      expect(copyToClipboard).not.toHaveBeenCalled()
    })

    it('uses a row of colored category buttons and keeps guide markers on group options', async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="key-group"]').trigger('click')
      const popup = groupPicker(wrapper)
      const tabs = groupPickerTabs(wrapper)
      expect(tabs.map(tab => tab.text())).toEqual(['OpenAI 1', 'Anthropic 1', 'Gemini 1', '国产模型 4', '画图 0', 'Grok 5'])
      expect(tabs.every(tab => !tab.attributes('data-tour'))).toBe(true)
      expect(tabs.map(tab => tab.findAllComponents({ name: 'PlatformIcon' }).map(icon => icon.props('platform')))).toEqual([
        ['openai'], ['anthropic'], ['gemini'], ['deepseek', 'kimi'], [], ['grok'],
      ])
      expect(tabs[0].classes()).toContain('text-green-600')
      expect(tabs[1].classes()).toContain('text-orange-600')
      expect(tabs[2].classes()).toContain('text-blue-600')
      expect(tabs[4].classes()).toContain('text-purple-600')
      expect(tabs[4].get('[data-icon="sparkles"]').attributes('aria-hidden')).toBe('true')

      const first = popup.get('[data-tour="key-group-option"]')
      expect(first.element.tagName).toBe('BUTTON')
      expect(first.findComponent({ name: 'GroupOptionItem' }).props('name')).toBe('Shared group 2')
      await pickerTab(wrapper, 'Gemini').trigger('click')
      expect(pickerTab(wrapper, 'Gemini').attributes('aria-pressed')).toBe('true')
      expect(updateKey).not.toHaveBeenCalled()
      await groupPicker(wrapper).get('input').setValue('Anthropic')
      expect(groupPickerTabs(wrapper).every(tab => tab.element.hasAttribute('disabled'))).toBe(true)
      expect(groupPicker(wrapper).findAll('[data-tour="key-group-option"]')).toHaveLength(0)
      expect(groupPicker(wrapper).text()).toContain('keys.noGroupFound')
      wrapper.unmount()
    })

    it('keeps the current Anthropic binding until an OpenAI group is explicitly clicked', async () => {
      const key = { ...createApiKey(), group_id: 1 }
      listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
      updateKey.mockResolvedValue({ ...key, group_id: 2 })
      const wrapper = await mountView()
      await wrapper.get('[data-tour="key-group"]').trigger('click')
      const popup = () => wrapper.get('[data-tour="key-group-dropdown"]')
      const options = () => popup().findAll('[data-tour="key-group-option"]')
      expect(pickerTab(wrapper, 'Anthropic').attributes('aria-pressed')).toBe('true')
      expect(options()[0].findComponent({ name: 'GroupOptionItem' }).props('selected')).toBe(true)
      await pickerTab(wrapper, 'OpenAI').trigger('click')
      expect(updateKey).not.toHaveBeenCalled()
      await pickerTab(wrapper, 'Anthropic').trigger('click')
      await options()[0].trigger('click')
      expect(wrapper.find('[data-tour="key-group-dropdown"]').exists()).toBe(false)
      expect(updateKey).not.toHaveBeenCalled()
      await wrapper.get('[data-tour="key-group"]').trigger('click')
      await pickerTab(wrapper, 'OpenAI').trigger('click')
      await options()[0].trigger('click')
      await flushPromises()
      expect(updateKey).toHaveBeenCalledTimes(1)
      expect(updateKey).toHaveBeenCalledWith(key.id, { group_id: 2 })
      expect(key.group_id).toBe(1)
      wrapper.unmount()
    })

    it('defaults to a provider with available groups and disables empty categories', async () => {
      getAvailableGroups.mockResolvedValue([availableGroups[5]])
      const wrapper = await openCreate()
      expect(wrapper.get<HTMLInputElement>('input[value="domestic"]').element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[value="anthropic"]').element.disabled).toBe(true)
      expect(optionIds(wrapper)).toEqual([6])
    })

    it('falls back to Anthropic when no OpenAI groups are available without selecting a group', async () => {
      getAvailableGroups.mockResolvedValue([availableGroups[0]])
      const wrapper = await openCreate()
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.disabled).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[value="anthropic"]').element.checked).toBe(true)
      expect(optionIds(wrapper)).toEqual([1])
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      expect(keysAPI.create).not.toHaveBeenCalled()
      expect(updateKey).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('shows the empty state when no groups are available', async () => {
      getAvailableGroups.mockResolvedValue([])
      const wrapper = await openCreate()
      expect(wrapper.get('[data-tour="key-form-provider"]').text()).toContain('common.noGroupsAvailable')
      expect(optionIds(wrapper)).toEqual([])
      expect(wrapper.findAll<HTMLInputElement>('input[name="key-provider"]').every((input) => input.element.disabled)).toBe(true)
    })

    it('selects an available provider when groups arrive after opening', async () => {
      let resolveGroups!: (value: typeof availableGroups) => void
      getAvailableGroups.mockReturnValue(new Promise((resolve) => { resolveGroups = resolve }))
      const wrapper = await openCreate()
      resolveGroups([availableGroups[1]])
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.checked).toBe(true)
      expect(optionIds(wrapper)).toEqual([2])
    })

    it('resets provider and group when reopening create, and preserves edit options', async () => {
      const wrapper = await openCreate()
      await chooseProvider(wrapper, 'domestic')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(optionIds(wrapper)).toEqual([2])
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(optionIds(wrapper)).toHaveLength(12)
    })
  })
  describe('single-column group picker', () => {
    const pickerGroups = [
      { id: 1, platform: 'openai', name: 'GPT Reference', description: null, rate_multiplier: 1, subscription_type: 'standard' },
      { id: 2, platform: 'anthropic', name: 'Claude Group', description: 'Reference access', rate_multiplier: 1, subscription_type: 'standard' },
      { id: 3, platform: 'grok', name: 'Grok Group', description: null, rate_multiplier: 1, subscription_type: 'standard' },
    ]
    let wrapper: Awaited<ReturnType<typeof mountView>> | undefined

    const openPicker = async () => {
      wrapper = await mountView()
      await wrapper.get('[data-tour="key-group"]').trigger('click')
      return wrapper
    }
    const items = (view: VueWrapper) => groupPicker(view).findAllComponents({ name: 'GroupOptionItem' })
    const names = (view: VueWrapper) => items(view).map(item => item.props('name'))
    const tabs = (view: VueWrapper) => groupPickerTabs(view).map(tab => tab.text())

    afterEach(() => {
      wrapper?.unmount()
      wrapper = undefined
      vi.useRealTimers()
    })

    it('searches across categories, disables the tabs, and restores the active category on clear', async () => {
      getAvailableGroups.mockResolvedValue(pickerGroups)
      const view = await openPicker()
      expect(tabs(view)).toEqual(['OpenAI 1', 'Anthropic 1', '画图 0', 'Grok 1'])
      await pickerTab(view, 'Grok').trigger('click')
      await groupPicker(view).get('input').setValue('  rEfErEnCe ')
      expect(names(view)).toEqual(['GPT Reference', 'Claude Group'])
      expect(groupPickerTabs(view).every(tab => tab.element.hasAttribute('disabled'))).toBe(true)
      await pickerTab(view, 'OpenAI').trigger('click')
      await groupPicker(view).get('input').setValue(' ')
      expect(pickerTab(view, 'Grok').attributes('aria-pressed')).toBe('true')
      expect(names(view)).toEqual(['Grok Group'])
      expect(updateKey).not.toHaveBeenCalled()
    })

    it('keeps the empty image tab blank and never assigns a group based on the import setting', async () => {
      getPublicSettings.mockResolvedValue({ quick_import_image_group_id: 1 })
      getAvailableGroups.mockResolvedValue(pickerGroups)
      const view = await openPicker()
      expect(tabs(view)).toEqual(['OpenAI 1', 'Anthropic 1', '画图 0', 'Grok 1'])
      expect(names(view)).toEqual(['GPT Reference'])
      vi.useFakeTimers()
      await pickerTab(view, '画图').trigger('pointerenter')
      await vi.advanceTimersByTimeAsync(150)
      expect(pickerTab(view, '画图').attributes('aria-pressed')).toBe('true')
      expect(names(view)).toEqual([])
      expect(groupPicker(view).text()).not.toContain('keys.noGroupFound')
      await groupPicker(view).get('input').setValue('Reference')
      expect(names(view)).toEqual(['GPT Reference', 'Claude Group'])
      await groupPicker(view).get('input').setValue('')
      expect(names(view)).toEqual([])
      await pickerTab(view, 'OpenAI').trigger('click')
      expect(names(view)).toEqual(['GPT Reference'])
      expect(updateKey).not.toHaveBeenCalled()
    })

    it('defaults to Grok instead of the empty image tab when only Grok groups arrive after opening', async () => {
      let release!: (value: typeof pickerGroups) => void
      getAvailableGroups.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
      const view = await openPicker()
      expect(tabs(view)).toEqual([])
      expect(names(view)).toEqual([])
      release([pickerGroups[2]])
      await flushPromises()
      expect(tabs(view)).toEqual(['画图 0', 'Grok 1'])
      expect(names(view)).toEqual(['Grok Group'])
    })

    it('keeps the original order, wraps long names, and lets display icons change independently', async () => {
      const groups = [
        { ...pickerGroups[1], sort_order: 2, display_icon: 'openai', display_category: 'gemini' },
        { ...pickerGroups[0], sort_order: 3, display_icon: 'anthropic', display_category: 'gemini' },
        { ...pickerGroups[1], id: 4, name: 'Claude without sort order or a short name', display_icon: 'openai', display_category: 'gemini' },
        { ...pickerGroups[0], id: 5, name: 'First GPT', sort_order: 1, display_icon: 'anthropic', display_category: 'gemini' },
      ]
      getAvailableGroups.mockResolvedValueOnce(groups)
      const view = await openPicker()
      const expected = sortGroupsForDisplay(groups)
      expect(tabs(view)).toEqual(['Gemini 4', '画图 0'])
      expect(names(view)).toEqual(expected.map(group => group.name))
      expect(items(view).map(item => item.props('platform'))).toEqual(expected.map(group => group.display_icon))
      expect(items(view).every(item => item.props('wrapName') === true)).toBe(true)

      getAvailableGroups.mockResolvedValueOnce(groups.map(group => ({ ...group, display_icon: 'gemini' })))
      await (view.vm as unknown as { loadGroups: () => Promise<void> }).loadGroups()
      expect(names(view)).toEqual(expected.map(group => group.name))
      expect(items(view).every(item => item.props('platform') === 'gemini')).toBe(true)
    })

    it('hovers for 150 ms, cancels on exit or search, and clicks immediately', async () => {
      getAvailableGroups.mockResolvedValue(pickerGroups)
      const view = await openPicker()
      vi.useFakeTimers()
      const anthropic = pickerTab(view, 'Anthropic')
      await anthropic.trigger('pointerenter')
      await vi.advanceTimersByTimeAsync(149)
      expect(names(view)).toEqual(['GPT Reference'])
      await anthropic.trigger('pointerleave')
      await vi.advanceTimersByTimeAsync(1)
      expect(names(view)).toEqual(['GPT Reference'])
      await groupPicker(view).get('input').setValue(' ')
      await anthropic.trigger('pointerenter')
      await vi.advanceTimersByTimeAsync(150)
      expect(names(view)).toEqual(['Claude Group'])
      await pickerTab(view, 'Grok').trigger('pointerenter')
      await groupPicker(view).get('input').setValue('Reference')
      await vi.advanceTimersByTimeAsync(150)
      await groupPicker(view).get('input').setValue('')
      expect(names(view)).toEqual(['Claude Group'])
      await pickerTab(view, 'OpenAI').trigger('pointerenter')
      await pickerTab(view, 'Grok').trigger('click')
      expect(names(view)).toEqual(['Grok Group'])
      await vi.advanceTimersByTimeAsync(150)
      expect(names(view)).toEqual(['Grok Group'])
      expect(updateKey).not.toHaveBeenCalled()
    })

    it('cancels hover and restores a valid category after a list change or closing', async () => {
      const setGroups = async (groups: typeof pickerGroups) => {
        getAvailableGroups.mockResolvedValueOnce(groups)
        await (view.vm as unknown as { loadGroups: () => Promise<void> }).loadGroups()
      }
      getAvailableGroups.mockResolvedValue(pickerGroups)
      const view = await openPicker()
      vi.useFakeTimers()
      await pickerTab(view, 'Grok').trigger('pointerenter')
      await setGroups(pickerGroups.slice(0, 2))
      await vi.advanceTimersByTimeAsync(150)
      expect(names(view)).toEqual(['GPT Reference'])
      await setGroups(pickerGroups)
      await pickerTab(view, 'Grok').trigger('click')
      await groupPicker(view).get('input').setValue('Reference')
      await setGroups(pickerGroups.slice(0, 2))
      expect(names(view)).toEqual(['GPT Reference', 'Claude Group'])
      await groupPicker(view).get('input').setValue('')
      expect(names(view)).toEqual(['GPT Reference'])
      await pickerTab(view, 'Anthropic').trigger('pointerenter')
      await view.get('[data-tour="key-group"]').trigger('click')
      await view.get('[data-tour="key-group"]').trigger('click')
      await vi.advanceTimersByTimeAsync(150)
      expect(names(view)).toEqual(['GPT Reference'])
    })
  })

  describe('guide success callbacks', () => {
    let wrapper: VueWrapper | undefined
    let release: (() => void) | undefined
    const complete = vi.fn()
    beforeEach(() => {
      complete.mockReset()
      getAvailableGroups.mockResolvedValue([{ id: 1, name: 'OpenAI', platform: 'openai', rate_multiplier: 1, subscription_type: 'standard' }])
      release = bindGuideBusinessSession({ sessionId: {}, userId: 7, accepts: () => true, complete })
    })
    afterEach(() => { release?.(); wrapper?.unmount(); wrapper = undefined })
    const prepare = async () => {
      wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await wrapper.get('[data-tour="key-form-name"]').setValue('Guide key')
      return wrapper
    }
    it('reports a successful creation ID before closing and preserves the administrator nextStep callback', async () => {
      const view = await prepare()
      view.findComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 1)
      isCurrentStep.mockReturnValue(true)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), id: 88, group_id: 1 })
      complete.mockImplementation(() => { expect(view.find('#key-form').exists()).toBe(true) })
      await view.get('#key-form').trigger('submit'); await flushPromises()
      expect(complete).toHaveBeenCalledWith('key-create', 88)
      expect(nextStep).toHaveBeenCalledWith(500)
      expect(view.find('#key-form').exists()).toBe(false)
    })
    it('does not report validation failures, rejected creations or updates as a successful new key', async () => {
      const view = await prepare()
      await view.get('#key-form').trigger('submit'); await flushPromises()
      expect(keysAPI.create).not.toHaveBeenCalled(); expect(complete).not.toHaveBeenCalled()
      view.findComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 1)
      vi.mocked(keysAPI.create).mockRejectedValue(new Error('creation failed'))
      await view.get('#key-form').trigger('submit'); await flushPromises()
      expect(complete).not.toHaveBeenCalled(); expect(nextStep).not.toHaveBeenCalled()
      expect(view.find('#key-form').exists()).toBe(true)
      await view.get('[data-test="close-dialog"]').trigger('click')
      await view.get('[data-tour="key-edit"]').trigger('click')
      view.findComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 1)
      updateKey.mockResolvedValue(createApiKey())
      await view.get('#key-form').trigger('submit'); await flushPromises()
      expect(complete).not.toHaveBeenCalled()
    })
    it('reports only successful native copies and ignores a copy completed after its session exits', async () => {
      wrapper = await mountView()
      copyToClipboard.mockResolvedValue(false)
      await wrapper.get('[data-tour="key-copy"]').trigger('click'); await flushPromises()
      expect(complete).not.toHaveBeenCalled()
      copyToClipboard.mockResolvedValue(true)
      await wrapper.get('[data-tour="key-copy"]').trigger('click'); await flushPromises()
      expect(complete).toHaveBeenCalledWith('key-copy', 1)
      complete.mockClear()
      let done!: (value: boolean) => void
      copyToClipboard.mockReturnValueOnce(new Promise(resolve => { done = resolve }))
      await wrapper.get('[data-tour="key-copy"]').trigger('click')
      release!()
      release = bindGuideBusinessSession({ sessionId: {}, userId: 7, accepts: () => true, complete })
      done(true); await flushPromises()
      expect(complete).not.toHaveBeenCalled()
    })
  })


  describe('quick create from the dashboard', () => {
    const group = (id: number, platform: string, sort_order: number) => ({ id, name: `g${id}`, platform, sort_order, rate_multiplier: 1, status: 'active', subscription_type: 'standard', is_exclusive: false })

    it('opens the create dialog with the first GPT group, hints at the create button and clears the query', async () => {
      routeState.query = { create: 'quick', tab: 'x' }
      getAvailableGroups.mockResolvedValue([group(1, 'anthropic', 1), group(8, 'openai', 9), group(4, 'openai', 2)])
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), id: 2 })
      const wrapper = await mountView()

      expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
      expect(routerReplace).toHaveBeenCalledWith({ query: { tab: 'x' } })
      expect(wrapper.get('[data-testid="guide-bubble"]').text()).toBe('直接点创建')
      expect(wrapper.get('[data-tour="key-form-name"]').attributes('placeholder')).toBe('测试')
      expect(wrapper.get('[data-tour="key-form-name"]').attributes('required')).toBeUndefined()

      // 名称留空直接创建：用「测试」，分组是 GPT 第一个
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0].slice(0, 2)).toEqual(['测试', 4])
      expect(wrapper.find('[data-testid="guide-bubble"]').exists()).toBe(false)
    })

    it('does not replace a manually selected group when settings arrive late', async () => {
      routeState.query = { create: 'quick' }
      getAvailableGroups.mockResolvedValue([group(4, 'openai', 1), group(8, 'openai', 2)])
      let finishSettings!: (settings: { quick_import_image_group_id: number }) => void
      getPublicSettings.mockReturnValueOnce(new Promise(resolve => { finishSettings = resolve }))
      const wrapper = await mountView()
      const state = wrapper.vm as unknown as { formData: { group_id: number | null } }
      expect(state.formData.group_id).toBeNull()
      state.formData.group_id = 8
      finishSettings({ quick_import_image_group_id: -1 })
      await flushPromises()
      expect(state.formData.group_id).toBe(8)
    })

    it('preselects the Gemini card for the first real GPT group and submits its ID', async () => {
      routeState.query = { create: 'quick' }
      getAvailableGroups.mockResolvedValue([
        group(1, 'gemini', 0),
        { ...group(4, 'openai', 1), display_icon: 'gemini', display_category: 'gemini' },
        group(8, 'openai', 2),
      ])
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), id: 2 })
      const wrapper = await mountView()
      expect(wrapper.get<HTMLInputElement>('input[value="gemini"]').element.checked).toBe(true)
      expect(wrapper.getComponent('[data-tour="key-form-group"]').props('modelValue')).toBe(4)
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0].slice(0, 2)).toEqual(['测试', 4])
      wrapper.unmount()
    })

    it('numbers the default name when test keys already exist, and stays normal without the query', async () => {
      listKeys.mockResolvedValue({ items: [{ ...createApiKey(), name: '测试' }, { ...createApiKey(), id: 2, name: '测试1' }], total: 2, page: 1, page_size: 20, pages: 1 })
      getAvailableGroups.mockResolvedValue([group(4, 'openai', 2)])
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), id: 3 })
      const wrapper = await mountView()
      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      expect(routerReplace).not.toHaveBeenCalled()

      await getButtonByText(wrapper, 'Create API Key').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="guide-bubble"]').exists()).toBe(false)
      ;(wrapper.vm as unknown as { formData: { group_id: number } }).formData.group_id = 4
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(vi.mocked(keysAPI.create).mock.calls[0][0]).toBe('测试2')
    })
  })


  describe('usage stats loading', () => {
    it('renders the key list without waiting for slow usage stats', async () => {
      let resolveUsage!: (value: { stats: Record<string, unknown> }) => void
      getDashboardApiKeysUsage.mockReturnValue(new Promise((resolve) => { resolveUsage = resolve }))
      const wrapper = await mountView()

      const table = wrapper.findComponent({ name: 'DataTable' })
      expect(table.props('loading')).toBe(false)
      expect(table.props('data')).toHaveLength(1)
      expect(wrapper.find('[data-test="usage-pending"]').exists()).toBe(true)

      resolveUsage({
        stats: { 1: { api_key_id: 1, today_actual_cost: 0.5, total_actual_cost: 12.25 } },
      })
      await flushPromises()
      expect(wrapper.find('[data-test="usage-pending"]').exists()).toBe(false)
      expect(wrapper.get('[data-test="usage"]').text()).toContain('$12.2500')
    })

    it('stops showing usage placeholders when the usage request fails', async () => {
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
      getDashboardApiKeysUsage.mockRejectedValue(new Error('timeout'))
      const wrapper = await mountView()

      expect(wrapper.find('[data-test="usage-pending"]').exists()).toBe(false)
      expect(wrapper.get('[data-test="usage"]').text()).toContain('$0.0000')
      consoleError.mockRestore()
    })
  })
})
