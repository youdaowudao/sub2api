<template>
  <section id="ops-error-records" ref="panel" class="card scroll-mt-24" data-testid="ops-error-records" :aria-label="t('admin.ops.records.title')">
    <div class="space-y-3 border-b border-gray-100 p-4 dark:border-dark-700">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.ops.records.title') }}</h2>
        <div class="inline-flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800">
          <button v-for="scope in scopes" :key="scope.value" type="button" :data-testid="`error-scope-${scope.value}`" :aria-pressed="filters.scope === scope.value" class="rounded-md px-3 py-1.5 text-sm transition-colors" :class="filters.scope === scope.value ? 'bg-white font-medium text-primary-700 shadow-sm dark:bg-dark-600 dark:text-primary-300' : 'text-gray-500 dark:text-gray-400'" @click="selectScope(scope.value)">{{ scope.label }}</button>
        </div>
      </div>
      <div class="flex flex-wrap items-end gap-3">
        <div class="min-w-[170px] flex-1 sm:max-w-[220px]">
          <label class="input-label">{{ t('admin.ops.records.finance') }}</label>
          <Select v-model="filters.finance" :options="financeOptions" @change="changeFilters" />
        </div>
        <div class="min-w-[200px] flex-1">
          <label class="sr-only" for="ops-error-search">{{ t('admin.ops.records.search') }}</label>
          <input id="ops-error-search" v-model="searchDraft" class="input" :placeholder="t('admin.ops.records.search')" maxlength="512" />
        </div>
        <label class="flex min-h-[40px] items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
          <input type="checkbox" class="checkbox" :checked="filters.display === 'grouped'" data-testid="error-group-toggle" @change="toggleGrouping" />{{ t('admin.ops.records.merge') }}
        </label>
        <button type="button" class="btn btn-secondary" :aria-expanded="showMore" @click="showMore = !showMore">{{ t('admin.ops.records.more') }}<span v-if="chips.length" class="ml-1 text-primary-600">{{ chips.length }}</span></button>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">{{ t('common.refresh') }}</button>
        <button type="button" class="btn btn-secondary" @click="resetPanel">{{ t('admin.ops.records.reset') }}</button>
        <details class="relative">
          <summary class="btn btn-secondary cursor-pointer">{{ t('admin.users.columnSettings') }}</summary>
          <div class="absolute right-0 z-40 mt-1 max-h-80 w-48 overflow-auto rounded-lg border border-gray-200 bg-white p-2 shadow-lg dark:border-dark-600 dark:bg-dark-800">
            <label v-for="column in allColumns" :key="column.key" class="flex cursor-pointer items-center gap-2 px-2 py-1.5 text-sm text-gray-700 dark:text-gray-200"><input type="checkbox" :checked="visibleColumns.includes(column.key)" :disabled="['message', 'created_at', 'actions'].includes(column.key)" @change="toggleColumn(column.key)" />{{ column.label }}</label>
            <p class="px-2 py-1.5 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.records.hiddenSortReset') }}</p>
          </div>
        </details>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <OpsErrorIdentityFilter kind="user" :id="filters.user_id" :label="t('admin.ops.errorLog.user')" @update:id="changeUser" />
        <OpsErrorIdentityFilter kind="key" v-model:id="filters.api_key_id" :user-id="filters.user_id" :label="t('admin.ops.errorLog.apiKey')" @update:id="changeFilters" />
        <OpsErrorIdentityFilter kind="account" v-model:id="filters.account_id" :label="t('admin.ops.errorLog.account')" @update:id="changeFilters" />
        <div>
          <label class="input-label" for="ops-error-group">{{ t('admin.usage.group') }}</label>
          <Select
            id="ops-error-group"
            :model-value="groupId"
            :options="groupOptions ?? []"
            :aria-label="t('admin.usage.group')"
            aria-describedby="ops-error-group-sync"
            searchable
            @update:model-value="emit('update:group', $event)"
          />
          <p id="ops-error-group-sync" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.records.groupSync') }}</p>
          <p v-if="groupOptionsFailed" role="alert" class="mt-1 text-xs text-rose-700 dark:text-rose-300">
            {{ t('admin.ops.records.groupLoadFailed') }}
            <button type="button" class="underline" @click="emit('retry-groups')">{{ t('admin.ops.records.retry') }}</button>
          </p>
        </div>
      </div>
      <div v-if="showMore" class="grid grid-cols-1 gap-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-900 sm:grid-cols-2 xl:grid-cols-4" data-testid="error-more-filters">
        <div><label class="input-label" for="ops-error-model">{{ t('usage.model') }}</label><input id="ops-error-model" :value="filters.model" class="input" @change="setText('model', $event)" /></div>
        <div><label class="input-label">{{ t('admin.ops.errorLog.type') }}</label><Select v-model="filters.phase" :options="phaseOptions" @change="changeFilters" /></div>
        <div><label class="input-label">{{ t('usage.errors.category') }}</label><Select v-model="filters.category" :options="categoryOptions" @change="changeFilters" /></div>
        <div><label class="input-label">{{ t('admin.ops.errorLog.status') }}</label><Select :model-value="filters.status_codes_other === 'true' ? 'other' : filters.status_codes" :options="statusOptions" @update:model-value="setStatus" /></div>
        <div><label class="input-label">{{ t('admin.ops.records.owner') }}</label><Select v-model="filters.error_owner" :options="ownerOptions" @change="changeFilters" /></div>
        <div><label class="input-label">{{ t('admin.ops.records.view') }}</label><Select v-model="filters.view" :options="viewOptions" @change="changeFilters" /></div>
      </div>
      <div v-if="chips.length" class="flex flex-wrap gap-1.5 text-xs" data-testid="error-active-filters"><span class="py-1 text-gray-400">{{ t('admin.ops.records.activeFilters') }}</span><button v-for="chip in chips" :key="chip.key" type="button" class="rounded-full bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300" :title="t('admin.ops.records.clear')" @click="clearChip(chip.key)">{{ chip.label }} ×</button></div>
      <div class="flex flex-wrap justify-between gap-1 text-xs text-gray-500 dark:text-gray-400" aria-live="polite">
        <span>{{ filters.display === 'grouped' ? t('admin.ops.records.groups', { groups: total, count: totalRecords }) : t('admin.ops.records.rawCount', { count: totalRecords }) }}</span>
        <span v-if="updatedAt">{{ t('admin.ops.records.updated') }} {{ formatDateTime(updatedAt) }}</span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.records.countHint') }}</p>
      <div v-if="windowRange" class="text-xs text-gray-400">{{ t('admin.ops.records.knownWindow', { start: formatDateTime(windowRange.start_time), end: formatDateTime(windowRange.end_time) }) }}</div>
    </div>
    <div v-if="error" role="alert" class="m-4 rounded-lg bg-rose-50 p-3 text-sm text-rose-700 dark:bg-rose-900/20 dark:text-rose-300"><div v-if="updatedAt">{{ t('admin.ops.records.refreshFailed', { time: formatDateTime(updatedAt) }) }}</div>{{ error }} <button type="button" class="ml-2 underline" @click="refresh">{{ t('admin.ops.records.retry') }}</button></div>
    <div v-if="loading" role="status" class="px-4 pt-2 text-xs text-gray-500">{{ t('admin.ops.records.loading') }}</div>
    <OpsErrorLogTable v-if="!error || rows.length" :key="tableRevision" class="ops-records-table" flat :rows="rows" :record-meta="meta" :sort-by="sortBy" :sort-order="sortOrder" :loading="loading" :total="total" :page="page" :page-size="pageSize" :page-size-options="[10, 20, 50]" :visible-column-keys="visibleColumns" :column-order="preferredOrder" :column-width-storage-key="prefix ? prefix + 'column-widths' : undefined" :empty-message="t('admin.ops.records.empty')" user-clickable @openErrorDetail="openRecord" @userClick="openUser" @sort="sort" @update:page="page = $event" @update:pageSize="pageSize = $event" />
    <BaseDialog :key="group?.representative_id || 0" keep-mounted prevent-scroll-on-focus :show="showGroup" :title="t('admin.ops.records.members')" width="full" @close="closeGroup">
      <div v-if="group && !memberLoading && !memberError && memberTotal !== group.count" class="mb-2 text-sm text-amber-700">{{ t('admin.ops.records.membersUpdated', { count: memberTotal }) }}</div>
      <div v-if="memberError" role="alert" class="mb-3 text-sm text-rose-600">{{ memberError }} <button class="ml-2 underline" @click="loadMembers(true)">{{ t('admin.ops.records.retry') }}</button></div>
      <OpsErrorLogTable v-if="!memberError" flat :rows="memberRows" :record-meta="memberMeta" :sort-by="memberSortBy" :sort-order="memberSortOrder" :loading="memberLoading" :total="memberTotal" :page="memberPage" :page-size="memberPageSize" :page-size-options="[10, 20, 50]" :column-width-storage-key="prefix ? prefix + 'members-column-widths' : undefined" user-clickable @openErrorDetail="id => openRecord(id, true)" @userClick="openUser" @sort="sortMembers" @update:page="memberPage = $event" @update:pageSize="memberPageSize = $event" />
    </BaseDialog>
    <OpsErrorDetailModal prevent-scroll-on-focus v-model:show="showDetail" :error-id="detailId" :error-type="detailType" back-to-list @back="backFromDetail" />
    <UserBalanceHistoryModal :show="!!balanceUser" :user="balanceUser" hide-actions @close="balanceUser = null" />
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import UserBalanceHistoryModal from '@/components/admin/user/UserBalanceHistoryModal.vue'
import OpsErrorLogTable from './OpsErrorLogTable.vue'
import OpsErrorDetailModal from './OpsErrorDetailModal.vue'
import OpsErrorIdentityFilter from './OpsErrorIdentityFilter.vue'
import { useOpsErrorRecords, errorRecordRows, errorRecordMeta, readRecordPreference, saveRecordPreference, type RecordFilters } from '../useOpsErrorRecords'
import { formatDateTime } from '../utils/opsFormatters'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
const props = withDefaults(defineProps<{
  timeRange: string
  customStartTime?: string | null
  customEndTime?: string | null
  platform?: string
  groupId?: number | null
  groupOptions?: Array<{ value: number | null; label: string }>
  groupOptionsFailed?: boolean
  refreshToken?: number
}>(), { platform: '', groupId: null, refreshToken: 0 })
const emit = defineEmits<{
  'update:group': [value: string | number | boolean | null]
  'retry-groups': []
}>()
const { t } = useI18n(); const auth = useAuthStore(); const app = useAppStore()
const prefix = computed(() => auth.user?.id ? `admin-ops-error-records:${auth.user.id}:` : undefined)
const { filters, page, pageSize, sortBy, sortOrder, items, total, totalRecords, loading, error, updatedAt, windowRange, changeFilters, reset, refresh, sort, group, showGroup, memberItems, memberTotal, memberPage, memberPageSize, memberSortBy, memberSortOrder, memberLoading, memberError, closeGroup, openGroup, loadMembers, sortMembers } = useOpsErrorRecords(props, () => prefix.value, t)
const rows = computed(() => errorRecordRows(items.value)); const meta = computed(() => errorRecordMeta(items.value))
const memberRows = computed(() => errorRecordRows(memberItems.value)); const memberMeta = computed(() => errorRecordMeta(memberItems.value))
const panel = ref<HTMLElement | null>(null); const showMore = ref(false); const tableRevision = ref(0)
const scopes = computed(() => (['all', 'request', 'upstream'] as const).map(value => ({ value, label: t(`admin.ops.records.${value}`) })))
const financeOptions = computed(() => ['hide', 'include', 'only'].map(value => ({ value, label: t(`admin.ops.records.${value}`) })))
const viewOptions = computed(() => [{ value: 'all', label: t('admin.ops.records.viewAll') }, { value: 'errors', label: t('admin.ops.records.viewErrors') }, { value: 'excluded', label: t('admin.ops.records.viewExcluded') }])
const ownerOptions = computed(() => [{ value: '', label: t('admin.ops.records.ownerAll') }, ...['provider', 'client', 'platform'].map(value => ({ value, label: t(`admin.ops.records.owner${value[0].toUpperCase() + value.slice(1)}`) }))])
const phaseOptions = computed(() => [{ value: '', label: t('common.all') }, { value: 'network', label: t('admin.ops.records.network') }, ...Object.entries({ request: 'typeRequest', auth: 'typeAuth', account_auth: 'typeAccountAuth', routing: 'typeRouting', upstream: 'typeUpstream', internal: 'typeInternal' }).map(([value, key]) => ({ value, label: t(`admin.ops.errorLog.${key}`) }))])
const categoryOptions = computed(() => [{ value: '', label: t('common.all') }, ...['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber'].map(value => ({ value, label: t(`usage.errors.categories.${value}`) }))])
const statusOptions = computed(() => [{ value: '', label: t('common.all') }, { value: '0', label: '0' }, ...COMMON_ERROR_STATUS_CODES.map(code => ({ value: String(code), label: String(code) })), { value: 'other', label: t('admin.ops.records.otherStatus') }])
const preferredOrder = ['message', 'account', 'model', 'user', 'created_at', 'actions', 'api_key', 'platform', 'endpoint', 'group', 'type', 'category', 'status', 'user_agent', 'client_ip']
const allColumns = computed(() => preferredOrder.map(key => ({ key, label: key === 'category' ? t('usage.errors.category') : key === 'user_agent' ? t('usage.userAgent') : t(`admin.ops.errorLog.${({ api_key: 'apiKey', created_at: 'time', actions: 'action', client_ip: 'ip' } as Record<string, string>)[key] || key}`) })))
const hidden = ref<string[]>(readRecordPreference(prefix.value, 'hidden-columns', preferredOrder.filter(key => !['message', 'account', 'model', 'user', 'created_at', 'actions'].includes(key)), (v): v is string[] => Array.isArray(v) && v.every(x => typeof x === 'string')))
const visibleColumns = computed(() => preferredOrder.filter(key => ['message', 'created_at', 'actions'].includes(key) || !hidden.value.includes(key)))
function toggleColumn(key: string) {
  const willHide = !hidden.value.includes(key)
  hidden.value = willHide ? [...hidden.value, key] : hidden.value.filter(x => x !== key)
  if (willHide && sortBy.value === (key === 'status' ? 'status_code' : key)) sort('created_at', 'desc')
  saveRecordPreference(prefix.value, 'hidden-columns', hidden.value)
}
function selectScope(scope: RecordFilters['scope']) { filters.scope = scope; changeFilters() }
function toggleGrouping(event: Event) { filters.display = (event.target as HTMLInputElement).checked ? 'grouped' : 'raw'; changeFilters() }
function setText(key: 'model', event: Event) { filters[key] = (event.target as HTMLInputElement).value.trim(); changeFilters() }
function setStatus(value: unknown) { filters.status_codes = value === 'other' ? '' : String(value ?? ''); filters.status_codes_other = value === 'other' ? 'true' : ''; changeFilters() }
function changeUser(id: number | undefined) { filters.user_id = id; filters.api_key_id = undefined; changeFilters() }
function resetPanel() { searchDraft.value = ''; reset(); tableRevision.value++ }
const searchDraft = ref(filters.q); let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(searchDraft, value => { clearTimeout(searchTimer); if (value === filters.q) return; searchTimer = setTimeout(() => { filters.q = value.trim(); changeFilters() }, 300) })
watch(() => filters.q, value => { if (value !== searchDraft.value.trim()) { clearTimeout(searchTimer); searchDraft.value = value } })
onUnmounted(() => clearTimeout(searchTimer))
const chips = computed(() => {
  const labels: Partial<Record<keyof RecordFilters, string>> = { user_id: t('admin.ops.errorLog.user'), api_key_id: t('admin.ops.errorLog.apiKey'), account_id: t('admin.ops.errorLog.account'), model: t('usage.model'), phase: t('admin.ops.errorLog.type'), category: t('usage.errors.category'), status_codes: t('admin.ops.errorLog.status'), status_codes_other: t('admin.ops.errorLog.status'), error_owner: t('admin.ops.records.owner'), error_source: t('admin.ops.records.source'), resolved: t('admin.ops.records.resolved'), view: t('admin.ops.records.view') }
  return (Object.keys(labels) as Array<keyof RecordFilters>).filter(key => filters[key] !== '' && filters[key] != null && !(key === 'view' && filters.view === 'all')).map(key => {
    const value = filters[key]
    const choices = key === 'view' ? viewOptions.value : key === 'phase' ? phaseOptions.value : key === 'category' ? categoryOptions.value : key === 'error_owner' ? ownerOptions.value : []
    return { key, label: `${labels[key]}: ${key === 'status_codes_other' ? t('admin.ops.records.otherStatus') : key === 'resolved' ? t(filters.resolved === 'true' ? 'admin.ops.records.yesResolved' : 'admin.ops.records.noResolved') : choices.find(x => x.value === value)?.label || value}` }
  })
})
function clearChip(key: keyof RecordFilters) { if (['user_id', 'api_key_id', 'account_id'].includes(key)) Object.assign(filters, { [key]: undefined }); else Object.assign(filters, { [key]: key === 'view' ? 'all' : '' }); changeFilters() }
const showDetail = ref(false); const detailId = ref<number | null>(null); const detailType = ref<'request' | 'upstream'>('request'); let detailFromGroup = false
function openRecord(id: number, fromGroup = false) {
  const record = (fromGroup ? memberMeta.value : meta.value)[id]
  if (!record) return
  if (!fromGroup && record.representative && record.count > 1) { openGroup(record); return }
  detailFromGroup = fromGroup; if (fromGroup) showGroup.value = false
  detailId.value = id; detailType.value = record.is_request_error ? 'request' : 'upstream'; showDetail.value = true
}
function backFromDetail() { showDetail.value = false; if (detailFromGroup && group.value) showGroup.value = true }
const balanceUser = ref<AdminUser | null>(null)
async function openUser(id: number) { try { balanceUser.value = await adminAPI.users.getById(id, true) } catch { app.showError(t('admin.usage.failedToLoadUser')) } }
async function focus() { await nextTick(); panel.value?.scrollIntoView?.({ block: 'start', behavior: 'smooth' }) }
defineExpose({ focus, refresh })
</script>

<style scoped>
.ops-records-table :deep(th), .ops-records-table :deep(td) {
  padding-left: 0.75rem;
  padding-right: 0.75rem;
}
</style>
