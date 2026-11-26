<template>
  <AppLayout>
    <template #header-actions>
      <button v-if="checkin.enabled" data-testid="checkin-header" class="daily-top-sign" :class="{ 'is-done': checkin.details?.status === 'done' }" :disabled="headerDisabled" @click="checkinCard?.fromHeader()">
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="3.5" y="5" width="17" height="16" rx="3"/><path d="M7.5 3v4M16.5 3v4M3.5 10h17"/><path v-if="checkin.details?.status==='done'" d="m8 15 2.7 2.7 5.3-5.1"/><path v-else d="M12 12.8v5M9.5 15.3h5"/>
        </svg>
        <span>{{ checkin.details?.status === 'done' ? t('checkin.signed') : t('checkin.headerSign') }}</span>
      </button>
    </template>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardQuickStart :total-api-keys="stats.total_api_keys || 0" :total-requests="stats.total_requests || 0" :balance="user?.balance || 0" />
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" :move-platform="checkin.showLayout" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :error="chartsError" :trend="trendData" :models="modelStats" @dateRangeChange="applyDateRange" @granularityChange="loadCharts" @refresh="refreshAll" :checkin-enabled="checkin.showLayout">
          <template #checkin><UserDashboardCheckin ref="checkinCard" :checkin="checkin" /></template>
        </UserDashboardCharts>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" :error="recentError" :start-date="startDate" :end-date="endDate" /></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
        </div>
        <div id="dashboard-checkin-platform" :hidden="!checkin.showLayout" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted, onUnmounted } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { useI18n } from 'vue-i18n'
import { useDashboardCheckin } from '@/composables/useDashboardCheckin'
import UserDashboardCheckin from '@/components/user/dashboard/UserDashboardCheckin.vue'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'; import UserDashboardQuickStart from '@/components/user/dashboard/UserDashboardQuickStart.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { getTodayRangeDates, getGranularityForDateRange } from '@/utils/dateRange'

const { t } = useI18n()
const checkin = reactive(useDashboardCheckin())
const checkinCard = ref<InstanceType<typeof UserDashboardCheckin> | null>(null)
const headerDisabled = computed(() => !checkinCard.value || checkin.loading || checkin.submitting || checkinCard.value.actionBusy || !['available','bind_email'].includes(checkin.details?.status || ''))
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
