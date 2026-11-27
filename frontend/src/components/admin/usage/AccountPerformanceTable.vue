<template>
  <div class="card p-4">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold">{{ t('admin.usage.performance.title') }}</h3>
      <span v-if="rangeLabel" class="text-xs text-gray-500">{{ rangeLabel }}</span>
    </div>
    <div v-if="loading" class="flex h-32 items-center justify-center"><LoadingSpinner /></div>
    <div v-else-if="error" class="py-6 text-center text-sm text-gray-500">{{ t('admin.usage.performance.failed') }} {{ error }}</div>
    <template v-else>
      <p v-if="result?.error_state && result.error_state !== 'available'" class="mb-2 text-xs text-amber-600">{{ t('admin.usage.performance.' + result.error_state) }}</p>
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <input v-model="search" class="input max-w-48" :placeholder="t('admin.usage.searchAccountPlaceholder')" />
        <Select v-model="platform" :options="platforms" class="w-40" />
        <label v-if="errorsAvailable" class="flex items-center gap-2 text-xs"><input v-model="errorsOnly" type="checkbox" />{{ t('admin.usage.performance.errorsOnly') }}</label>
      </div>
      <p v-if="!rows.length" class="py-6 text-center text-sm text-gray-500">{{ t('admin.usage.performance.empty') }}</p>
      <div v-else class="max-h-96 overflow-auto">
        <table class="w-full whitespace-nowrap text-xs">
          <thead class="sticky top-0 bg-white dark:bg-dark-800"><tr class="text-gray-500">
            <th class="py-2 text-left">{{ t('admin.usage.account') }}</th>
            <th v-for="col in columns" :key="col.key" class="px-2 py-2 text-right">
              <button type="button" @click="sort(col.key)">{{ t(col.label) }} {{ sortKey === col.key ? (descending ? '↓' : '↑') : '' }}</button>
            </th>
            <th class="px-2 py-2 text-left">{{ t('admin.accounts.schedulable') }}</th>
          </tr></thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id" class="border-t border-gray-100 dark:border-dark-700">
              <td class="max-w-64 truncate py-2 font-medium" :title="'#' + row.id">
                <button v-if="row.account_exists" class="text-primary-600 hover:underline" type="button" @click="manage(row)">{{ row.name || '#' + row.id }}</button>
                <span v-else>{{ row.name || '#' + row.id }}</span>
              </td>
              <td class="px-2 text-right">{{ row.requests.toLocaleString() }}</td>
              <td class="px-2 text-right"><button v-if="row.upstream_error_count" class="text-rose-600 hover:underline" @click="details(row, false)">{{ row.upstream_error_count }}</button><span v-else>{{ row.upstream_error_count ?? '—' }}</span></td>
              <td class="px-2 text-right"><button v-if="row.rate_limit_count" class="text-amber-600 hover:underline" @click="details(row, true)">{{ row.rate_limit_count }}</button><span v-else>{{ row.rate_limit_count ?? '—' }}</span></td>
              <td class="px-2 text-right text-blue-600">{{ formatCacheHitRate(row.cache_hit_rate) }}</td>
              <td class="px-2 text-right">{{ row.average_duration_ms == null ? '—' : Math.round(row.average_duration_ms).toLocaleString() + 'ms' }}</td>
              <td class="px-2">{{ row.schedulable == null ? '—' : t(row.schedulable ? 'admin.accounts.schedulableEnabled' : 'admin.accounts.schedulableDisabled') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="errorsAvailable" class="mt-2 text-xs text-gray-500">{{ t('admin.usage.performance.errorHint') }}</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { formatCacheHitRate } from '@/utils/format'
import { displayAdminWindow } from '@/utils/adminRecentRange'
import type { CacheHitRateResult, CacheHitRateItem } from '@/types'
import type { AdminUsageQueryParams } from '@/api/admin/usage'
const props = defineProps<{ result: CacheHitRateResult | null; loading: boolean; error: string | null; filters?: AdminUsageQueryParams & { platform?: string } }>()
const { t } = useI18n()
const router = useRouter()
type SortKey = 'requests' | 'upstream_error_count' | 'rate_limit_count' | 'cache_hit_rate' | 'average_duration_ms'
const columns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'admin.usage.performance.requests' },
  { key: 'upstream_error_count', label: 'admin.usage.performance.errors' },
  { key: 'rate_limit_count', label: 'admin.usage.performance.limits' },
  { key: 'cache_hit_rate', label: 'usage.cacheRate' },
  { key: 'average_duration_ms', label: 'admin.usage.performance.duration' }
]
const sortKey = ref<SortKey>('requests'), descending = ref(true)
const search = ref(''), platform = ref<string | null>(null), errorsOnly = ref(false)
const errorsAvailable = computed(() => props.result?.error_state === 'available')
const rangeLabel = computed(() => displayAdminWindow(props.result?.window_start, props.result?.window_end) || (props.result ? props.result.start_date + ' – ' + props.result.end_date : ''))
const platforms = computed(() => [{ value: null, label: t('common.all') }, ...Array.from(new Set((props.result?.items ?? []).map(x => x.platform).filter(Boolean))).sort().map(x => ({ value: x, label: x }))])
const rows = computed(() => (props.result?.items ?? []).filter(x =>
  (!platform.value || x.platform === platform.value) &&
  (!search.value || (x.name + ' #' + x.id).toLowerCase().includes(search.value.trim().toLowerCase())) &&
  (!errorsOnly.value || !errorsAvailable.value || (x.upstream_error_count ?? 0) > 0)
).slice().sort((a,b) => {
  const av = a[sortKey.value], bv = b[sortKey.value]
  if (av == null || bv == null) return av == null && bv == null ? a.id-b.id : av == null ? 1 : -1
  return (descending.value ? bv-av : av-bv) || a.id-b.id
}))
function sort(key: SortKey) { if (sortKey.value === key) descending.value = !descending.value; else { sortKey.value = key; descending.value = true } }
function manage(row: CacheHitRateItem) { void router.push({ path: '/admin/accounts', query: { account_id: String(row.id) } }) }
function details(row: CacheHitRateItem, limit: boolean) {
  if (!props.result?.window_start || !props.result.window_end) return
  const query: Record<string,string> = { tr:'custom', start_time: props.result.window_start, end_time: props.result.window_end, errors_focus:'1', errors_scope:'upstream', errors_view:'all', errors_finance:'include', errors_display:'raw', errors_account_id:String(row.id) }
  for (const [source, target] of [['user_id','errors_user_id'], ['api_key_id','errors_api_key_id'], ['model','errors_model'], ['group_id','group_id'], ['platform','platform']] as const) {
    const value = props.filters?.[source]
    if (value != null && value !== '') query[target] = String(value)
  }
  if (limit) query.errors_status_codes = '429'
  void router.push({ path: '/admin/ops', query })
}
</script>
