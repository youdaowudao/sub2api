<template>
  <div :class="flat ? 'p-4 sm:p-6' : 'card p-6'">
    <!-- Toolbar: left filters (multi-line) + right actions -->
    <div class="flex flex-wrap items-end justify-between gap-4">
      <!-- Left: filters (allowed to wrap to multiple rows) -->
      <div class="flex flex-1 flex-wrap items-end gap-4">
        <!-- User Search -->
        <div ref="userSearchRef" @focusout="onChoiceFocusOut($event, userSearchRef, closeUserChoices)" class="usage-filter-dropdown relative w-full sm:w-auto sm:min-w-[240px]">
          <label class="input-label">{{ t('admin.usage.userFilter') }}</label>
          <input
            v-model="userKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchUserPlaceholder')"
            @input="debounceUserSearch"
            @focus="onUserFocus" @keydown="userNavigation.keydown"
          />
          <button
            v-if="filters.user_id"
            type="button"
            @click="clearUser"
            class="absolute right-2 top-9 text-gray-400"
            aria-label="Clear user filter"
          >
            ✕
          </button>
          <div
            v-if="showUserDropdown && (userResults.length > 0 || userKeyword || showHistory)"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:bg-dark-800"
          >
            <AdminUserHistoryList v-if="showHistory && !userKeyword.trim()" @select="item => selectUser({ id: item.id, email: item.email, deleted: false })" />
            <button
              v-for="u in userResults" data-filter-choice
              :key="u.id"
              type="button"
              @click="selectUser(u)"
              class="w-full px-4 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
            >
              <span>{{ u.email }}<span v-if="u.deleted" class="ml-1 text-xs text-gray-400">（{{ t('admin.usage.userDeletedBadge') }}）</span></span>
              <span class="ml-2 text-xs text-gray-400">#{{ u.id }}</span>
            </button>
          </div>
        </div>

        <!-- API Key Search -->
        <div ref="apiKeySearchRef" @focusout="onChoiceFocusOut($event, apiKeySearchRef, closeKeyChoices)" class="usage-filter-dropdown relative w-full sm:w-auto sm:min-w-[240px]">
          <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
          <input
            v-model="apiKeyKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchApiKeyPlaceholder')"
            @input="debounceApiKeySearch"
            @focus="onApiKeyFocus" @keydown="keyNavigation.keydown"
          />
          <button
            v-if="filters.api_key_id"
            type="button"
            @click="onClearApiKey"
            class="absolute right-2 top-9 text-gray-400"
            aria-label="Clear API key filter"
          >
            ✕
          </button>
          <div
            v-if="showApiKeyDropdown && apiKeyResults.length > 0"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:bg-dark-800"
          >
            <button
              v-for="k in apiKeyResults" data-filter-choice
              :key="k.id"
              type="button"
              @click="selectApiKey(k)"
              class="w-full px-4 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
            >
              <span class="truncate">{{ k.name || `#${k.id}` }}</span>
              <span class="ml-2 text-xs text-gray-400">#{{ k.id }}</span>
            </button>
          </div>
        </div>

        <!-- Model Filter -->
        <div class="w-full sm:w-auto sm:min-w-[220px]">
          <!-- 排序口径提示放在标签旁的提示图标里，不占高度，避免与同行/下一行字段错位 -->
          <label class="input-label flex items-center gap-1">
            {{ t('usage.model') }}
            <HelpTooltip v-if="showActions && modelRankingAvailable !== undefined" :content="t(modelRankingAvailable ? 'admin.usage.modelRankingBasis' : 'admin.usage.noModelRanking')" />
          </label>
          <Select v-model="filters.model" :options="modelOptions" searchable :creatable="showActions" @change="emitChange" />
        </div>

        <!-- Account Filter -->
        <div ref="accountSearchRef" @focusout="onChoiceFocusOut($event, accountSearchRef, closeAccountChoices)" class="usage-filter-dropdown relative w-full sm:w-auto sm:min-w-[220px]">
          <label class="input-label">{{ t('admin.usage.account') }}</label>
          <input
            v-model="accountKeyword"
            type="text"
            class="input pr-8"
            :placeholder="t('admin.usage.searchAccountPlaceholder')"
            @input="debounceAccountSearch"
            @focus="onAccountFocus" @keydown="accountNavigation.keydown"
          />
          <button
            v-if="filters.account_id"
            type="button"
            @click="clearAccount"
            class="absolute right-2 top-9 text-gray-400"
            aria-label="Clear account filter"
          >
            ✕
          </button>
          <div
            v-if="showAccountDropdown && (showActions || visibleAccountChoices.length > 0)"
            class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:bg-dark-800"
          >
            <p v-if="showActions && !accountSearching" class="px-3 py-2 text-xs text-gray-500">{{ visibleAccountChoices.length ? t(candidatePrevious ? 'admin.usage.previousAccountRanking' : 'admin.usage.accountRanking', { range: candidateWindow || '' }) : t('admin.usage.noAccountRanking') }}</p>
            <button
              v-for="a in visibleAccountChoices" data-filter-choice
              :key="a.id"
              type="button"
              @click="selectAccount(a)"
              class="w-full px-4 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
            >
              <span class="truncate">{{ a.name }}</span>
              <span class="ml-2 text-xs text-gray-400">#{{ a.id }}</span>
              <span v-if="a.deleted" class="ml-1 text-xs text-gray-400">{{ t('admin.usage.deletedAccount') }}</span>
              <span v-if="a.requests != null" class="float-right text-xs tabular-nums text-gray-500">{{ a.requests.toLocaleString() }}</span>
            </button>
          </div>
        </div>

        <!-- Request Type Filter (usage only) -->
        <div v-if="mode !== 'errors'" class="w-full sm:w-auto sm:min-w-[180px]">
          <label class="input-label">{{ t('usage.type') }}</label>
          <Select v-model="filters.request_type" :options="requestTypeOptions" @change="emitChange" />
        </div>

        <!-- Native compaction is independent of the transport request type. -->
        <div v-if="mode !== 'errors'" class="w-full sm:w-auto sm:min-w-[180px]">
          <label class="input-label">{{ t('usage.compactionFilter') }}</label>
          <Select v-model="filters.native_compaction_v2" :options="compactionOptions" @change="emitChange" />
        </div>

        <!-- Billing Type Filter (usage only) -->
        <div v-if="mode !== 'errors'" class="w-full sm:w-auto sm:min-w-[200px]">
          <label class="input-label">{{ t('admin.usage.billingType') }}</label>
          <Select v-model="filters.billing_type" :options="billingTypeOptions" @change="emitChange" />
        </div>

        <!-- Billing Mode Filter (usage only；用户排行的 user-breakdown 接口不支持该维度) -->
        <div v-if="mode === 'usage'" class="w-full sm:w-auto sm:min-w-[200px]">
          <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
          <Select v-model="filters.billing_mode" :options="billingModeOptions" @change="emitChange" />
        </div>

        <div v-if="mode === 'usage'" class="w-full sm:w-auto sm:min-w-[220px]">
          <label class="input-label">{{ t('admin.usage.upstreamModelAudit') }}</label>
          <Select v-model="filters.upstream_model_mismatch" :options="upstreamModelMismatchOptions" @change="emitChange" />
        </div>

        <!-- Error Phase Filter (errors only) -->
        <div v-if="mode === 'errors'" class="w-full sm:w-auto sm:min-w-[180px]">
          <label class="input-label">{{ t('admin.ops.errorLog.type') }}</label>
          <Select v-model="filters.error_phase" :options="errorPhaseOptions" @change="emitChange" />
        </div>

        <!-- Error Category Filter (errors only) -->
        <div v-if="mode === 'errors'" class="w-full sm:w-auto sm:min-w-[180px]">
          <label class="input-label">{{ t('usage.errors.category') }}</label>
          <Select v-model="filters.error_category" :options="errorCategoryOptions" @change="emitChange" />
        </div>

        <!-- Status Code Filter (errors only) -->
        <div v-if="mode === 'errors'" class="w-full sm:w-auto sm:min-w-[180px]">
          <label class="input-label">{{ t('admin.ops.errorLog.status') }}</label>
          <Select v-model="filters.status_code" :options="statusCodeOptions" @change="emitChange" />
        </div>

        <!-- Group Filter -->
        <div class="w-full sm:w-auto sm:min-w-[200px]">
          <label class="input-label">{{ t('admin.usage.group') }}</label>
          <AdminGroupPicker :model-value="filters.group_id ?? null" :groups="fetchedGroups" :special-options="[{ value: null, label: t('admin.usage.allGroups') }]" :options="groupOptions" @update:model-value="filters.group_id = $event" @change="emitChange" />
        </div>

      </div>

      <!-- Right: actions -->
      <div v-if="showActions" class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
        <button v-if="flat" type="button" class="btn btn-secondary" @click="$emit('clear-preserve-time')">{{ t('admin.usage.clearKeepTime') }}</button>
        <button type="button" @click="$emit('refresh')" class="btn btn-secondary">
          {{ t('common.refresh') }}
        </button>
        <button type="button" @click="resetFilters" class="btn btn-secondary">
          {{ t('common.reset') }}
        </button>
        <slot name="after-reset" />
        <template v-if="mode === 'usage'">
          <button type="button" @click="$emit('cleanup')" :disabled="disableCleanup" :title="disableCleanup ? t('admin.usage.recentRange.cleanupDateOnly') : undefined" class="btn btn-danger">
            {{ t('admin.usage.cleanup.button') }}
          </button>
          <button type="button" @click="$emit('export')" :disabled="exporting" class="btn btn-primary">
            {{ t('usage.exportExcel') }}
          </button>
        </template>
      </div>
    </div>
    <div v-if="showActions && flat && activeChips.length" class="mt-3 flex flex-wrap gap-1.5 text-xs">
      <span class="py-1 text-gray-500">{{ t('admin.ops.records.activeFilters') }}</span>
      <button v-for="chip in activeChips" :key="chip.key" type="button" class="rounded-full bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300" @click="clearChip(chip.key)">{{ chip.label }} ×</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { buildAdminGroupSelectOptions } from '@/utils/groupDisplay'
import { useFilterChoiceNavigation } from '@/composables/useFilterChoiceNavigation'
import type { AdminGroup } from '@/types'

import { ref, onMounted, onUnmounted, toRef, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import AdminUserHistoryList from './AdminUserHistoryList.vue'
import AdminGroupPicker from '@/components/admin/AdminGroupPicker.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { useAdminUserHistory } from '@/composables/useAdminUserHistory'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'

type ModelValue = Record<string, any>

interface Props {
  modelValue: ModelValue
  exporting: boolean
  startDate: string
  endDate: string
  showActions?: boolean
  disableCleanup?: boolean
  modelOptions?: Array<string | SelectOption>
  accountCandidates?: SimpleAccount[]
  candidateWindow?: string
  candidatePrevious?: boolean
  modelRankingAvailable?: boolean
  /**
   * errors 模式:隐藏用量专属字段/按钮,显示错误类型+状态码(错误请求 tab 用)
   * ranking 模式:同 usage 但隐藏计费模式筛选与清理/导出按钮(用户排行 tab 用)
   */
  mode?: 'usage' | 'errors' | 'ranking'
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showActions: true,
  mode: 'usage',
  flat: false
})
const emit = defineEmits([
  'update:modelValue',
  'change',
  'refresh',
  'reset',
  'clear-preserve-time',
  'export',
  'cleanup'
])

const { t } = useI18n()
const filters = toRef(props, 'modelValue')
const history = useAdminUserHistory()
const showHistory = computed(() => props.showActions && history.enabled.value && (history.entries.value.length > 0 || history.failed.value))

const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const accountSearchRef = ref<HTMLElement | null>(null)

const userKeyword = ref('')
const selectedUserLabel = ref('')
const userResults = ref<SimpleUser[]>([])
const showUserDropdown = ref(false)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0
let userResultsKeyword: string | null = null

const apiKeyKeyword = ref('')
const selectedKeyLabel = ref('')
const apiKeyResults = ref<SimpleApiKey[]>([])
const showApiKeyDropdown = ref(false)
let apiKeySearchTimeout: ReturnType<typeof setTimeout> | null = null
let apiKeySearchSequence = 0
let apiKeyResultsContext: string | null = null
const apiKeyContext = (id: number | undefined | null, keyword: string) => JSON.stringify([id ?? null, keyword])

interface SimpleAccount {
  id: number
  name: string
  requests?: number
  deleted?: boolean
}
const accountKeyword = ref('')
const selectedAccountLabel = ref('')
const selectedGroupLabel = ref('')
const accountResults = ref<SimpleAccount[]>([])
const showAccountDropdown = ref(false)
let accountSearchTimeout: ReturnType<typeof setTimeout> | null = null
let accountSearchSequence = 0
const accountSearching = ref(false)
const visibleAccountChoices = computed(() => accountSearching.value ? accountResults.value : (props.accountCandidates ?? []))
const userNavigation = useFilterChoiceNavigation(userSearchRef, showUserDropdown, () => closeUserChoices())
const keyNavigation = useFilterChoiceNavigation(apiKeySearchRef, showApiKeyDropdown, () => closeKeyChoices())
const accountNavigation = useFilterChoiceNavigation(accountSearchRef, showAccountDropdown, () => closeAccountChoices())


const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...(props.modelOptions ?? []).map((m) => typeof m === 'string' ? { value: m, label: m } : m),
  ...(filters.value.model && !(props.modelOptions ?? []).some(m => (typeof m === 'string' ? m : m.value) === filters.value.model) ? [{ value: filters.value.model, label: filters.value.model }] : []),
])
const fetchedGroups = ref<AdminGroup[]>([])
const groupOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allGroups') },
  ...buildAdminGroupSelectOptions(fetchedGroups.value, g => ({ value: g.id, label: g.name }), t),
  ...(filters.value.group_id && !fetchedGroups.value.some(g => g.id === filters.value.group_id)
    ? [{ value: filters.value.group_id, label: selectedGroupLabel.value || '#' + filters.value.group_id }] : []),
])

const requestTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
  { value: 'cyber', label: t('usage.cyber') }
])

const compactionOptions = ref<SelectOption[]>([
  { value: null, label: t('usage.allCompactionTypes') },
  { value: true, label: t('usage.compactionOnly') }
])

const billingTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') }
])

// 错误类型对应后端 phase 参数(与错误表"类型"徽章同语义)
const errorPhaseOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'upstream', label: t('admin.ops.errorLog.typeUpstream') },
  { value: 'account_auth', label: t('admin.ops.errorLog.typeAccountAuth') },
  { value: 'request', label: t('admin.ops.errorLog.typeRequest') },
  { value: 'auth', label: t('admin.ops.errorLog.typeAuth') },
  { value: 'routing', label: t('admin.ops.errorLog.typeRouting') },
  { value: 'internal', label: t('admin.ops.errorLog.typeInternal') },
])

// 分类码同用户端 /usage 错误筛选;"other" 无法反查为过滤条件,刻意不列
const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

const statusCodeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const billingModeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') }
])

const upstreamModelMismatchOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allUpstreamModelAudit') },
  { value: true, label: t('admin.usage.upstreamModelMismatchOnly') },
  { value: false, label: t('admin.usage.upstreamModelMatchedOnly') }
])

const emitChange = () => emit('change')

const clearPendingUserSearch = () => {
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
    userSearchTimeout = null
  }
  userSearchSequence += 1
}

const debounceUserSearch = () => {
  showUserDropdown.value = true
  clearPendingUserSearch()
  userNavigation.reset()
  const query = userKeyword.value.trim()
  if (userResultsKeyword === query) return
  userResults.value = []
  userResultsKeyword = null
  if (!query) return

  const sequence = userSearchSequence
  userSearchTimeout = setTimeout(async () => {
    userSearchTimeout = null
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === userSearchSequence) {
        userResults.value = results.sort((a, b) => Number(a.deleted) - Number(b.deleted))
        userResultsKeyword = query
      }
    } catch {
      if (sequence === userSearchSequence) {
        userResults.value = []
        userResultsKeyword = null
      }
    }
  }, 300)
}

const onUserFocus = () => {
  showUserDropdown.value = true
  const word = userKeyword.value.trim()
  if (word && word !== selectedUserLabel.value && userResultsKeyword !== word && userSearchTimeout === null) debounceUserSearch()
}

const invalidateApiKeySearch = () => {
  if (apiKeySearchTimeout) clearTimeout(apiKeySearchTimeout)
  apiKeySearchTimeout = null
  return ++apiKeySearchSequence
}

const debounceApiKeySearch = () => {
  showApiKeyDropdown.value = true
  const seq = invalidateApiKeySearch()
  const userId = filters.value.user_id
  const keyword = apiKeyKeyword.value
  keyNavigation.reset()
  if (apiKeyResultsContext === apiKeyContext(userId, keyword)) return
  apiKeyResults.value = []; apiKeyResultsContext = null
  apiKeySearchTimeout = setTimeout(async () => {
    apiKeySearchTimeout = null
    try {
      const results = await adminAPI.usage.searchApiKeys(userId, keyword || '')
      if (seq === apiKeySearchSequence && userId === filters.value.user_id) { apiKeyResults.value = results; apiKeyResultsContext = apiKeyContext(userId, keyword) }
    } catch {
      if (seq === apiKeySearchSequence && userId === filters.value.user_id) { apiKeyResults.value = []; apiKeyResultsContext = null }
    }
  }, 300)
}

const selectUser = async (u: SimpleUser) => {
  clearPendingUserSearch(); userNavigation.reset()
  userResults.value = []; userResultsKeyword = null
  userKeyword.value = u.email
  selectedUserLabel.value = u.email
  showUserDropdown.value = false
  const changed = filters.value.user_id !== u.id
  filters.value.user_id = u.id
  if (changed) clearApiKey()
  if (props.showActions) history.record(u.id, u.email)
  if (!props.showActions && changed) {
    const seq = apiKeySearchSequence
    try {
      const results = await adminAPI.usage.searchApiKeys(u.id, '')
      if (seq === apiKeySearchSequence && filters.value.user_id === u.id) { apiKeyResults.value = results; apiKeyResultsContext = apiKeyContext(u.id, '') }
    } catch { if (seq === apiKeySearchSequence) apiKeyResults.value = [] }
    if (seq !== apiKeySearchSequence || filters.value.user_id !== u.id) return
  }
  if (changed) emitChange()
}

const clearUser = () => {
  clearPendingUserSearch()
  userKeyword.value = ''
  selectedUserLabel.value = ''
  userResults.value = []
  userResultsKeyword = null
  showUserDropdown.value = false
  filters.value.user_id = undefined
  clearApiKey()
  emitChange()
}

const selectApiKey = (k: SimpleApiKey) => {
  keyNavigation.reset(); invalidateApiKeySearch()
  apiKeyKeyword.value = k.name || String(k.id)
  selectedKeyLabel.value = apiKeyKeyword.value
  showApiKeyDropdown.value = false
  if (filters.value.api_key_id === k.id) return
  filters.value.api_key_id = k.id
  emitChange()
}

const clearApiKey = () => {
  invalidateApiKeySearch()
  apiKeyKeyword.value = ''
  selectedKeyLabel.value = ''
  apiKeyResults.value = []
  apiKeyResultsContext = null
  showApiKeyDropdown.value = false
  filters.value.api_key_id = undefined
}

const onClearApiKey = () => {
  clearApiKey()
  emitChange()
}

const resetFilters = () => {
  clearPendingUserSearch()
  userKeyword.value = ''
  selectedUserLabel.value = ''
  userResults.value = []
  userResultsKeyword = null
  showUserDropdown.value = false
  clearApiKey()
  emit('reset')
}

const invalidateAccountSearch = () => {
  if (accountSearchTimeout) clearTimeout(accountSearchTimeout)
  accountSearchTimeout = null
  return ++accountSearchSequence
}
const onAccountFocus = (event: FocusEvent) => {
  showAccountDropdown.value = true
  if (!props.showActions) return
  invalidateAccountSearch(); accountSearching.value = false; accountResults.value = []
  ;(event.target as HTMLInputElement | null)?.select()
}
const debounceAccountSearch = () => {
  showAccountDropdown.value = true
  const seq = invalidateAccountSearch()
  const keyword = accountKeyword.value.trim()
  accountSearching.value = !!keyword
  accountResults.value = []
  if (!keyword) return
  accountSearchTimeout = setTimeout(async () => {
    try {
      const res = await adminAPI.accounts.list(1, 20, { search: keyword })
      if (seq === accountSearchSequence) accountResults.value = res.items.map(a => ({ id: a.id, name: a.name }))
    } catch { if (seq === accountSearchSequence) accountResults.value = [] }
  }, 300)
}
const selectAccount = (a: SimpleAccount) => {
  setAccountKeyword(a.name)
  if (filters.value.account_id === a.id) return
  filters.value.account_id = a.id
  emitChange()
}
const clearAccount = () => {
  setAccountKeyword('')
  filters.value.account_id = undefined
  emitChange()
}
const setAccountKeyword = (name: string) => {
  accountNavigation.reset(); invalidateAccountSearch(); accountSearching.value = false; accountResults.value = []; accountKeyword.value = name; selectedAccountLabel.value = name; showAccountDropdown.value = false
}
const setApiKeyKeyword = (name: string) => {
  invalidateApiKeySearch(); apiKeyResults.value = []; apiKeyResultsContext = null; apiKeyKeyword.value = name; selectedKeyLabel.value = name; showApiKeyDropdown.value = false
}

const onApiKeyFocus = () => {
  showApiKeyDropdown.value = true
  // Reuse even an empty successful result for the same user and search.
  if (apiKeyResultsContext !== apiKeyContext(filters.value.user_id, apiKeyKeyword.value) && apiKeySearchTimeout === null) {
    debounceApiKeySearch()
  }
}

const closeUserChoices = () => { showUserDropdown.value = false; clearPendingUserSearch(); userNavigation.reset() }
const closeKeyChoices = () => { showApiKeyDropdown.value = false; invalidateApiKeySearch(); keyNavigation.reset() }
const closeAccountChoices = () => { showAccountDropdown.value = false; invalidateAccountSearch(); accountNavigation.reset() }
// The dropdown's own buttons stay usable; Tab away cancels pending requests.
const onChoiceFocusOut = (event: FocusEvent, container: HTMLElement | null, close: () => void) => {
  const target = event.relatedTarget
  if (target instanceof Node && container && !container.contains(target)) close()
}
const onDocumentClick = (e: MouseEvent) => {
  const target = e.target as Node | null
  if (!target) return

  const clickedInsideUser = userSearchRef.value?.contains(target) ?? false
  const clickedInsideApiKey = apiKeySearchRef.value?.contains(target) ?? false
  const clickedInsideAccount = accountSearchRef.value?.contains(target) ?? false

  if (!clickedInsideUser && showUserDropdown.value) closeUserChoices()
  if (!clickedInsideApiKey && showApiKeyDropdown.value) closeKeyChoices()
  if (!clickedInsideAccount && showAccountDropdown.value) closeAccountChoices()
}

watch(
  () => props.startDate,
  (value) => {
    filters.value.start_date = value
  },
  { immediate: true }
)

watch(
  () => props.endDate,
  (value) => {
    filters.value.end_date = value
  },
  { immediate: true }
)

watch(
  () => filters.value.user_id,
  (userId) => {
    if (!userId) {
      clearPendingUserSearch()
      invalidateApiKeySearch()
      apiKeyKeyword.value = ''
      selectedKeyLabel.value = ''
      apiKeyResults.value = []
      apiKeyResultsContext = null
      userKeyword.value = ''
      selectedUserLabel.value = ''
      userResults.value = []
      userResultsKeyword = null
    }
  }
)

watch(
  () => filters.value.api_key_id,
  (apiKeyId) => {
    if (!apiKeyId) {
      apiKeyKeyword.value = ''
      selectedKeyLabel.value = ''
      apiKeyResults.value = []
      apiKeyResultsContext = null
    }
  }
)

watch(
  () => filters.value.account_id,
  (accountId) => {
    if (!accountId) {
      setAccountKeyword('')
    }
  }
)

let groupsLoading: Promise<void> | undefined
const loadGroups = async () => {
  try {
    const gs = await adminAPI.groups.list(1, 1000)
    fetchedGroups.value = gs.items
  } catch {
    // Ignore filter option loading errors (page still usable)
  }
}
onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  groupsLoading = loadGroups()
})
const waitForGroups = async () => {
  await groupsLoading
  return fetchedGroups.value
}

onUnmounted(() => {
  clearPendingUserSearch()
  invalidateApiKeySearch()
  invalidateAccountSearch()
  document.removeEventListener('click', onDocumentClick)
})

// 供外部(如用户排行下钻)在程序化设置 user_id 后回显选中的用户邮箱
const setUserKeyword = (email: string) => {
  clearPendingUserSearch()
  invalidateApiKeySearch()
  apiKeyResults.value = []
  apiKeyResultsContext = null
  userKeyword.value = email
  selectedUserLabel.value = email
  userResults.value = []
  userResultsKeyword = null
  showUserDropdown.value = false
}

const getUserSearchRevision = () => userSearchSequence

const activeChips = computed(() => {
  const choiceLabel = (key: string, options: SelectOption[]) =>
    options.find(option => option.value === filters.value[key])?.label
  const definitions: Record<string, { label: string; value?: unknown; mode?: 'usage' | 'errors' }> = {
    user_id: { label: t('admin.usage.userFilter'), value: selectedUserLabel.value || filters.value.user_id },
    api_key_id: { label: t('usage.apiKeyFilter'), value: selectedKeyLabel.value || filters.value.api_key_id },
    account_id: { label: t('admin.usage.account'), value: selectedAccountLabel.value || filters.value.account_id },
    group_id: { label: t('admin.usage.group'), value: fetchedGroups.value.find(g => g.id === filters.value.group_id)?.name || selectedGroupLabel.value || '#' + filters.value.group_id },
    model: { label: t('usage.model') },
    request_type: { value: choiceLabel('request_type', requestTypeOptions.value), label: t('usage.type'), mode: 'usage' },
    stream: { label: t('usage.type'), mode: 'usage' },
    native_compaction_v2: { value: choiceLabel('native_compaction_v2', compactionOptions.value), label: t('usage.compactionFilter'), mode: 'usage' },
    billing_type: { value: choiceLabel('billing_type', billingTypeOptions.value), label: t('admin.usage.billingType'), mode: 'usage' },
    billing_mode: { value: choiceLabel('billing_mode', billingModeOptions.value), label: t('admin.usage.billingMode'), mode: 'usage' },
    upstream_model_mismatch: { value: choiceLabel('upstream_model_mismatch', upstreamModelMismatchOptions.value), label: t('admin.usage.upstreamModelAudit'), mode: 'usage' },
    error_phase: { value: choiceLabel('error_phase', errorPhaseOptions.value), label: t('admin.ops.errorLog.type'), mode: 'errors' },
    error_category: { value: choiceLabel('error_category', errorCategoryOptions.value), label: t('usage.errors.category'), mode: 'errors' },
    status_code: { value: choiceLabel('status_code', statusCodeOptions.value), label: t('admin.ops.errorLog.status'), mode: 'errors' },
  }
  return Object.entries(definitions).flatMap(([key, def]) => filters.value[key] == null || filters.value[key] === '' ? [] : [{ key, label: def.label + ': ' + (def.value ?? filters.value[key]) + (def.mode && def.mode !== props.mode ? ' · ' + t(def.mode === 'usage' ? 'admin.usage.onlyUsage' : 'admin.usage.onlyErrors') : '') }])
})
const clearChip = (key: string) => {
  if (key === 'user_id') { clearUser(); return }
  if (key === 'api_key_id') { onClearApiKey(); return }
  if (key === 'account_id') { clearAccount(); return }
  filters.value[key] = undefined; emitChange()
}
const setGroupLabel = (name: string) => { selectedGroupLabel.value = name }
watch(() => filters.value.group_id, id => { if (!id) setGroupLabel('') })
defineExpose({ getUserSearchRevision, setUserKeyword, setAccountKeyword, setApiKeyKeyword, setGroupLabel, waitForGroups })
</script>

<style scoped>
:deep([data-keyboard-active=true]) { outline: 2px solid currentColor; outline-offset: -2px; }
</style>
