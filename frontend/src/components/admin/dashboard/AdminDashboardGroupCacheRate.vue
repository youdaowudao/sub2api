<template>
  <div class="card min-w-0 p-4">
    <h3 class="mb-1 text-sm font-semibold text-gray-900 dark:text-white">
      {{ t('admin.dashboard.groupCacheHitRate') }}
    </h3>
    <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.groupCacheHitRateHint') }}
    </p>
    <div v-if="loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <ul v-else-if="topGroups.length > 0" class="space-y-3">
      <li v-for="group in topGroups" :key="group.group_id" class="space-y-1">
        <div class="flex items-center justify-between gap-2 text-xs">
          <span
            class="min-w-0 flex-1 truncate font-medium text-gray-900 dark:text-white"
            :title="group.group_name || t('admin.dashboard.noGroup')"
          >
            {{ group.group_name || t('admin.dashboard.noGroup') }}
          </span>
          <span class="shrink-0 text-gray-500 dark:text-gray-400">
            {{ t('admin.dashboard.requests') }} {{ group.requests.toLocaleString() }}
          </span>
          <span
            class="shrink-0 tabular-nums"
            :class="group.cache_hit_rate == null ? 'text-gray-500 dark:text-gray-400' : 'text-blue-600 dark:text-blue-400'"
          >
            {{ group.cache_hit_rate == null ? '—' : `${group.cache_hit_rate.toFixed(1)}%` }}
          </span>
        </div>
        <div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700" aria-hidden="true">
          <div
            v-if="group.cache_hit_rate != null"
            class="h-full rounded-full bg-blue-500"
            :style="{ width: `${Math.max(0, Math.min(100, group.cache_hit_rate))}%` }"
          ></div>
        </div>
      </li>
    </ul>
    <div
      v-else
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { GroupStat } from '@/types'

const { t } = useI18n()

const props = defineProps<{
  groups: GroupStat[]
  loading: boolean
}>()

const topGroups = computed(() => [...props.groups].sort((a, b) => b.requests - a.requests).slice(0, 5))
</script>
