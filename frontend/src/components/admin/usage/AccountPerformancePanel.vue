<template>
  <div class="space-y-2">
    <div class="flex items-center gap-2">
      <button class="btn btn-secondary" type="button" :aria-expanded="expanded" @click="expanded = !expanded">{{ t('admin.usage.performance.title') }} {{ expanded ? '−' : '+' }}</button>
      <button v-if="expanded && range" class="btn btn-secondary" type="button" :disabled="loading" @click="load(true)">{{ t('common.refresh') }}</button>
    </div>
    <template v-if="expanded">
      <AccountPerformanceTable v-if="range" :result="result" :loading="loading" :error="error" :filters="range" />
      <p v-else class="text-sm text-gray-500">{{ t('admin.usage.performance.unsupportedRange') }}</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import AccountPerformanceTable from '@/components/admin/usage/AccountPerformanceTable.vue'
import type { CacheHitRateResult } from '@/types'
import type { AdminUsageQueryParams } from '@/api/admin/usage'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ range: (AdminUsageQueryParams & { platform?: string }) | null; currentRange?: (AdminUsageQueryParams & { platform?: string }) | null; refreshToken?: number }>()
const { t } = useI18n()
const expanded = ref(false), loading = ref(false), error = ref<string|null>(null), result = ref<CacheHitRateResult|null>(null)
let seq = 0
const key = computed(() => JSON.stringify(props.range))
async function load(force = false, opening = false) {
  const current = ++seq
  if (!expanded.value || !props.range) { result.value = null; loading.value = false; return }
  const range = { ...(force || opening ? props.currentRange ?? props.range : props.range) }
  loading.value = true; error.value = null; result.value = null
  try {
    const data = await adminAPI.usage.getCacheHitRate({ ...range, dimension:'account', include_performance: 1, ...(force ? { nocache:1 } : {}) })
    if (seq === current) result.value = data
  } catch (err) {
    if (seq === current) error.value = extractApiErrorMessage(err)
  } finally { if (seq === current) loading.value = false }
}
watch([expanded, key, () => props.refreshToken], (next, previous) => {
  void load(next[2] !== previous[2], next[0] && !previous[0])
})
onUnmounted(() => { seq++ })
</script>
