<template>
  <template v-for="section in sections" :key="section.category">
    <optgroup v-if="native" :label="t('keys.providers.' + section.category)">
      <template v-for="group in section.groups" :key="group.id">
        <slot :group="group" />
      </template>
    </optgroup>
    <template v-else>
    <div role="presentation" class="col-span-full w-full basis-full border-b border-gray-100 px-2 py-1 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">
      {{ t('keys.providers.' + section.category) }}
    </div>
    <template v-for="group in section.groups" :key="group.id">
      <slot :group="group" />
    </template>
    </template>
  </template>
</template>
<script setup lang="ts" generic="T extends { id: number; platform: string; display_category?: string; sort_order?: number }">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { groupGroupsByCategory } from '@/utils/groupDisplay'
const props = defineProps<{ groups: T[]; native?: boolean }>()
const { t } = useI18n()
const sections = computed(() => groupGroupsByCategory(props.groups))
</script>
