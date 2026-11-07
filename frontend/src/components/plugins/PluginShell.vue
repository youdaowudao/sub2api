<template>
  <div v-if="shells.length" class="fixed bottom-4 right-4 z-40 flex max-w-[calc(100vw-2rem)] flex-col items-end gap-2">
    <div class="flex max-w-full flex-wrap justify-end gap-2">
      <button
        v-for="entry in shells"
        :key="pluginEntryKey(entry)"
        type="button"
        class="btn btn-secondary btn-sm"
        :aria-pressed="store.selectedShellKey === pluginEntryKey(entry)"
        @click="store.openShell(entry)"
      >{{ t('admin.plugins.openPanel', { name: entry.label }) }}</button>
    </div>
    <section v-if="selected" class="w-[380px] max-w-full overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-900" :aria-label="selected.label">
      <div class="flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-2 dark:border-dark-700">
        <span class="truncate text-sm font-medium">{{ selected.label }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="store.closeShell">{{ t('admin.plugins.closePanel') }}</button>
      </div>
      <PluginFrame
        :key="`${store.identityVersion}:${pluginEntryKey(selected)}:${selected.revision}`"
        :entry="selected"
        :anchors="anchors"
        @close="store.closeShell"
        @unavailable="unavailable"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { pluginEntryKey } from '@/api/plugins'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import { useAppStore } from '@/stores/app'
import PluginFrame from './PluginFrame.vue'

defineProps<{ anchors: { navigation: HTMLElement | null; content: HTMLElement | null } }>()
const { t } = useI18n()
const store = usePluginEntriesStore()
const app = useAppStore()
const shells = computed(() => store.forMount('shell'))
const selected = computed(() => shells.value.find(entry => pluginEntryKey(entry) === store.selectedShellKey))

watch(shells, (current, previous) => {
  if (previous.some(entry => pluginEntryKey(entry) === store.selectedShellKey) && !current.some(entry => pluginEntryKey(entry) === store.selectedShellKey)) {
    app.showInfo(t(store.error ? 'admin.plugins.pageLoadFailed' : 'admin.plugins.pageDisabled'))
  }
}, { flush: 'sync' })

function unavailable(reason: 'disabled' | 'forbidden'): void {
  app.showInfo(t(reason === 'disabled' ? 'admin.plugins.pageDisabled' : 'admin.plugins.pageForbidden'))
  store.closeShell()
  void store.refresh()
}
</script>
