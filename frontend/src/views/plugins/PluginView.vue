<template>
  <AppLayout>
    <section class="card overflow-hidden">
      <div class="border-b border-gray-200 p-4 dark:border-dark-700">
        <h2 class="text-lg font-semibold">{{ entry?.label || t('admin.plugins.businessTitle') }}</h2>
      </div>
      <div v-if="loading" class="p-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="failure" role="alert" class="space-y-4 p-8 text-center">
        <p>{{ failure }}</p>
        <div class="flex justify-center gap-2">
          <button type="button" class="btn btn-secondary" @click="loadEntry">{{ t('admin.plugins.retry') }}</button>
          <button type="button" class="btn btn-secondary" @click="router.back()">{{ t('admin.plugins.goBack') }}</button>
        </div>
      </div>
      <PluginFrame
        v-else-if="entry"
        :key="`${store.identityVersion}:${entry.plugin_id}:${entry.entry_id}:${entry.revision}`"
        :entry="entry"
        @unavailable="frameUnavailable"
      />
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { PluginEntry } from '@/api/plugins'
import { usePluginEntriesStore } from '@/stores/pluginEntries'
import AppLayout from '@/components/layout/AppLayout.vue'
import PluginFrame from '@/components/plugins/PluginFrame.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const store = usePluginEntriesStore()
const entry = ref<PluginEntry | null>(null)
const loading = ref(true)
const failure = ref('')
let generation = 0

// The rendered frame keeps this closure until it is replaced. A notice from
// that frame must not close the page while a new identity or entry is loading.
const frameUnavailable = computed(() => {
  const identity = store.identityVersion
  const version = generation
  const current = entry.value
  return (reason: 'disabled' | 'forbidden'): void => {
    if (identity !== store.identityVersion || version !== generation || !current ||
      !entry.value || entry.value.plugin_id !== current.plugin_id ||
      entry.value.entry_id !== current.entry_id || entry.value.revision !== current.revision) return
    closeUnavailable(reason)
  }
})

function matchingEntry(): PluginEntry | undefined {
  const pluginID = String(route.params.pluginID || '')
  const mount = route.meta.requiresAdmin === true ? 'admin' : 'user'
  if (!/^[1-9]\d*$/.test(pluginID)) return undefined
  return store.entries.find(item => String(item.plugin_id) === pluginID && item.entry_id === route.params.entryID && item.mount === mount)
}

async function loadEntry(): Promise<void> {
  const version = ++generation
  entry.value = null
  failure.value = ''
  loading.value = true
  await store.refresh()
  if (version !== generation) return
  if (store.error) failure.value = t('admin.plugins.pageLoadFailed')
  else {
    entry.value = matchingEntry() || null
    if (!entry.value) failure.value = t('admin.plugins.pageForbidden')
  }
  loading.value = false
}

function closeUnavailable(reason: 'disabled' | 'forbidden'): void {
  entry.value = null
  failure.value = t(reason === 'disabled' ? 'admin.plugins.pageDisabled' : 'admin.plugins.pageForbidden')
  void store.refresh()
}

watch(() => [route.params.pluginID, route.params.entryID, route.meta.requiresAdmin, store.identityVersion], () => { void loadEntry() }, { immediate: true, flush: 'sync' })
watch(() => store.entries, () => {
  if (!entry.value) return
  const current = matchingEntry()
  if (!current || store.error) {
    entry.value = null
    failure.value = t(store.error ? 'admin.plugins.pageLoadFailed' : 'admin.plugins.pageDisabled')
  } else entry.value = current
}, { flush: 'sync' })

onBeforeUnmount(() => { generation++ })
</script>
