<template>
  <div v-if="history.enabled.value" class="space-y-1 p-1 text-sm">
    <template v-for="section in sections" :key="section.label">
      <div v-if="section.items.length" class="px-2 pt-1 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(section.label) }}</div>
      <div v-for="item in section.items" :key="item.id" class="flex items-center gap-1 rounded hover:bg-gray-100 dark:hover:bg-dark-700">
        <button class="min-w-0 flex-1 truncate px-2 py-2 text-left" type="button" @click="emit('select', item)">{{ item.email }} <span class="text-gray-400">#{{ item.id }}</span></button>
        <button type="button" class="px-2 py-1 text-xs text-gray-500 hover:text-primary-600" :title="t(item.pinned ? 'admin.usage.history.unpin' : 'admin.usage.history.pin')" @click.stop="history.togglePin(item.id)">{{ item.pinned ? '★' : '☆' }}</button>
        <button type="button" class="px-2 py-1 text-gray-500" :aria-label="t('admin.usage.history.remove')" @click.stop="history.remove(item.id)">×</button>
      </div>
    </template>
    <p v-if="history.failed.value" class="px-2 py-1 text-xs text-amber-600">{{ t('admin.usage.history.unavailable') }}</p>
    <div v-if="history.entries.value.length" class="flex justify-end gap-2 border-t px-2 py-1 text-xs">
      <template v-if="confirmClear">
        <button type="button" class="text-gray-500" @click.stop="confirmClear = false">{{ t('common.cancel') }}</button>
        <button type="button" class="text-rose-600" @click.stop="history.clear(); confirmClear = false">{{ t('admin.usage.history.confirmClear') }}</button>
      </template>
      <button v-else type="button" class="text-gray-500" @click.stop="confirmClear = true">{{ t('admin.usage.history.clear') }}</button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAdminUserHistory, type AdminUserHistoryEntry } from '@/composables/useAdminUserHistory'
const { t } = useI18n()
const emit = defineEmits<{ select: [item: AdminUserHistoryEntry] }>()
const history = useAdminUserHistory()
const confirmClear = ref(false)
const sections = computed(() => [
  { label: 'admin.usage.history.pinned', items: history.entries.value.filter(x => x.pinned) },
  { label: 'admin.usage.history.recent', items: history.entries.value.filter(x => !x.pinned) }
])
</script>
