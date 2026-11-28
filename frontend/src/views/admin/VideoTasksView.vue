<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <form class="card space-y-4 p-4 sm:p-6" @submit.prevent="search">
          <div class="flex flex-wrap items-end gap-4">
            <label class="w-full sm:w-48">
              <span class="input-label">{{ t('admin.videoTasks.columns.status') }}</span>
              <Select v-model="status" :options="statuses" />
            </label>
            <label class="w-full sm:w-40">
              <span class="input-label">{{ t('admin.videoTasks.userId') }}</span>
              <input
                v-model.trim="userId" class="input" type="text" inputmode="numeric"
                :aria-invalid="filterError === 'invalidUserId' || undefined"
                :aria-describedby="filterError === 'invalidUserId' ? 'video-filter-error' : undefined"
              />
            </label>
            <label class="w-full sm:w-72">
              <span class="input-label">{{ t('admin.videoTasks.columns.task_id') }}</span>
              <input
                v-model.trim="taskId" class="input font-mono" type="text"
                :aria-invalid="filterError === 'invalidTaskId' || undefined"
                :aria-describedby="filterError === 'invalidTaskId' ? 'video-filter-error' : undefined"
              />
            </label>
            <label class="flex items-center gap-2 self-center text-sm">
              <input v-model="riskOnly" type="checkbox" class="rounded border-gray-300 text-primary-600 dark:border-dark-600" />
              {{ t('admin.videoTasks.riskFilter') }}
            </label>
            <button class="btn btn-primary" type="submit">{{ t('common.search') }}</button>
            <button class="btn btn-secondary" type="button" @click="loadPage">{{ t('common.refresh') }}</button>
            <div ref="columnDropdownRef" class="relative">
              <button type="button" class="btn btn-secondary" :aria-expanded="showColumnDropdown"
                :title="t('admin.videoTasks.columnSettings')" @click="showColumnDropdown = !showColumnDropdown">
                <Icon name="grid" size="md" class="mr-2" />
                {{ t('admin.videoTasks.columnSettings') }}
              </button>
              <div v-if="showColumnDropdown" class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800">
                <button v-for="col in toggleableColumns" :key="col.key" type="button"
                  :aria-pressed="!hiddenColumns.has(col.key)" @click="toggleColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700">
                  <span>{{ col.label }}</span>
                  <Icon v-if="!hiddenColumns.has(col.key)" name="check" size="sm" class="text-primary-500" :stroke-width="2" />
                </button>
              </div>
            </div>
          </div>
          <p v-if="filterError" id="video-filter-error" class="text-sm text-red-600 dark:text-red-400" role="alert">
            {{ t(`admin.videoTasks.${filterError}`) }}
          </p>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.videoTasks.riskExplanation') }}</p>
        </form>
      </template>

      <template #table>
        <div v-if="failed" class="card py-8 text-center" role="alert">
          <p class="mb-3">{{ t('admin.videoTasks.loadFailed') }}</p>
          <button class="btn btn-secondary" type="button" data-test="retry-video-tasks" @click="loadPage">
            {{ t('common.tryAgain') }}
          </button>
        </div>
        <DataTable v-else :columns="columns" :data="tasks" :loading="loading" row-key="task_id">
          <template #cell-task_id="{ value }">
            <span class="font-mono text-xs">{{ value }}</span>
          </template>
          <template #cell-user_id="{ row }">
            <span>{{ row.user_email || (!row.user_name ? '—' : '') }}</span>
            <span v-if="row.user_name" class="block text-xs text-gray-500 dark:text-gray-400">{{ row.user_name }}</span>
          </template>
          <template #cell-group_id="{ row }">{{ row.group_name || '—' }}</template>
          <template #cell-account_id="{ row }">
            <span>{{ row.account_name || t('admin.videoTasks.deletedAccount') }}</span>
            <span v-if="row.account_name && row.account_deleted" class="ml-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.videoTasks.deleted') }}</span>
          </template>
          <template #cell-seconds="{ value }">{{ t('admin.videoTasks.duration', { seconds: value }) }}</template>
          <template #cell-hold_amount="{ value }">${{ formatCostFixed(value) }}</template>
          <template #cell-status="{ value }">{{ t(`admin.videoTasks.statuses.${value}`) }}</template>
          <template #cell-billing_state="{ value }">{{ t(`admin.videoTasks.billing.${value}`) }}</template>
          <template #cell-failure_reason="{ row }">
            <span>{{ row.failure_reason ? t(`admin.videoTasks.reasons.${row.failure_reason}`) : '—' }}</span>
            <span v-if="row.risk" class="badge badge-warning mt-1 block w-fit">
              {{ t('admin.videoTasks.providerCostBadge') }}
            </span>
          </template>
          <template #cell-upstream_task_id="{ value }">
            <span class="font-mono text-xs">{{ value || '—' }}</span>
          </template>
          <template #cell-created_at="{ value }">{{ formatDateTime(value) }}</template>
          <template #cell-settled_at="{ value }">{{ value ? formatDateTime(value) : '—' }}</template>
          <template #empty>
            <p class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.videoTasks.empty') }}</p>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="!failed && total > 0"
          :total="total"
          :page="page"
          :page-size="pageSize"
          @update:page="changePage"
          @update:page-size="changePageSize"
        />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { VideoTask, VideoTasksQuery, VideoTaskStatus } from '@/api/admin/videoTasks'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import { formatCostFixed, formatDateTime } from '@/utils/format'

const { t } = useI18n()
const tasks = ref<VideoTask[]>([])
const loading = ref(false)
const failed = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const status = ref<VideoTaskStatus | ''>('')
const userId = ref('')
const taskId = ref('')
const riskOnly = ref(false)
const filterError = ref<'invalidUserId' | 'invalidTaskId' | ''>('')

let latestRequest = 0
let applied: Omit<VideoTasksQuery, 'page' | 'page_size'> = {}

const statuses = computed(() => [
  { label: t('common.all'), value: '' },
  ...(['processing', 'confirmation', 'completed', 'failed'] as const).map(value => ({
    label: t(`admin.videoTasks.statuses.${value}`), value
  }))
])

const allColumns = computed<Column[]>(() => [
  'task_id', 'user_id', 'group_id', 'account_id', 'model', 'upstream_model', 'seconds',
  'resolution', 'hold_amount', 'status', 'billing_state', 'failure_reason', 'upstream_task_id',
  'created_at', 'settled_at'
].map(key => ({ key, label: t(`admin.videoTasks.columns.${key}`) })))

const HIDDEN_COLUMNS_KEY = 'video-tasks-hidden-columns'
const COLUMN_SETTINGS_VERSION_KEY = 'video-tasks-column-settings-version'
const COLUMN_SETTINGS_VERSION = 1
const hiddenColumns = ref(new Set<string>(['group_id']))
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)
const toggleableColumns = computed(() => allColumns.value.filter(col => col.key !== 'task_id'))
const columns = computed(() => allColumns.value.filter(col => !hiddenColumns.value.has(col.key)))

function loadSavedColumns() {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved === null) {
      saveColumns()
      return
    }
    const parsed: unknown = JSON.parse(saved)
    if (Array.isArray(parsed)) {
      const validKeys = new Set(toggleableColumns.value.map(col => col.key))
      hiddenColumns.value = new Set(parsed.filter((key): key is string => typeof key === 'string' && validKeys.has(key)))
      if (localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) === null) saveColumns()
    }
  } catch {
    // Keep the default column visibility when storage is unavailable.
  }
}

function saveColumns() {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns.value]))
    localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
  } catch {
    // The current page remains usable even when storage is unavailable.
  }
}

function toggleColumn(key: string) {
  if (!toggleableColumns.value.some(col => col.key === key)) return
  if (hiddenColumns.value.has(key)) hiddenColumns.value.delete(key)
  else hiddenColumns.value.add(key)
  saveColumns()
}

function handleClickOutside(event: MouseEvent) {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as Node)) {
    showColumnDropdown.value = false
  }
}

async function loadPage() {
  const request = ++latestRequest
  loading.value = true
  failed.value = false
  try {
    const result = await adminAPI.videoTasks.list({ page: page.value, page_size: pageSize.value, ...applied })
    if (request === latestRequest) {
      tasks.value = result.items
      total.value = result.total
    }
  } catch {
    if (request === latestRequest) {
      failed.value = true
      tasks.value = []
      total.value = 0
    }
  } finally {
    if (request === latestRequest) loading.value = false
  }
}

function isValidUserId(value: string) {
  if (!/^\+?[0-9]+$/.test(value)) return false
  const id = BigInt(value)
  return id > 0n && id <= 9223372036854775807n
}

function search() {
  if (userId.value && !isValidUserId(userId.value)) {
    filterError.value = 'invalidUserId'
    return
  }
  if (taskId.value && !/^video_[0-9a-f]{32}$/.test(taskId.value)) {
    filterError.value = 'invalidTaskId'
    return
  }
  filterError.value = ''
  applied = {
    status: status.value || undefined,
    user_id: userId.value || undefined,
    task_id: taskId.value || undefined,
    risk_only: riskOnly.value ? true : undefined
  }
  page.value = 1
  void loadPage()
}

function changePage(value: number) {
  page.value = value
  void loadPage()
}

function changePageSize(value: number) {
  pageSize.value = value
  page.value = 1
  void loadPage()
}

watch([status, riskOnly], search)
onMounted(() => {
  loadSavedColumns()
  document.addEventListener('click', handleClickOutside)
  void loadPage()
})
onUnmounted(() => document.removeEventListener('click', handleClickOutside))
</script>
