<template>
  <div ref="root" class="relative min-w-0">
    <label class="input-label" :for="inputId">{{ label }}</label>
    <div class="flex items-center gap-1">
      <input :id="inputId" v-model="keyword" class="input min-w-0" :placeholder="label" autocomplete="off" @input="search" @focus="open = true; search()" @keydown.esc="open = false" />
      <button v-if="id" type="button" class="btn btn-secondary px-2" :aria-label="t('common.clear')" @click="clear">×</button>
    </div>
    <div v-if="open && (items.length || failed || showHistory)" class="absolute z-40 mt-1 max-h-56 w-full overflow-auto rounded-lg border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-600 dark:bg-dark-800">
      <AdminUserHistoryList v-if="showHistory && !keyword.trim()" @select="item => select({ id: item.id, label: item.email })" />
      <button v-for="item in items" :key="item.id" type="button" class="block w-full truncate rounded px-2 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700" @click="select(item)">{{ item.label }} <span class="text-gray-400">#{{ item.id }}</span></button>
      <button v-if="failed" type="button" class="px-2 py-2 text-sm text-rose-600" @click="search">{{ t('admin.ops.records.searchFailed') }}</button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AdminUserHistoryList from '@/components/admin/usage/AdminUserHistoryList.vue'
import { useAdminUserHistory } from '@/composables/useAdminUserHistory'
import { adminAPI } from '@/api/admin'
const props = defineProps<{ kind: 'user' | 'key' | 'account'; id?: number; userId?: number; label: string }>()
const emit = defineEmits<{ (e: 'update:id', id: number | undefined): void }>()
const { t } = useI18n()
const history = useAdminUserHistory()
const showHistory = computed(() => props.kind === 'user' && history.enabled.value && (history.entries.value.length > 0 || history.failed.value))
const inputId = `ops-error-filter-${props.kind}`
const root = ref<HTMLElement | null>(null)
const keyword = ref(props.id ? `#${props.id}` : '')
const selectedId = ref(props.id)
const open = ref(false)
const failed = ref(false)
const items = ref<Array<{ id: number; label: string }>>([])
let timer: ReturnType<typeof setTimeout> | undefined
let sequence = 0
function invalidate() { sequence++; clearTimeout(timer); items.value = []; failed.value = false }
function search() {
  invalidate(); const seq = sequence; const q = keyword.value.trim(); open.value = true
  if (!q && props.kind !== 'key') return
  timer = setTimeout(async () => {
    try {
      let result: Array<{ id: number; label: string }>
      if (props.kind === 'user') result = (await adminAPI.usage.searchUsers(q)).map(x => ({ id: x.id, label: x.email }))
      else if (props.kind === 'key') result = (await adminAPI.usage.searchApiKeys(props.userId, q)).map(x => ({ id: x.id, label: x.name || `#${x.id}` }))
      else result = (await adminAPI.accounts.list(1, 20, { search: q })).items.map(x => ({ id: x.id, label: x.name }))
      if (seq === sequence) items.value = result
    } catch { if (seq === sequence) failed.value = true }
  }, 300)
}
function select(item: { id: number; label: string }) { if (props.kind === 'user') history.record(item.id, item.label); invalidate(); selectedId.value = item.id; emit('update:id', item.id); keyword.value = item.label; open.value = false }
function clear() { invalidate(); selectedId.value = undefined; keyword.value = ''; open.value = false; emit('update:id', undefined) }
watch(() => props.id, id => { invalidate(); if (id !== selectedId.value) keyword.value = id ? `#${id}` : ''; selectedId.value = id })
watch(() => props.userId, () => { invalidate(); open.value = false })
function outside(event: MouseEvent) { if (!root.value?.contains(event.target as Node)) open.value = false }
onMounted(() => document.addEventListener('click', outside))
onUnmounted(() => { invalidate(); document.removeEventListener('click', outside) })
</script>
