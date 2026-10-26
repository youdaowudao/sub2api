<template>
  <div class="card p-4">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('usage.cacheRateAccounts') }}</h3>
    <div v-if="loading" class="flex h-48 items-center justify-center"><LoadingSpinner /></div>
    <div v-else-if="error" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('usage.cacheRateAccountsFailed', { reason: error }) }}</div>
    <div v-else-if="!items.length" class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('usage.cacheRateNoAccounts') }}</div>
    <div v-else class="max-h-80 overflow-auto">
      <table class="w-full text-xs">
        <thead>
          <tr class="text-gray-500 dark:text-gray-400">
            <th class="pb-2 text-left">{{ t('usage.cacheRateAccount') }}</th>
            <th class="pb-2 text-right">{{ t('usage.cacheRateRequests') }}</th>
            <th class="pb-2 text-right">{{ t('usage.cacheRate') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id" class="border-t border-gray-100 dark:border-dark-700">
            <td class="max-w-64 truncate py-1.5 font-medium text-gray-900 dark:text-white" :title="`#${item.id}`">{{ item.name || `#${item.id}` }}</td>
            <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ item.requests.toLocaleString() }}</td>
            <td class="py-1.5 text-right tabular-nums" :class="item.cache_hit_rate == null ? 'text-gray-400' : 'text-blue-600 dark:text-blue-400'" :title="t('usage.cacheRateFormula')">{{ formatCacheHitRate(item.cache_hit_rate) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CacheHitRateItem } from '@/types'
import { formatCacheHitRate } from '@/utils/format'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

defineProps<{ loading: boolean; error: string | null; items: CacheHitRateItem[] }>()
const { t } = useI18n()
</script>
