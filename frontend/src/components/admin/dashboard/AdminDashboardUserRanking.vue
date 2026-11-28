<template>
  <div class="card min-w-0 p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('admin.dashboard.userUsageRanking') }}
      </h3>
      <span v-if="!loading && !error" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.dashboard.totalUsersInRange', { count: totalUsers }) }}
      </span>
    </div>
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
            <th v-if="showCompare" class="pb-2 pl-3 text-right">{{ t('admin.dashboard.vsYesterday') }}<span v-if="compare?.window" class="block text-xs font-normal">{{ compare.window }}</span></th>
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

// Preserve the previous ranking's remainder calculation, but only when there are other users.
const otherRankingItem = computed<UserSpendingRankingItem | null>(() => {
  if (!props.items.length || props.totalUsers <= props.items.length) return null

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
  return props.totalActualCost === 0 ? '—' : `${(value / props.totalActualCost * 100).toFixed(1)}%`
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
