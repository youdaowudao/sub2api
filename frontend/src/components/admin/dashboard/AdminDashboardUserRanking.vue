<template>
  <div class="card min-w-0 p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('admin.dashboard.userUsageRanking') }}
      </h3>
      <span v-if="!loading && !error" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.dashboard.totalUsersInRange', { count: isMetricValue(totalUsers) ? totalUsers : '—' }) }}
      </span>
    </div>
    <section v-if="showCompare && !loading && !error" data-testid="site-totals" class="mb-4 rounded-lg bg-gray-50 p-3 dark:bg-dark-700/40">
      <div class="mb-3 flex flex-wrap items-baseline justify-between gap-2">
        <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.dashboard.siteTotals') }}</h4>
        <span v-if="currentWindow" class="text-xs text-gray-500 dark:text-gray-400">{{ currentWindow }}</span>
      </div>
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div v-for="metric in siteMetrics" :key="metric.key" :data-testid="`site-total-${metric.key}`" class="min-w-0">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
          <p class="break-words text-sm font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
          <p class="text-xs" :class="comparisonClass(metric.comparison)">{{ metric.comparison === 'new' ? t('admin.dashboard.yesterdayZero') : formatComparison(metric.comparison) }}</p>
        </div>
      </div>
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {{ comparisonHeading }}<template v-if="previousWindow || compare?.window"> · {{ previousWindow || compare?.window }}</template>
      </p>
    </section>
    <div v-if="loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div
      v-else-if="error"
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.failedToLoad') }}
    </div>
    <div v-else-if="displayItems.length > 0" class="overflow-x-auto">
      <table class="w-full whitespace-nowrap text-xs">
        <thead>
          <tr class="text-gray-500 dark:text-gray-400">
            <th class="pb-2 pr-3 text-left">#</th>
            <th class="pb-2 text-left">{{ t('admin.dashboard.spendingRankingUser') }}</th>
            <th class="pb-2 pl-3 text-right">{{ t('admin.dashboard.spendingRankingRequests') }}</th>
            <th class="pb-2 pl-3 text-right">{{ t('admin.dashboard.spendingRankingTokens') }}</th>
            <th class="pb-2 pl-3 text-right">{{ t('admin.dashboard.spendingRankingSpend') }}</th>
            <th class="pb-2 pl-3 text-right">{{ t('admin.dashboard.share') }}</th>
            <th v-if="showCompare" class="pb-2 pl-3 text-right">{{ comparisonHeading }}<span v-if="previousWindow || compare?.window" class="block text-xs font-normal">{{ previousWindow || compare?.window }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, index) in displayItems"
            :key="row.isOther ? 'others' : row.item.user_id"
            class="border-t border-gray-100 transition-colors dark:border-dark-700"
            :class="row.isOther
              ? 'bg-gray-50/70 dark:bg-dark-700/20'
              : 'cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-700/40'"
            @click="row.isOther ? undefined : emit('row-click', row.item)"
          >
            <td class="py-1.5 pr-3 font-semibold text-gray-500 dark:text-gray-400">
              {{ row.isOther ? 'Σ' : index + 1 }}
            </td>
            <td class="py-1.5">
              <span
                class="block max-w-[140px] truncate font-medium text-gray-900 dark:text-white"
                :title="getRankingRowLabel(row)"
              >
                {{ getRankingRowLabel(row) }}
              </span>
            </td>
            <td class="py-1.5 pl-3 text-right text-gray-600 dark:text-gray-400">
              {{ toFiniteNumber(row.item.requests).toLocaleString() }}
            </td>
            <td class="py-1.5 pl-3 text-right text-gray-600 dark:text-gray-400">
              {{ formatTokens(row.item.tokens) }}
            </td>
            <td class="py-1.5 pl-3 text-right text-green-600 dark:text-green-400">
              ¥{{ formatCost(row.item.actual_cost) }}
            </td>
            <td class="py-1.5 pl-3 text-right text-gray-600 dark:text-gray-400">
              {{ formatShare(row.item.actual_cost) }}
            </td>
            <td v-if="showCompare" class="py-1.5 pl-3 text-right" :class="comparisonClass(row.comparison)">
              {{ formatComparison(row.comparison) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div
      v-else
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script lang="ts">
import type { UserSpendingRankingItem } from '@/types'

interface RankingComparison {
  items: UserSpendingRankingItem[]
  totalUsers: number
  totalRequests?: number
  totalTokens?: number
  totalActualCost?: number
  window?: string
}

type ComparisonValue = number | 'new' | null

export function getSpendingComparison(
  item: UserSpendingRankingItem,
  compare: RankingComparison | null
): ComparisonValue {
  if (!compare) return null
  const previous = compare.items.find((entry) => entry.user_id === item.user_id)
  if (!previous) return compare.totalUsers <= compare.items.length ? 'new' : null
  if (previous.actual_cost === 0) return 'new'
  if (previous.actual_cost > 0) {
    return (item.actual_cost - previous.actual_cost) / previous.actual_cost * 100
  }
  return null
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const { t } = useI18n()

const props = defineProps<{
  items: UserSpendingRankingItem[]
  totalActualCost: number
  totalRequests: number
  totalTokens: number
  totalUsers: number
  compare: RankingComparison | null
  showCompare: boolean
  currentWindow?: string
  previousWindow?: string
  differentDuration?: boolean
  loading: boolean
  error: boolean
}>()

const emit = defineEmits<{
  'row-click': [item: UserSpendingRankingItem]
}>()

interface DisplayRow {
  item: UserSpendingRankingItem
  isOther: boolean
  comparison: ComparisonValue
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}
const isMetricValue = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0
const totalComparison = (current: unknown, previous: unknown): ComparisonValue => {
  if (!isMetricValue(current) || !isMetricValue(previous)) return null
  if (previous === 0) return current === 0 ? 0 : 'new'
  const change = (current - previous) / previous * 100
  return Number.isFinite(change) ? change : null
}
const comparisonHeading = computed(() => t(props.differentDuration ? 'admin.dashboard.vsYesterdayDifferentDuration' : 'admin.dashboard.vsYesterday'))
const siteMetrics = computed(() => [
  { key: 'users', label: t('admin.dashboard.siteActiveUsers'), current: props.totalUsers, previous: props.compare?.totalUsers, format: (n: number) => n.toLocaleString() },
  { key: 'requests', label: t('admin.dashboard.spendingRankingRequests'), current: props.totalRequests, previous: props.compare?.totalRequests, format: (n: number) => n.toLocaleString() },
  { key: 'tokens', label: t('admin.dashboard.spendingRankingTokens'), current: props.totalTokens, previous: props.compare?.totalTokens, format: formatTokens },
  { key: 'cost', label: t('admin.dashboard.spendingRankingSpend'), current: props.totalActualCost, previous: props.compare?.totalActualCost, format: (n: number) => `¥${formatCost(n)}` },
].map(metric => ({
  key: metric.key, label: metric.label,
  value: isMetricValue(metric.current) ? metric.format(metric.current) : '—',
  comparison: totalComparison(metric.current, metric.previous),
})))

// Preserve the previous ranking's remainder calculation, but only when there are other users.
const otherRankingItem = computed<UserSpendingRankingItem | null>(() => {
  if (!props.items.length || !isMetricValue(props.totalUsers) || props.totalUsers <= props.items.length ||
      !isMetricValue(props.totalActualCost) || !isMetricValue(props.totalRequests) || !isMetricValue(props.totalTokens)) return null

  const rankedActualCost = props.items.reduce((sum, item) => sum + toFiniteNumber(item.actual_cost), 0)
  const rankedRequests = props.items.reduce((sum, item) => sum + toFiniteNumber(item.requests), 0)
  const rankedTokens = props.items.reduce((sum, item) => sum + toFiniteNumber(item.tokens), 0)
  const otherActualCost = Math.max((props.totalActualCost || 0) - rankedActualCost, 0)
  const otherRequests = Math.max((props.totalRequests || 0) - rankedRequests, 0)
  const otherTokens = Math.max((props.totalTokens || 0) - rankedTokens, 0)

  if (otherActualCost <= 0.000001 && otherRequests <= 0 && otherTokens <= 0) return null

  return {
    user_id: 0,
    email: '',
    username: '',
    actual_cost: otherActualCost,
    requests: otherRequests,
    tokens: otherTokens,
  }
})

const displayItems = computed<DisplayRow[]>(() => {
  const rows: DisplayRow[] = props.items.map((item) => ({
    item,
    isOther: false,
    comparison: props.showCompare ? getSpendingComparison(item, props.compare) : null,
  }))
  if (otherRankingItem.value) {
    rows.push({ item: otherRankingItem.value, isOther: true, comparison: null })
  }
  return rows
})

const getRankingRowLabel = (row: DisplayRow): string => {
  if (row.isOther) return t('admin.dashboard.otherUsers')
  if (row.item.username?.trim()) return row.item.username.trim()
  if (row.item.email?.trim()) return row.item.email.trim()
  return t('admin.redeem.userPrefix', { id: row.item.user_id })
}

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) return (safeValue / 1000).toFixed(2) + 'K'
  if (safeValue >= 1) return safeValue.toFixed(2)
  if (safeValue >= 0.01) return safeValue.toFixed(3)
  return safeValue.toFixed(4)
}

const formatShare = (value: number): string => {
  return !isMetricValue(props.totalActualCost) || props.totalActualCost === 0 ? '—' : `${(value / props.totalActualCost * 100).toFixed(1)}%`
}

const formatComparison = (value: ComparisonValue): string => {
  if (value === null) return '—'
  if (value === 'new') return t('admin.dashboard.newUser')
  if (value === 0) return '0%'
  return `${value > 0 ? '↑' : '↓'} ${Math.abs(value).toFixed(1)}%`
}

const comparisonClass = (value: ComparisonValue): string => {
  if (typeof value === 'number') {
    if (value > 0) return 'text-green-600 dark:text-green-400'
    if (value < 0) return 'text-red-600 dark:text-red-400'
  }
  return 'text-gray-500 dark:text-gray-400'
}
</script>
