<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <div class="grid grid-cols-2 gap-4 lg:grid-cols-5" data-testid="dashboard-overview">
          <div class="card p-4">
            <div class="flex items-center gap-3">
              <div class="rounded-lg bg-green-100 p-2 dark:bg-green-900/30">
                <Icon name="chart" size="md" class="text-green-600 dark:text-green-400" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.todayRequestsCard') }}</p>
                <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatNumber(stats.today_requests) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('common.total') }}: {{ formatNumber(stats.total_requests) }}</p>
              </div>
            </div>
          </div>

          <div class="card p-4">
            <div class="flex items-center gap-3">
              <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30">
                <Icon name="cube" size="md" class="text-amber-600 dark:text-amber-400" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.todayTokens') }}</p>
                <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatTokens(stats.today_tokens) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('common.total') }}: {{ formatTokens(stats.total_tokens) }}</p>
              </div>
            </div>
          </div>

          <div class="card p-4">
            <div class="flex items-center gap-3">
              <div class="rounded-lg bg-indigo-100 p-2 dark:bg-indigo-900/30">
                <Icon name="database" size="md" class="text-indigo-600 dark:text-indigo-400" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.todayConsumption') }}</p>
                <p class="text-xl font-bold text-green-600 dark:text-green-400" :title="t('admin.dashboard.actual')">${{ formatCost(stats.today_actual_cost) }}</p>
                <p class="text-xs">
                  <span class="text-orange-500 dark:text-orange-400" :title="t('admin.dashboard.accountCost')">${{ formatCost(stats.today_account_cost) }}</span>
                  <span class="text-gray-400 dark:text-gray-500"> / </span>
                  <span class="text-gray-400 dark:text-gray-500" :title="t('admin.dashboard.standard')">${{ formatCost(stats.today_cost) }}</span>
                </p>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('common.total') }}:
                  <span class="text-green-600 dark:text-green-400" :title="totalCostTitle">${{ formatCost(stats.total_actual_cost) }}</span>
                </p>
              </div>
            </div>
          </div>

          <div class="card p-4">
            <div class="flex items-center gap-3">
              <div class="rounded-lg bg-emerald-100 p-2 dark:bg-emerald-900/30">
                <Icon name="userPlus" size="md" class="text-emerald-600 dark:text-emerald-400" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.users') }}</p>
                <p class="text-xl font-bold text-emerald-600 dark:text-emerald-400">+{{ formatNumber(stats.today_new_users) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.activeUsers') }}: {{ formatNumber(stats.active_users) }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('common.total') }}: {{ formatNumber(stats.total_users) }}</p>
              </div>
            </div>
          </div>

          <div class="card p-4">
            <div class="flex items-center gap-3">
              <div class="rounded-lg bg-violet-100 p-2 dark:bg-violet-900/30">
                <Icon name="bolt" size="md" class="text-violet-600 dark:text-violet-400" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.performance') }}</p>
                <div class="flex items-baseline gap-2">
                  <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatTokens(stats.rpm) }}</p>
                  <span class="text-xs text-gray-500 dark:text-gray-400">RPM</span>
                </div>
                <div class="flex items-baseline gap-2">
                  <p class="text-sm font-semibold text-violet-600 dark:text-violet-400">{{ formatTokens(stats.tpm) }}</p>
                  <span class="text-xs text-gray-500 dark:text-gray-400">TPM</span>
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.dashboard.todayAvgResponse') }}: <span data-testid="today-average-response">{{ stats.today_average_duration_ms == null ? '—' : formatDuration(stats.today_average_duration_ms) }}</span>
                </p>
              </div>
            </div>
          </div>
        </div>

        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <button type="button" :class="['btn', rangeMode === 'lastHour' ? 'btn-primary' : 'btn-secondary']" :aria-pressed="rangeMode === 'lastHour'" @click="selectLastHour">
              {{ t('admin.dashboard.lastHour') }}
            </button>
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" @change="onDateRangeChange" />
            </div>
            <div v-if="rangeMode === 'dates'" class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
            <button type="button" class="btn btn-secondary" :disabled="chartsLoading" @click="loadDashboardStats">{{ t('common.refresh') }}</button>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" :show-cache-hit-rate="false" />
          <ModelDistributionChart
            :key="rangeKey"
            :model-stats="modelStats"
            :enable-ranking-view="false"
            :loading="chartsLoading"
            :start-date="rangeMode === 'dates' ? startDate : undefined"
            :end-date="rangeMode === 'dates' ? endDate : undefined"
            :filters="modelFilters"
          />
        </div>

        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <AdminDashboardUserRanking
            class="min-w-0 lg:col-span-2"
            :items="rankingItems"
            :total-actual-cost="rankingTotalActualCost"
            :total-requests="rankingTotalRequests"
            :total-tokens="rankingTotalTokens"
            :total-users="rankingTotalUsers"
            :compare="compare"
            :show-compare="rangeMode === 'lastHour'"
            :loading="rankingLoading"
            :error="rankingError"
            @row-click="goToUserUsage"
          />
          <AdminDashboardGroupCacheRate :groups="groupStats" :loading="chartsLoading" />
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { DashboardStats, GroupStat, ModelStat, TrendDataPoint, UserSpendingRankingItem } from '@/types'
import { getGranularityForDateRange, getTodayRangeDates } from '@/utils/dateRange'
import { formatDateLocalInput } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import AdminDashboardUserRanking from '@/components/admin/dashboard/AdminDashboardUserRanking.vue'
import AdminDashboardGroupCacheRate from '@/components/admin/dashboard/AdminDashboardGroupCacheRate.vue'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const chartsLoading = ref(false)
const rankingLoading = ref(false)
const rankingError = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
const rankingTotalUsers = ref(0)
const compare = ref<{ items: UserSpendingRankingItem[]; totalUsers: number } | null>(null)
let chartLoadSeq = 0
let rankingLoadSeq = 0
let compareLoadSeq = 0

const rangeMode = ref<'lastHour' | 'dates'>('lastHour')
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getTodayRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
type PreciseRange = { start_time: string; end_time: string }
type DateRange = { start_date: string; end_date: string }
type DashboardRange = PreciseRange | DateRange
const preciseWindow = ref<PreciseRange | null>(null)
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])
const modelFilters = computed(() => rangeMode.value === 'lastHour' ? (preciseWindow.value ?? {}) : {})
const rangeKey = computed(() => rangeMode.value === 'lastHour'
  ? `lastHour:${preciseWindow.value?.start_time}:${preciseWindow.value?.end_time}`
  : `dates:${startDate.value}:${endDate.value}`)

const formatTokens = (value: number | undefined): string => {
  if (value == null) return '0'
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}
const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}
const formatNumber = (value: number | null | undefined): string => toFiniteNumber(value).toLocaleString()
const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) return (safeValue / 1000).toFixed(2) + 'K'
  if (safeValue >= 1) return safeValue.toFixed(2)
  if (safeValue >= 0.01) return safeValue.toFixed(3)
  return safeValue.toFixed(4)
}
const formatDuration = (ms: number): string => ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`
const totalCostTitle = computed(() => [
  `${t('admin.dashboard.actual')}: $${formatCost(stats.value?.total_actual_cost)}`,
  `${t('admin.dashboard.accountCost')}: $${formatCost(stats.value?.total_account_cost)}`,
  `${t('admin.dashboard.standard')}: $${formatCost(stats.value?.total_cost)}`
].join(' / '))

const goToUserUsage = (item: UserSpendingRankingItem) => {
  const precise = rangeMode.value === 'lastHour' ? preciseWindow.value : null
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      start_date: precise ? formatDateLocalInput(new Date(precise.start_time)) : startDate.value,
      end_date: precise ? formatDateLocalInput(new Date(new Date(precise.end_time).getTime() - 1)) : endDate.value
    }
  })
}

const localMinuteLabel = (date: string): string => {
  const local = new Date(date)
  return `${String(local.getHours()).padStart(2, '0')}:${String(local.getMinutes()).padStart(2, '0')}`
}

const loadDashboardSnapshot = async (range: DashboardRange, includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) loading.value = true
  chartsLoading.value = true
  try {
    const precise = 'start_time' in range
    const response = await adminAPI.dashboard.getSnapshotV2({
      ...range,
      granularity: precise ? 'minute' : granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: true,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) stats.value = response.stats
    trendData.value = precise
      ? (response.trend || []).map(point => ({ ...point, date: localMinuteLabel(point.date) }))
      : response.trend || []
    modelStats.value = response.models || []
    groupStats.value = response.groups || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    trendData.value = []
    modelStats.value = []
    groupStats.value = []
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async (range: DashboardRange) => {
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  rankingItems.value = []
  rankingTotalActualCost.value = 0
  rankingTotalRequests.value = 0
  rankingTotalTokens.value = 0
  rankingTotalUsers.value = 0
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({ ...range, limit: 12 })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
    rankingTotalUsers.value = response.total_users || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingError.value = true
  } finally {
    if (currentSeq === rankingLoadSeq) rankingLoading.value = false
  }
}

const loadYesterdayRanking = async (range: DashboardRange) => {
  const currentSeq = ++compareLoadSeq
  compare.value = null
  if (!('start_time' in range)) return
  const yesterday = (value: string) => new Date(new Date(value).getTime() - 24 * 60 * 60 * 1000).toISOString()
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      start_time: yesterday(range.start_time),
      end_time: yesterday(range.end_time),
      limit: 50
    })
    if (currentSeq !== compareLoadSeq) return
    compare.value = { items: response.ranking || [], totalUsers: response.total_users }
  } catch (error) {
    if (currentSeq !== compareLoadSeq) return
    console.error('Error loading yesterday ranking:', error)
  }
}

const load = async (includeStats: boolean) => {
  let range: DashboardRange
  if (rangeMode.value === 'lastHour') {
    const end = Math.floor(Date.now() / 60000) * 60000
    const start = end - 60 * 60000
    preciseWindow.value = { start_time: new Date(start).toISOString(), end_time: new Date(end).toISOString() }
    range = preciseWindow.value
  } else {
    range = { start_date: startDate.value, end_date: endDate.value }
  }
  await Promise.all([loadDashboardSnapshot(range, includeStats), loadUserSpendingRanking(range), loadYesterdayRanking(range)])
}
const loadDashboardStats = () => load(true)
const loadChartData = () => load(false)
const selectLastHour = () => {
  rangeMode.value = 'lastHour'
  return loadChartData()
}
const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  rangeMode.value = 'dates'
  granularity.value = getGranularityForDateRange(startDate.value, endDate.value)
  return loadChartData()
}

onMounted(loadDashboardStats)
</script>
