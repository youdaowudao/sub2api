<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :error="chartsError" :trend="trendData" :models="modelStats" @dateRangeChange="applyDateRange" @granularityChange="loadCharts" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" :error="recentError" :start-date="startDate" :end-date="endDate" /></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { getTodayRangeDates, getGranularityForDateRange } from '@/utils/dateRange'

const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const today = getTodayRangeDates()
const startDate = ref(today.start)
const endDate = ref(today.end)
const granularity = ref<'hour' | 'day'>('hour')
const chartsError = ref(false)
const recentError = ref(false)
let chartsSeq = 0
let recentSeq = 0

const loadStats = async () => { loading.value = true; try { await authStore.refreshUser(); stats.value = await usageAPI.getDashboardStats() } catch (error) { console.error('Failed to load dashboard stats:', error) } finally { loading.value = false } }
const loadCharts = async () => {
  const seq = ++chartsSeq
  const start = startDate.value, end = endDate.value, grain = granularity.value
  const current = () => seq === chartsSeq && start === startDate.value && end === endDate.value && grain === granularity.value
  loadingCharts.value = true
  chartsError.value = false
  trendData.value = []
  modelStats.value = []
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ start_date: start, end_date: end, granularity: grain }),
      usageAPI.getDashboardModels({ start_date: start, end_date: end }),
    ])
    if (!current()) return
    trendData.value = trend.trend || []
    modelStats.value = models.models || []
  } catch {
    if (current()) chartsError.value = true
  } finally {
    if (current()) loadingCharts.value = false
  }
}
const loadRecent = async () => {
  const seq = ++recentSeq
  const start = startDate.value, end = endDate.value
  const current = () => seq === recentSeq && start === startDate.value && end === endDate.value
  loadingUsage.value = true
  recentError.value = false
  recentUsage.value = []
  try {
    const res = await usageAPI.getByDateRange(start, end)
    if (current()) recentUsage.value = res.items.slice(0, 5)
  } catch {
    if (current()) recentError.value = true
  } finally {
    if (current()) loadingUsage.value = false
  }
}
const loadRangeData = () => { void loadCharts(); void loadRecent() }
const applyDateRange = () => {
  granularity.value = getGranularityForDateRange(startDate.value, endDate.value)
  loadRangeData()
}
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
const refreshAll = () => { loadStats(); loadRangeData(); loadPlatformQuotas() }

onMounted(() => { refreshAll() })
onUnmounted(() => { ++chartsSeq; ++recentSeq })
</script>
