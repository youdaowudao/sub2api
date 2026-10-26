<template>
  <span v-if="loading" class="text-gray-400">…</span>
  <span v-else-if="error" class="text-gray-400" :title="t('usage.cacheRateLoadFailed', { reason: error })">-</span>
  <div v-else-if="item" class="tabular-nums">
    <span :class="item.cache_hit_rate == null ? 'text-gray-400' : 'text-blue-600 dark:text-blue-400'" :title="t('usage.cacheRateFormula')">{{ formatCacheHitRate(item.cache_hit_rate) }}</span>
    <div class="text-xs text-gray-400">{{ item.requests.toLocaleString() }}</div>
  </div>
  <span v-else class="text-gray-400">-</span>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CacheHitRateItem } from '@/types'
import { formatCacheHitRate } from '@/utils/format'

defineProps<{ loading: boolean; error: string | null; item?: CacheHitRateItem }>()
const { t } = useI18n()
</script>
