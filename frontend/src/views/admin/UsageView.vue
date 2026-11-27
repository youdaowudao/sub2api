<template>
  <AppLayout>
    <div class="space-y-6">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <button v-for="minutes in recentMinutes" :key="minutes" type="button" :class="['btn', recentMode === minutes ? 'btn-primary' : 'btn-secondary']" :aria-pressed="recentMode === minutes" @click="selectRecent(minutes)">{{ minutes === 60 ? t('admin.dashboard.lastHour') : t('admin.usage.recentRange.' + minutes) }}</button>
            <button type="button" class="btn btn-secondary" @click="selectToday">{{ t('dates.today') }}</button>
            <span v-if="preciseWindow" class="text-xs text-gray-500">{{ displayAdminWindow(preciseWindow.start_time, preciseWindow.end_time) }}</span>
            <span v-if="invalidRange" class="text-sm text-rose-600">{{ t('admin.usage.recentRange.invalid') }}</span>
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div v-if="recentMode === null" class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>
        </div>
      <div class="flex flex-wrap items-center gap-3">
        <button type="button" class="btn btn-secondary" :aria-expanded="overviewVisible" :disabled="focusMode" @click="toggleOverview">{{ t(overviewVisible ? 'admin.usage.collapseOverview' : 'admin.usage.expandOverview') }}</button>
        <button type="button" class="btn btn-secondary" :aria-pressed="focusMode" @click="toggleFocus">{{ t(focusMode ? 'admin.usage.exitFocus' : 'admin.usage.focusMode') }}</button>
      </div>
      <div v-show="overviewVisible" class="space-y-6">
      <UsageStatsCards :stats="usageStats" :cache-hit-rate="cacheHitRateTotal" />
      <p v-if="cacheHitRateTotal.window" class="text-xs text-gray-500">{{ t('usage.cacheRate') }}: {{ cacheHitRateTotal.window }}</p>
      <!-- Charts Section -->
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:source="modelDistributionSource"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :upstream-model-stats="upstreamModelStats"
            :mapping-model-stats="mappingModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :start-date="preciseWindow ? undefined : startDate"
            :end-date="preciseWindow ? undefined : endDate"
            :filters="breakdownFilters"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :cache-hit-rate="cacheHitRateGroups"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :start-date="preciseWindow ? undefined : startDate"
            :end-date="preciseWindow ? undefined : endDate"
            :filters="breakdownFilters"
          />
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :title="t('usage.endpointDistribution')"
            :start-date="preciseWindow ? undefined : startDate"
            :end-date="preciseWindow ? undefined : endDate"
            :filters="breakdownFilters"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <AccountPerformanceTable :loading="cacheHitRateAccounts.loading" :error="cacheHitRateAccounts.error" :result="cacheHitRateAccounts.result" :filters="cacheHitRateAccounts.params" />
      </div>
      <!-- 明细区：tab 栏 + 筛选 + 内容收进同一张卡片，消除割裂感 -->
      <div ref="detailSection" class="card scroll-mt-24">
        <div class="flex flex-wrap items-center border-b border-gray-200 px-2 dark:border-dark-700 sm:px-4">
          <button
            v-for="tab in detailTabs"
            :key="tab.key"
            type="button"
            data-testid="usage-detail-tab"
            class="-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-3 text-sm font-medium transition-colors sm:px-4"
            :class="activeTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
            @click="switchTab(tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>

        <UsageFilters v-model="filters" ref="usageFiltersRef" flat :mode="activeTab" class="border-b border-gray-100 dark:border-dark-700/50" :start-date="startDate" :end-date="endDate" :exporting="exporting" :disable-cleanup="!!preciseWindow || invalidRange" :model-options="modelNameOptions" :model-ranking-available="modelRankingAvailable" :account-candidates="accountCandidates" :candidate-window="accountCandidateWindow" :candidate-previous="!!filters.account_id" @clear-preserve-time="clearFiltersKeepTime" @change="applyFilters" @refresh="refreshData" @reset="resetFilters" @cleanup="openCleanupDialog" @export="exportToExcel">
          <template #after-reset>
            <div v-if="activeTab !== 'ranking'" class="relative" ref="columnDropdownRef">
              <button
                data-testid="usage-column-settings"
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                </svg>
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in currentToggleableColumns"
                  :key="col.key"
                  :data-testid="`usage-column-toggle-${col.key}`"
                  @click="toggleCurrentColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isCurrentColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
          </template>
        </UsageFilters>

        <div v-show="activeTab === 'usage'" class="overflow-hidden rounded-b-2xl">
          <UsageTable
            flat
            column-width-storage-key="admin-usage-column-widths"
            :data="usageLogs"
            :loading="loading"
            :columns="visibleColumns"
            :server-side-sort="true"
            :default-sort-key="'created_at'"
            :default-sort-order="'desc'"
            :controlled-sort="{ key: sortState.sort_by, order: sortState.sort_order }"
            top-horizontal-scroll quick-filters
            @quick-filter="applyQuickFilter" @manage-account="manageAccount"
            @sort="handleSort"
            @userClick="handleUserClick"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
          <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="handlePageChange" @update:pageSize="handlePageSizeChange" />
        </div>
        <div v-if="errorsMounted" v-show="activeTab === 'errors'" class="rounded-b-2xl">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-2 text-xs text-gray-500 dark:border-dark-700">
            <label class="flex items-center gap-2"><input type="checkbox" :checked="errorDisplay === 'grouped'" @change="toggleErrorDisplay" />{{ t('admin.ops.records.merge') }}</label>
            <span>{{ errorDisplay === 'grouped' ? t('admin.ops.records.groups', { groups: errorTotal, count: errorTotalRecords }) : t('admin.ops.records.rawCount', { count: errorTotalRecords }) }}</span>
          </div>
          <div v-if="errorListError" role="alert" class="m-4 rounded-lg bg-rose-50 p-3 text-sm text-rose-700 dark:bg-rose-900/20 dark:text-rose-300">
            {{ errorListError }}
            <button type="button" class="ml-2 underline" @click="loadErrorRequests">{{ t('admin.ops.records.retry') }}</button>
          </div>
          <OpsErrorLogTable
            v-else
            flat
            user-clickable
            column-width-storage-key="admin-usage-error-column-widths"
            :rows="errorRows"
            :record-meta="errorMeta"
            :column-order="errorRecordColumnOrder"
            top-horizontal-scroll quick-filters
            @quick-filter="applyQuickFilter"
            :total="errorTotal"
            :loading="errorLoading"
            :page="errorPage"
            :page-size="errorPageSize"
            :page-size-options="errorPageSizeOptions"
            :sort-by="errorSortBy"
            :sort-order="errorSortOrder"
            :visible-column-keys="errorVisibleColumnKeys"
            @sort="sortErrors"
            @update:page="changeErrorPage"
            @update:pageSize="changeErrorPageSize"
            @userClick="handleUserClick"
            @openErrorDetail="openErrorDetail"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
          <OpsErrorRecordMembersDialog :members="errorMembers" column-width-storage-key="admin-usage-error-members-column-widths" quick-filters @openErrorDetail="id => openErrorDetail(id, true)" @userClick="handleUserClick" @quick-filter="applyQuickFilter" />
          <OpsErrorDetailModal prevent-scroll-on-focus :show="showErrorDetail" @update:show="value => { if (!value) backFromErrorDetail() }" :error-id="errorDetailId" :error-type="errorDetailType" back-to-list @back="backFromErrorDetail" />
        </div>
        <!-- 懒挂载：首次切到该 tab 才请求排行数据，之后随筛选自动刷新 -->
        <div v-if="rankingMounted && !invalidRange" v-show="activeTab === 'ranking'" class="overflow-hidden rounded-b-2xl">
          <UserTokenRanking
            ref="rankingRef"
            :start-date="preciseWindow ? undefined : startDate"
            :end-date="preciseWindow ? undefined : endDate"
            :filters="breakdownFilters"
            :model="filters.model"
            @select-user="handleRankingSelectUser"
          />
        </div>
      </div>
    </div>
  </AppLayout>
  <UsageExportProgress :show="exportProgress.show" :progress="exportProgress.progress" :current="exportProgress.current" :total="exportProgress.total" :estimated-time="exportProgress.estimatedTime" @cancel="cancelExport" />
  <UsageCleanupDialog
    :show="cleanupDialogVisible"
    :filters="filters"
    :start-date="startDate"
    :end-date="endDate"
    @close="cleanupDialogVisible = false"
  />
  <!-- Balance history modal triggered from usage table user click -->
  <UserBalanceHistoryModal
    :show="showBalanceHistoryModal"
    :user="balanceHistoryUser"
    :hide-actions="true"
    @close="showBalanceHistoryModal = false; balanceHistoryUser = null"
  >
    <template #query-actions><button type="button" class="btn btn-primary" @click="queryBalanceUser">{{ t('admin.usage.queryThisUser') }}</button></template>
  </UserBalanceHistoryModal>
</template>

<script setup lang="ts">
import { getTodayRangeDates } from '@/utils/dateRange'
import type { CacheHitRateResult } from '@/types'
import AccountPerformanceTable from '@/components/admin/usage/AccountPerformanceTable.vue'
import { recentMinutes, makeRecentRange, restoreRecentRange, displayAdminWindow, localMinuteLabel, type RecentMinutes, type PreciseAdminRange } from '@/utils/adminRecentRange'
import { extractApiErrorMessage } from '@/utils/apiError'
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'; import { adminAPI } from '@/api/admin'; import { adminUsageAPI } from '@/api/admin/usage'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatDateLocalInput, formatReasoningEffort } from '@/utils/format'
import { getConfiguredTablePageSizeOptions } from '@/utils/tablePreferences'
import { opsAPI, type OpsErrorLog, type OpsErrorRecord, type OpsErrorRecordsParams } from '@/api/admin/ops'
import OpsErrorLogTable from './ops/components/OpsErrorLogTable.vue'
import OpsErrorDetailModal from './ops/components/OpsErrorDetailModal.vue'
import { errorRecordRows, errorRecordMeta, readRecordPreference, saveRecordPreference } from './ops/useOpsErrorRecords'
import { useErrorRecordMembers } from './ops/useErrorRecordMembers'
import OpsErrorRecordMembersDialog from './ops/components/OpsErrorRecordMembersDialog.vue'
import { errorRecordColumnOrder, errorRecordDefaultVisible, errorRecordAlwaysVisible } from './ops/errorRecordPresentation'
import type { UsageQuickFilter } from '@/utils/usageQuickFilter'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import AppLayout from '@/components/layout/AppLayout.vue'; import Pagination from '@/components/common/Pagination.vue'; import Select from '@/components/common/Select.vue'; import DateRangePicker from '@/components/common/DateRangePicker.vue'
import { useAdminUserHistory } from '@/composables/useAdminUserHistory'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'; import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'; import UsageExportProgress from '@/components/admin/usage/UsageExportProgress.vue'
import UserTokenRanking from '@/components/admin/usage/UserTokenRanking.vue'
import UsageCleanupDialog from '@/components/admin/usage/UsageCleanupDialog.vue'
import UserBalanceHistoryModal from '@/components/admin/user/UserBalanceHistoryModal.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'; import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'; import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import Icon from '@/components/icons/Icon.vue'
import type { AdminUsageLog, TrendDataPoint, ModelStat, GroupStat, EndpointStat, AdminUser } from '@/types'; import type { AdminUsageStatsResponse, AdminUsageQueryParams } from '@/api/admin/usage'

const { t } = useI18n()
const appStore = useAppStore()
const userHistory = useAdminUserHistory()
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
const route = useRoute()
const router = useRouter()
const detailSection = ref<HTMLElement | null>(null)
const overviewExpanded = ref(readRecordPreference('admin-usage-', 'overview-expanded', true, (v): v is boolean => typeof v === 'boolean'))
const focusMode = ref(false)
let previousSidebar = false
const overviewVisible = computed(() => overviewExpanded.value && !focusMode.value)
const toggleOverview = () => { if (focusMode.value) return; overviewExpanded.value = !overviewExpanded.value; saveRecordPreference('admin-usage-', 'overview-expanded', overviewExpanded.value) }
const toggleFocus = () => { if (!focusMode.value) { previousSidebar = appStore.sidebarCollapsed; appStore.setSidebarCollapsed(true); focusMode.value = true } else { appStore.setSidebarCollapsed(previousSidebar); focusMode.value = false } }
const scrollToDetails = () => detailSection.value?.scrollIntoView?.({ block: 'start' })
const usageStats = ref<AdminUsageStatsResponse | null>(null); const usageLogs = ref<AdminUsageLog[]>([]); const loading = ref(false); const exporting = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const requestedModelStats = ref<ModelStat[]>([]); const upstreamModelStats = ref<ModelStat[]>([]); const mappingModelStats = ref<ModelStat[]>([]); const groupStats = ref<GroupStat[]>([]); const chartsLoading = ref(false); const modelStatsLoading = ref(false); const granularity = ref<'day' | 'hour' | 'minute'>('hour'); const recentMode = ref<RecentMinutes | null>(null); const preciseWindow = ref<PreciseAdminRange | null>(null); const invalidRange = ref(false)
const modelDistributionMetric = ref<DistributionMetric>('tokens')
const modelDistributionSource = ref<ModelDistributionSource>('requested')
const loadedModelSources = reactive<Record<ModelDistributionSource, boolean>>({
  requested: false,
  upstream: false,
  mapping: false,
})
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const endpointStatsLoading = ref(false)
let abortController: AbortController | null = null; let exportAbortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0
const exportProgress = reactive({ show: false, progress: 0, current: 0, total: 0, estimatedTime: '' })
const cleanupDialogVisible = ref(false)
// Balance history modal state
const showBalanceHistoryModal = ref(false)
const balanceHistoryUser = ref<AdminUser | null>(null)

const breakdownFilters = computed(() => {
  const f: Record<string, any> = { ...(preciseWindow.value ?? {}) }
  if (filters.value.user_id) f.user_id = filters.value.user_id
  if (filters.value.api_key_id) f.api_key_id = filters.value.api_key_id
  if (filters.value.account_id) f.account_id = filters.value.account_id
  if (filters.value.group_id) f.group_id = filters.value.group_id
  if (filters.value.request_type != null) f.request_type = filters.value.request_type
  if (filters.value.native_compaction_v2 != null) f.native_compaction_v2 = filters.value.native_compaction_v2
  if (filters.value.billing_type != null) f.billing_type = filters.value.billing_type
  return f
})

const stableCandidateKey = (params: Record<string, any>, omit: string[]) => JSON.stringify(Object.entries(params).filter(([key, value]) => !omit.includes(key) && value != null && value !== '').sort(([a], [b]) => a.localeCompare(b)))
const modelContext = () => {
  const request = filters.value.request_type
  const stream = request ? requestTypeToLegacyStream(request) : filters.value.stream
  return stableCandidateKey({ ...rangeParams(), user_id: filters.value.user_id, api_key_id: filters.value.api_key_id, account_id: filters.value.account_id, group_id: filters.value.group_id, request_type: request, stream, native_compaction_v2: filters.value.native_compaction_v2, billing_type: filters.value.billing_type, upstream_model_mismatch: filters.value.upstream_model_mismatch }, [])
}
const modelRankingKey = ref('')
const modelRankingAvailable = computed(() => modelRankingKey.value !== '' && modelRankingKey.value === modelContext() && requestedModelStats.value.length > 0)
const modelNameOptions = computed(() => {
  if (!modelRankingAvailable.value) return []
  return requestedModelStats.value.filter(m => !!m.model).slice().sort((a,b) => (b.total_tokens ?? 0) - (a.total_tokens ?? 0) || (b.requests ?? 0) - (a.requests ?? 0) || a.model.localeCompare(b.model)).map(m => ({ value: m.model, label: `${m.model} · ${(m.total_tokens ?? 0).toLocaleString()} Token · ${(m.requests ?? 0).toLocaleString()} ${t('admin.accounts.stats.requests')}` }))
})

const handleUserClick = async (userId: number) => {
  const actor = userHistory.adminId.value
  try {
    const user = await adminAPI.users.getById(userId, true)
    userHistory.record(user.id, user.email, actor)
    balanceHistoryUser.value = user
    showBalanceHistoryModal.value = true
  } catch {
    appStore.showError(t('admin.usage.failedToLoadUser'))
  }
}

// Drill down from the per-user token ranking: scope the whole usage view to
// that user and jump to the usage-detail tab so the drill-down is visible.
const setQuickUser = (id: number, email: string) => {
  const changed = filters.value.user_id !== id
  userHistory.record(id, email || '#' + id)
  if (changed) { filters.value.user_id = id; filters.value.api_key_id = undefined; usageFiltersRef.value?.setApiKeyKeyword?.('') }
  usageFiltersRef.value?.setUserKeyword?.(email || '#' + id)
  return changed
}
const handleRankingSelectUser = (userId: number, email: string) => {
  setQuickUser(userId, email)
  activeTab.value = 'usage'
  applyFilters()
  scrollToDetails()
}
const queryBalanceUser = () => {
  const user = balanceHistoryUser.value
  if (!user) return
  const changed = setQuickUser(user.id, user.email)
  showBalanceHistoryModal.value = false; balanceHistoryUser.value = null
  if (changed) { errorMembers.closeGroup(); showErrorDetail.value = false; applyFilters() }
  scrollToDetails()
}
const applyQuickFilter = (filter: UsageQuickFilter) => {
  let changed = false
  if (filter.field === 'api_key_id' && filter.userId) changed = setQuickUser(filter.userId, filter.userLabel || '') || changed
  changed = filters.value[filter.field] !== filter.value || changed
  Object.assign(filters.value, { [filter.field]: filter.value })
  if (filter.field === 'account_id') usageFiltersRef.value?.setAccountKeyword?.(filter.label || '#' + filter.value)
  if (filter.field === 'api_key_id') usageFiltersRef.value?.setApiKeyKeyword?.(filter.label || '#' + filter.value)
  if (filter.field === 'group_id') usageFiltersRef.value?.setGroupLabel?.(filter.label || '#' + filter.value)
  errorMembers.closeGroup(); showErrorDetail.value = false
  if (changed) applyFilters()
  scrollToDetails()
}
const manageAccount = (id: number) => { void router.push({ path: '/admin/accounts', query: { account_id: String(id) } }) }

const granularityOptions = computed(() => [{ value: 'day', label: t('admin.dashboard.day') }, { value: 'hour', label: t('admin.dashboard.hour') }])
// Use local timezone to avoid UTC timezone issues
const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  const daysDiff = Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24))
  return daysDiff <= 1 ? 'hour' : 'day'
}
const defaultRange = getTodayRangeDates()
const startDate = ref(defaultRange.start); const endDate = ref(defaultRange.end)
const filters = ref<AdminUsageQueryParams>({ user_id: undefined, model: undefined, group_id: undefined, request_type: undefined, native_compaction_v2: null, billing_type: null, start_date: startDate.value, end_date: endDate.value })
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const sortState = reactive(readUsageSort('usage'))

const getSingleQueryValue = (value: string | null | Array<string | null> | undefined): string | undefined => {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

const getNumericQueryValue = (value: string | null | Array<string | null> | undefined): number | undefined => {
  const raw = getSingleQueryValue(value)
  if (!raw) return undefined
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

const applyRouteQueryFilters = () => {
  const queryStartDate = getSingleQueryValue(route.query.start_date)
  const queryEndDate = getSingleQueryValue(route.query.end_date)
  const queryUserId = getNumericQueryValue(route.query.user_id)
  const hasExact = route.query.start_time !== undefined || route.query.end_time !== undefined
  if (hasExact) {
    const restored = restoreRecentRange(route.query.start_time, route.query.end_time)
    if (!restored || route.query.start_date !== undefined || route.query.end_date !== undefined) invalidRange.value = true
    else { recentMode.value = restored.minutes; preciseWindow.value = restored.range }
  }

  if (queryStartDate) {
    startDate.value = queryStartDate
  }
  if (queryEndDate) {
    endDate.value = queryEndDate
  }

  filters.value = {
    ...filters.value,
    user_id: queryUserId,
    start_date: startDate.value,
    end_date: endDate.value
  }
  for (const key of restoredStringFilters) { const value = getSingleQueryValue(route.query[key]); if (value) Object.assign(filters.value, { [key]: value }) }
  for (const key of restoredNumberFilters) { const value = getNumericQueryValue(route.query[key]); if (value !== undefined) Object.assign(filters.value, { [key]: value }) }
  if (filters.value.account_id) usageFiltersRef.value?.setAccountKeyword?.('#' + filters.value.account_id)
  if (filters.value.api_key_id) usageFiltersRef.value?.setApiKeyKeyword?.('#' + filters.value.api_key_id)
  for (const key of restoredBooleanFilters) { const value = getSingleQueryValue(route.query[key]); if (value === 'true' || value === 'false') Object.assign(filters.value, { [key]: value === 'true' }) }
  granularity.value = preciseWindow.value ? 'minute' : getGranularityForRange(startDate.value, endDate.value)
}

const loadRouteUserFilterLabel = async () => {
  const requestedUserId = filters.value.user_id
  const actor = userHistory.adminId.value
  if (!requestedUserId) return
  const userSearchRevision = usageFiltersRef.value?.getUserSearchRevision?.()

  const routeUserFilterIsCurrent = () => (
    filters.value.user_id === requestedUserId
    && usageFiltersRef.value?.getUserSearchRevision?.() === userSearchRevision
  )

  try {
    const user = await adminAPI.users.getById(requestedUserId, true)
    if (!routeUserFilterIsCurrent()) return
    userHistory.record(user.id, user.email, actor)
    usageFiltersRef.value?.setUserKeyword?.(user.email || String(requestedUserId))
  } catch {
    if (!routeUserFilterIsCurrent()) return
    usageFiltersRef.value?.setUserKeyword?.(String(requestedUserId))
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  invalidRange.value = false; recentMode.value = null; preciseWindow.value = null
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value = {
    ...filters.value,
    start_date: range.startDate,
    end_date: range.endDate
  }
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const selectRecent = (minutes: RecentMinutes) => {
  invalidRange.value = false; recentMode.value = minutes; preciseWindow.value = makeRecentRange(minutes)
  granularity.value = 'minute'; cleanupDialogVisible.value = false
  applyFilters()
}
const selectToday = () => { const today = getTodayRangeDates(); onDateRangeChange({ startDate: today.start, endDate: today.end, preset: 'today' }) }
const rangeParams = () => preciseWindow.value ?? { start_date: startDate.value, end_date: endDate.value }
const readFilters = () => {
  const rest = { ...filters.value }
  delete rest.start_date; delete rest.end_date; delete rest.start_time; delete rest.end_time
  return { ...rest, ...rangeParams() }
}

const buildUsageListParams = (
  page: number,
  pageSize: number,
  exactTotal: boolean
): AdminUsageQueryParams => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    page,
    page_size: pageSize,
    exact_total: exactTotal,
    ...readFilters(),
    stream: legacyStream === null ? undefined : legacyStream,
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}

const loadLogs = async () => {
  if (invalidRange.value) return
  abortController?.abort(); const c = new AbortController(); abortController = c; loading.value = true
  try {
    const res = await adminAPI.usage.list(
      buildUsageListParams(pagination.page, pagination.page_size, false),
      { signal: c.signal }
    )
    if(!c.signal.aborted) { usageLogs.value = res.items; pagination.total = res.total }
  } catch (error: any) { if(error?.name !== 'AbortError') console.error('Failed to load usage logs:', error) } finally { if(abortController === c) loading.value = false }
}
const cacheHitRateTotal = reactive({ loading: false, error: null as string | null, rate: null as number | null, window: '' })
const cacheHitRateGroups = reactive({ loading: false, error: null as string | null, byId: {} as Record<number, number | null>, window: '' })
const cacheHitRateAccounts = reactive({ loading: false, error: null as string | null, result: null as CacheHitRateResult | null, params: {} as AdminUsageQueryParams })
const accountRankingSnapshot = ref<{ key: string; result: CacheHitRateResult } | null>(null)
const accountCandidateKey = (params: Record<string, any>) => stableCandidateKey(params, ['account_id','nocache'])
const accountCandidateResult = computed(() => {
  const key = accountCandidateKey(readFilters())
  if (!filters.value.account_id && cacheHitRateAccounts.loading) return null
  return accountRankingSnapshot.value?.key === key ? accountRankingSnapshot.value.result : null
})
const accountCandidates = computed(() => (accountCandidateResult.value?.items ?? []).filter(row => row.requests > 0).slice().sort((a,b) => b.requests-a.requests || a.id-b.id).slice(0,20).map(row => ({ id: row.id, name: row.name || '#' + row.id, requests: row.requests, deleted: row.account_exists === false })))
const accountCandidateWindow = computed(() => {
  const r = accountCandidateResult.value
  return r ? displayAdminWindow(r.window_start, r.window_end) || [r.start_date, r.end_date].join(' – ') : ''
})
let cacheHitRateReqSeq = 0
const loadCacheHitRates = async (force = false) => {
  const seq = ++cacheHitRateReqSeq
  const params = { ...readFilters(), ...(force ? { nocache: 1 } : {}) }
  const states = { total: cacheHitRateTotal, group: cacheHitRateGroups, account: cacheHitRateAccounts }
  await Promise.all((['total', 'group', 'account'] as const).map(async dimension => {
    const state = states[dimension]
    state.loading = true; state.error = null
    if (dimension === 'account') { cacheHitRateAccounts.result = null; if (!params.account_id) accountRankingSnapshot.value = null }
    if (dimension === 'total') cacheHitRateTotal.window = ''
    if (dimension === 'group') cacheHitRateGroups.window = ''
    try {
      const result = await adminAPI.usage.getCacheHitRate({ ...params, dimension, ...(dimension === 'account' ? { include_performance: 1 } : {}) })
      if (seq !== cacheHitRateReqSeq) return
      if (dimension === 'total') { cacheHitRateTotal.rate = result.items[0]?.cache_hit_rate ?? null; cacheHitRateTotal.window = displayAdminWindow(result.window_start, result.window_end) || (result.start_date && result.end_date ? result.start_date + ' – ' + result.end_date : '') }
      if (dimension === 'group') {
        cacheHitRateGroups.byId = Object.fromEntries(result.items.map(item => [item.id, item.cache_hit_rate]))
        cacheHitRateGroups.window = displayAdminWindow(result.window_start, result.window_end) || (result.start_date && result.end_date ? result.start_date + ' – ' + result.end_date : '')
      }
      if (dimension === 'account') { cacheHitRateAccounts.result = result; cacheHitRateAccounts.params = params; if (!params.account_id) accountRankingSnapshot.value = { key: accountCandidateKey(params), result } }
    } catch (error) {
      if (seq !== cacheHitRateReqSeq) return
      state.error = extractApiErrorMessage(error)
    } finally {
      if (seq === cacheHitRateReqSeq) state.loading = false
    }
  }))
}

const loadStats = async (force = false) => {
  if (invalidRange.value) return
  void loadCacheHitRates(force)
  const seq = ++statsReqSeq
  endpointStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const s = await adminAPI.usage.getStats({
      ...readFilters(),
      stream: legacyStream === null ? undefined : legacyStream,
      ...(force ? { nocache: 1 } : {}),
    })
    if (seq !== statsReqSeq) return
    usageStats.value = s
    inboundEndpointStats.value = s.endpoints || []
    upstreamEndpointStats.value = s.upstream_endpoints || []
    endpointPathStats.value = s.endpoint_paths || []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

// 失效模型统计缓存:仅标记需要重取,保留旧数据直到新数据到达(避免刷新时图表闪空)。
const invalidateModelStatsCache = () => {
  loadedModelSources.requested = false
  loadedModelSources.upstream = false
  loadedModelSources.mapping = false
}

const loadModelStats = async (source: ModelDistributionSource, force = false) => {
  if (invalidRange.value) return
  if (!force && loadedModelSources[source]) {
    return
  }

  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const baseParams = {
      ...rangeParams(),
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      native_compaction_v2: filters.value.native_compaction_v2,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
    }

    const candidateKey = modelContext()
    const response = await adminAPI.dashboard.getModelStats({ ...baseParams, model_source: source })

    if (seq !== modelStatsReqSeq) return

    const models = response.models || []
    if (source === 'requested') {
      requestedModelStats.value = models; modelRankingKey.value = candidateKey
    } else if (source === 'upstream') {
      upstreamModelStats.value = models
    } else {
      mappingModelStats.value = models
    }
    loadedModelSources[source] = true
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
    if (source === 'requested') {
      requestedModelStats.value = []; modelRankingKey.value = ''
    } else if (source === 'upstream') {
      upstreamModelStats.value = []
    } else {
      mappingModelStats.value = []
    }
    loadedModelSources[source] = false
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  if (invalidRange.value) return
  const seq = ++chartReqSeq
  chartsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const snapshot = await adminAPI.dashboard.getSnapshotV2({
      ...rangeParams(),
      granularity: granularity.value,
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      native_compaction_v2: filters.value.native_compaction_v2,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
      include_stats: false,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
      include_users_trend: false
    })
    if (seq !== chartReqSeq) return
    trendData.value = preciseWindow.value ? (snapshot.trend || []).map(point => ({ ...point, date: localMinuteLabel(point.date) })) : snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) { console.error('Failed to load chart data:', error) } finally { if (seq === chartReqSeq) chartsLoading.value = false }
}
const applyFilters = () => {
  pagination.page = 1
  errorPage.value = 1
  if (activeTab.value === 'errors') void loadErrorRequests()
  invalidateModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
}
const refreshData = () => {
  const old = preciseWindow.value?.end_time
  if (recentMode.value !== null) preciseWindow.value = makeRecentRange(recentMode.value)
  invalidateModelStatsCache()
  loadLogs()
  loadStats(true)
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeTab.value === 'errors') void loadErrorRequests()
  if (rankingMounted.value && old === preciseWindow.value?.end_time) rankingRef.value?.reload()
}
const clearQueryLabels = () => { usageFiltersRef.value?.setUserKeyword?.(''); usageFiltersRef.value?.setApiKeyKeyword?.(''); usageFiltersRef.value?.setAccountKeyword?.(''); usageFiltersRef.value?.setGroupLabel?.('') }
const clearFiltersKeepTime = () => {
  clearQueryLabels()
  filters.value = { start_date: startDate.value, end_date: endDate.value, request_type: undefined, native_compaction_v2: null, billing_type: null, billing_mode: undefined }; applyFilters()
}
const resetFilters = () => {
  clearQueryLabels()
  invalidRange.value = false; recentMode.value = null; preciseWindow.value = null
  const range = getTodayRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = { start_date: startDate.value, end_date: endDate.value, request_type: undefined, native_compaction_v2: null, billing_type: null, billing_mode: undefined }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
  applyFilters()
}
const handlePageChange = (p: number) => { pagination.page = p; loadLogs() }
const handlePageSizeChange = (s: number) => { pagination.page_size = s; pagination.page = 1; loadLogs() }
function readUsageSort(mode: 'usage' | 'errors'): {sort_by: string; sort_order: 'asc' | 'desc'} {
  const allowed = mode === 'usage' ? ['created_at','model'] : ['created_at','model','status_code']
  return readRecordPreference('admin-usage-', mode + '-sort', { sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' }, (v): v is {sort_by: string; sort_order: 'asc' | 'desc'} => !!v && typeof v === 'object' && allowed.includes((v as any).sort_by) && ['asc','desc'].includes((v as any).sort_order))
}
const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  saveRecordPreference('admin-usage-', 'usage-sort', { ...sortState })
  pagination.page = 1
  loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}
const cancelExport = () => exportAbortController?.abort()
const openCleanupDialog = () => { if (!preciseWindow.value && !invalidRange.value) cleanupDialogVisible.value = true }
const getRequestTypeLabel = (log: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'cyber') return t('usage.cyber')
  if (requestType === 'live') return t('usage.live')
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const exportToExcel = async () => {
  if (exporting.value || invalidRange.value) return; exporting.value = true; exportProgress.show = true
  const c = new AbortController(); exportAbortController = c
  try {
    let p = 1; let total = pagination.total; let exportedCount = 0
    const frozenParams = buildUsageListParams(1, 100, true)
    const XLSX = await import('xlsx')
    const headers = [
      t('usage.time'), t('admin.usage.user'), t('usage.apiKeyFilter'),
      t('admin.usage.account'), t('usage.requestedModel'), t('usage.sentUpstreamModel'), t('usage.upstreamResponseModel'), t('usage.upstreamModelMismatch'), t('usage.requestedReasoningEffort'), t('usage.reasoningEffort'), t('admin.usage.group'),
      t('usage.inboundEndpoint'), t('usage.upstreamEndpoint'),
      t('usage.type'),
      t('admin.usage.inputTokens'), t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'), t('admin.usage.cacheCreationTokens'),
      t('admin.usage.inputCost'), t('admin.usage.outputCost'),
      t('admin.usage.cacheReadCost'), t('admin.usage.cacheCreationCost'),
      t('usage.rate'), t('usage.accountMultiplier'), t('usage.original'), t('usage.userBilled'), t('usage.accountBilled'),
      t('usage.firstToken'), t('usage.duration'),
      t('admin.usage.requestId'), t('admin.usage.upstreamRequestId'), t('usage.userAgent'), t('admin.usage.ipAddress')
    ]
    const ws = XLSX.utils.aoa_to_sheet([headers])
    while (true) {
      const res = await adminUsageAPI.list(
        { ...frozenParams, page: p },
        { signal: c.signal }
      )
      if (c.signal.aborted) break; if (p === 1) { total = res.total; exportProgress.total = total }
      const rows = (res.items || []).map((log: AdminUsageLog) => [
        log.created_at, log.user?.email || '', log.api_key?.name || '', log.account?.name || '', log.model,
        log.upstream_model || log.model, log.upstream_response_model || '', log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'), formatReasoningEffort(log.reasoning_effort), formatReasoningEffort(log.upstream_reasoning_effort || log.reasoning_effort), log.group?.name || '',
        log.inbound_endpoint || '', log.upstream_endpoint || '', getRequestTypeLabel(log),
        log.input_tokens, log.output_tokens, log.cache_read_tokens, log.cache_creation_tokens,
        log.input_cost?.toFixed(6) || '0.000000', log.output_cost?.toFixed(6) || '0.000000',
        log.cache_read_cost?.toFixed(6) || '0.000000', log.cache_creation_cost?.toFixed(6) || '0.000000',
        log.rate_multiplier?.toPrecision(4) || '1.00', (log.account_rate_multiplier ?? 1).toPrecision(4),
        log.total_cost?.toFixed(6) || '0.000000', log.actual_cost?.toFixed(6) || '0.000000',
        ((log.account_stats_cost ?? log.total_cost) * (log.account_rate_multiplier ?? 1)).toFixed(6), log.first_token_ms ?? '', log.duration_ms,
        log.request_id || '', log.upstream_request_id || '', log.user_agent || '', log.ip_address || ''
      ])
      if (rows.length) {
        XLSX.utils.sheet_add_aoa(ws, rows, { origin: -1 })
      }
      exportedCount += rows.length
      exportProgress.current = exportedCount
      exportProgress.progress = total > 0 ? Math.min(100, Math.round(exportedCount / total * 100)) : 0
      if (exportedCount >= total || res.items.length < 100) break; p++
    }
    if(!c.signal.aborted) {
      const wb = XLSX.utils.book_new()
      XLSX.utils.book_append_sheet(wb, ws, 'Usage')
      saveAs(new Blob([XLSX.write(wb, { bookType: 'xlsx', type: 'array' })], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }), `usage_${filters.value.start_date}_to_${filters.value.end_date}.xlsx`)
      appStore.showSuccess(t('usage.exportSuccess'))
    }
  } catch (error) { console.error('Failed to export:', error); appStore.showError('Export Failed') }
  finally { if(exportAbortController === c) { exportAbortController = null; exporting.value = false; exportProgress.show = false } }
}

// Column visibility
const ALWAYS_VISIBLE = ['user', 'created_at']
const DEFAULT_HIDDEN_COLUMNS = ['reasoning_effort', 'request_id', 'upstream_request_id', 'user_agent']
const HIDDEN_COLUMNS_KEY = 'usage-hidden-columns'
const HIDDEN_COLUMNS_VERSION_KEY = 'usage-hidden-columns-version'
// 隐藏列版本链：每级只把当级新增列加入隐藏集，不重置用户已显式打开的列。
const HIDDEN_COLUMNS_PREV_VERSION = 'request-id-hidden-by-default'
const HIDDEN_COLUMNS_CURRENT_VERSION = 'upstream-request-id-hidden-by-default'

const allColumns = computed(() => [
  { key: 'user', label: t('admin.usage.user'), sortable: false },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'account', label: t('admin.usage.account'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'request_id', label: t('admin.usage.requestId'), sortable: false },
  { key: 'upstream_request_id', label: t('admin.usage.upstreamRequestId'), sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
  { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false }
])

const hiddenColumns = reactive<Set<string>>(new Set())

const toggleableColumns = computed(() =>
  allColumns.value.filter(col => !ALWAYS_VISIBLE.includes(col.key))
)

const visibleColumns = computed(() =>
  allColumns.value.filter(col =>
    ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key)
  )
)

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
    if (sortState.sort_by === key) handleSort('created_at', 'desc')
  }
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
    localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

// ---- 错误请求 tab 列设置(与用量明细同机制,独立存储) ----
const ERROR_ALWAYS_VISIBLE = errorRecordAlwaysVisible
const ERROR_DEFAULT_HIDDEN_COLUMNS = errorRecordColumnOrder.filter(key => !errorRecordDefaultVisible.includes(key))
const ERROR_HIDDEN_COLUMNS_KEY = 'usage-error-hidden-columns'

// key 集合须与 OpsErrorLogTable 内部 allColumns 一致
const errorColumns = computed(() => [
  { key: 'user', label: t('admin.ops.errorLog.user') },
  { key: 'api_key', label: t('admin.ops.errorLog.apiKey') },
  { key: 'account', label: t('admin.ops.errorLog.account') },
  { key: 'platform', label: t('admin.ops.errorLog.platform') },
  { key: 'model', label: t('admin.ops.errorLog.model') },
  { key: 'endpoint', label: t('admin.ops.errorLog.endpoint') },
  { key: 'group', label: t('admin.ops.errorLog.group') },
  { key: 'type', label: t('admin.ops.errorLog.type') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('admin.ops.errorLog.status') },
  { key: 'message', label: t('admin.ops.errorLog.message') },
  { key: 'created_at', label: t('admin.ops.errorLog.time') },
  { key: 'user_agent', label: t('usage.userAgent') },
  { key: 'client_ip', label: t('admin.ops.errorLog.ip') },
  { key: 'actions', label: t('admin.ops.errorLog.action') },
])

const errorHiddenColumns = reactive(new Set(readErrorHiddenColumns()))

function readErrorHiddenColumns(): string[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(ERROR_HIDDEN_COLUMNS_KEY) ?? 'null')
    if (Array.isArray(value) && value.every(item => typeof item === 'string')) return value
  } catch {
    // A malformed saved preference falls back to the defaults.
  }
  return ERROR_DEFAULT_HIDDEN_COLUMNS
}

const errorToggleableColumns = computed(() =>
  errorColumns.value.filter(col => !ERROR_ALWAYS_VISIBLE.includes(col.key))
)

const errorVisibleColumnKeys = computed(() =>
  errorColumns.value
    .filter(col => ERROR_ALWAYS_VISIBLE.includes(col.key) || !errorHiddenColumns.has(col.key))
    .map(col => col.key)
)

const toggleErrorColumn = (key: string) => {
  if (errorHiddenColumns.has(key)) {
    errorHiddenColumns.delete(key)
  } else {
    errorHiddenColumns.add(key)
    if (key === (errorSortBy.value === 'status_code' ? 'status' : errorSortBy.value)) sortErrors('created_at', 'desc')
  }
  try {
    localStorage.setItem(ERROR_HIDDEN_COLUMNS_KEY, JSON.stringify([...errorHiddenColumns]))
  } catch (e) {
    console.error('Failed to save error columns:', e)
  }
}

const currentToggleableColumns = computed(() =>
  activeTab.value === 'errors' ? errorToggleableColumns.value : toggleableColumns.value
)
const isCurrentColumnVisible = (key: string) =>
  activeTab.value === 'errors' ? !errorHiddenColumns.has(key) : isColumnVisible(key)
const toggleCurrentColumn = (key: string) =>
  activeTab.value === 'errors' ? toggleErrorColumn(key) : toggleColumn(key)

const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      (JSON.parse(saved) as string[]).forEach((key) => {
        hiddenColumns.add(key)
      })
      const savedVersion = localStorage.getItem(HIDDEN_COLUMNS_VERSION_KEY)
      if (savedVersion !== HIDDEN_COLUMNS_CURRENT_VERSION) {
        if (savedVersion !== HIDDEN_COLUMNS_PREV_VERSION) {
          hiddenColumns.add('request_id')
        }
        hiddenColumns.add('upstream_request_id')
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
        localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        hiddenColumns.add(key)
      })
      localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
    }
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
      hiddenColumns.add(key)
    })
  }
}

// Detail tabs
type DetailTab = 'usage' | 'errors' | 'ranking'
const activeTab = ref<DetailTab>('usage')
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  { key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const },
  { key: 'ranking' as const, label: t('usage.tabs.ranking'), icon: 'chart' as const },
])
const usageFiltersRef = ref<InstanceType<typeof UsageFilters> | null>(null)
const rankingMounted = ref(false)
const rankingRef = ref<InstanceType<typeof UserTokenRanking> | null>(null)

const restoredStringFilters = ['model', 'request_type', 'billing_mode', 'error_phase', 'error_category'] as const
const restoredNumberFilters = ['api_key_id', 'account_id', 'group_id', 'billing_type', 'status_code'] as const
const restoredBooleanFilters = ['native_compaction_v2', 'upstream_model_mismatch'] as const
const errorsMounted = ref(false)
const errorRows = ref<OpsErrorLog[]>([])
const errorItems = ref<OpsErrorRecord[]>([])
const errorMeta = computed(() => errorRecordMeta(errorItems.value))
const errorTotalRecords = ref(0)
const errorDisplay = ref(readRecordPreference('usage-error-', 'display', 'grouped' as 'grouped' | 'raw', (v): v is 'grouped' | 'raw' => v === 'grouped' || v === 'raw'))
const activeErrorParams = ref<OpsErrorRecordsParams | null>(null)
const errorMembers = useErrorRecordMembers(activeErrorParams, t)
const errorDetailType = ref<'request' | 'upstream'>('request')
let errorDetailFromGroup = false
const toggleErrorDisplay = () => { errorDisplay.value = errorDisplay.value === 'grouped' ? 'raw' : 'grouped'; saveRecordPreference('usage-error-', 'display', errorDisplay.value); errorPage.value = 1; void loadErrorRequests() }

const errorTotal = ref(0)
const errorPage = ref(1)
const errorPageSize = ref(readRecordPreference('usage-error-', 'page-size', 20, (v): v is number => typeof v === 'number' && [20,...getConfiguredTablePageSizeOptions().filter(n => n <= 100)].includes(v)))
const errorPageSizeOptions = [
  ...new Set([20, ...getConfiguredTablePageSizeOptions().filter(size => size <= 100)])
].sort((a, b) => a - b)
const errorSortBy = ref(readUsageSort('errors').sort_by)
const errorSortOrder = ref<'asc' | 'desc'>(readUsageSort('errors').sort_order)
const errorListError = ref('')
const errorLoading = ref(false)
const showErrorDetail = ref(false)
const errorDetailId = ref<number | null>(null)
let errorController: AbortController | undefined

const switchTab = (tab: DetailTab) => {
  if (tab === activeTab.value) return
  if (activeTab.value === 'errors') {
    errorController?.abort()
    errorMembers.closeGroup(); showErrorDetail.value = false
  }
  activeTab.value = tab
  if (tab === 'errors') { errorsMounted.value = true; void loadErrorRequests() }
  if (tab === 'ranking') rankingMounted.value = true
}

async function loadErrorRequests() {
  errorController?.abort()
  errorMembers.closeGroup(); showErrorDetail.value = false; activeErrorParams.value = null
  errorRows.value = []; errorItems.value = []
  errorTotal.value = 0; errorTotalRecords.value = 0
  errorListError.value = ''

  if (invalidRange.value) { errorLoading.value = false; errorListError.value = t('admin.usage.recentRange.invalid'); return }
  const start = new Date(startDate.value + 'T00:00:00')
  const end = new Date(endDate.value + 'T00:00:00')
  if (!preciseWindow.value && (!startDate.value || !endDate.value ||
      formatDateLocalInput(start) !== startDate.value ||
      formatDateLocalInput(end) !== endDate.value || start > end)) {
    errorLoading.value = false
    errorListError.value = t('admin.ops.records.invalid')
    return
  }
  // Set the next day's wall-clock midnight even if this day's midnight was skipped.
  end.setHours(24, 0, 0, 0)

  const controller = new AbortController()
  errorController = controller
  errorLoading.value = true
  const { user_id, api_key_id, account_id, group_id, model, error_phase, error_category, status_code } = filters.value
  try {
    const query: OpsErrorRecordsParams = {
      scope: 'request', view: 'all', finance: 'include', display: errorDisplay.value,
      ...(preciseWindow.value ?? { start_time: start.toISOString(), end_time: end.toISOString() }),
      user_id: user_id ?? undefined,
      api_key_id: api_key_id ?? undefined,
      account_id: account_id ?? undefined,
      group_id: group_id ?? undefined,
      model: model || undefined,
      phase: error_phase || undefined,
      category: error_category || undefined,
      status_codes: status_code == null ? undefined : String(status_code),
      page: errorPage.value,
      page_size: errorPageSize.value,
      sort_by: errorSortBy.value,
      sort_order: errorSortOrder.value,
    }
    const response = await opsAPI.listErrorRecords(query, { signal: controller.signal })
    if (controller.signal.aborted) return
    errorItems.value = response.items; errorRows.value = errorRecordRows(response.items)
    errorTotal.value = response.total; errorTotalRecords.value = response.total_records ?? response.total
    activeErrorParams.value = { ...query, start_time: response.window_start || query.start_time, end_time: response.window_end || query.end_time, time_range: undefined }
  } catch (error) {
    if (controller.signal.aborted) return
    const failure = error as { code?: string; reason?: string } | null
    if (failure?.code === 'OPS_DISABLED') {
      errorListError.value = t('admin.ops.records.disabled')
    } else if (failure?.reason === 'OPS_ERROR_RECORDS_QUERY_TIMEOUT') {
      errorListError.value = t('usage.errors.queryTimeout')
    } else {
      errorListError.value = t('usage.errors.retryLoad')
    }
  } finally {
    if (!controller.signal.aborted) errorLoading.value = false
  }
}

function changeErrorPage(page: number) {
  errorPage.value = page
  void loadErrorRequests()
}

function changeErrorPageSize(size: number) {
  errorPageSize.value = size
  saveRecordPreference('usage-error-', 'page-size', size)
  changeErrorPage(1)
}

function sortErrors(key: string, order: 'asc' | 'desc') {
  errorSortBy.value = key
  errorSortOrder.value = order
  saveRecordPreference('admin-usage-', 'errors-sort', {sort_by: key, sort_order: order})
  changeErrorPage(1)
}

function openErrorDetail(id: number, fromGroup = false) {
  const record = (fromGroup ? errorRecordMeta(errorMembers.memberItems.value) : errorMeta.value)[id]
  if (!fromGroup && record?.representative && record.count > 1) { errorMembers.openGroup(record); return }
  errorDetailFromGroup = fromGroup
  if (fromGroup) errorMembers.showGroup.value = false
  errorDetailId.value = id
  errorDetailType.value = record?.is_request_error === false ? 'upstream' : 'request'
  showErrorDetail.value = true
}
function backFromErrorDetail() {
  showErrorDetail.value = false
  if (errorDetailFromGroup && errorMembers.group.value) errorMembers.showGroup.value = true
}

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}

onMounted(() => {
  applyRouteQueryFilters()
  void loadRouteUserFilterLabel()
  loadSavedColumns()
  if (!errorVisibleColumnKeys.value.includes(errorSortBy.value === 'status_code' ? 'status' : errorSortBy.value)) { errorSortBy.value = 'created_at'; errorSortOrder.value = 'desc'; saveRecordPreference('admin-usage-', 'errors-sort', {sort_by: 'created_at',sort_order: 'desc'}) }
  if (!visibleColumns.value.some(col => col.key === sortState.sort_by)) { sortState.sort_by = 'created_at'; sortState.sort_order = 'desc'; saveRecordPreference('admin-usage-', 'usage-sort', {...sortState}) }
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  window.setTimeout(() => {
    void loadChartData()
  }, 120)
  document.addEventListener('click', handleColumnClickOutside)
})
onUnmounted(() => { if (focusMode.value) appStore.setSidebarCollapsed(previousSidebar); cacheHitRateReqSeq++; abortController?.abort(); exportAbortController?.abort(); errorController?.abort(); document.removeEventListener('click', handleColumnClickOutside) })

watch(modelDistributionSource, (source) => {
  void loadModelStats(source)
})

defineExpose({ requestedModelStats, refreshData })
</script>
