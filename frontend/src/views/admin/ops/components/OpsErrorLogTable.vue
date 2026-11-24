<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden" :class="flat ? '' : 'card'">
      <IpGeoBatchToolbar :ips="rows.map((r) => r.client_ip)" @failed="emit('ipGeoBatchFailed')" />

      <DataTable
        :columns="columns"
        :data="rows"
        :column-width-storage-key="columnWidthStorageKey"
        :loading="loading"
        clickable-rows
        server-side-sort
        :default-sort-key="sortBy === 'status_code' ? 'status' : (sortBy || 'created_at')"
        :default-sort-order="sortOrder || 'desc'"
        @sort="onSort"
        @rowClick="(row) => emit('openErrorDetail', row.id)"
      >
        <template #cell-created_at="{ row }">
          <div v-if="(recordOf(row)?.count ?? 0) > 1" class="space-y-0.5 text-xs text-gray-500 dark:text-gray-400">
            <div class="font-medium text-gray-700 dark:text-gray-200">{{ t('admin.ops.records.latest') }} {{ formatDateTime(recordOf(row)?.last_at || row.created_at) }}</div>
            <div>{{ t('admin.ops.records.first') }} {{ formatDateTime(recordOf(row)?.first_at || row.created_at) }}</div>
            <div>{{ t('admin.ops.records.span') }} {{ formatSpan(recordOf(row)) }}</div>
          </div>
          <span v-else
            class="text-sm text-gray-600 dark:text-gray-400"
            :title="row.request_id || row.client_request_id"
          >{{ formatDateTime(row.created_at) }}</span>
        </template>

        <template #cell-type="{ row }">
          <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="getTypeBadge(row).className">
            {{ getTypeBadge(row).label }}
          </span>
        </template>

        <template #cell-endpoint="{ row }">
          <div class="max-w-[320px] space-y-1 text-xs">
            <div class="break-all text-gray-700 dark:text-gray-300">
              <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('usage.inbound') }}:</span>
              <span class="ml-1">{{ row.inbound_endpoint?.trim() || '-' }}</span>
            </div>
            <div v-if="row.upstream_endpoint" class="break-all text-gray-700 dark:text-gray-300">
              <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('usage.upstream') }}:</span>
              <span class="ml-1">{{ row.upstream_endpoint?.trim() || '-' }}</span>
            </div>
          </div>
        </template>

        <template #cell-platform="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.platform || '-' }}</span>
        </template>

        <template #cell-model="{ row }">
          <div v-if="hasModelMapping(row)" class="space-y-0.5 text-xs">
            <div class="break-all font-medium text-gray-900 dark:text-white">{{ row.requested_model }}</div>
            <div class="break-all text-gray-500 dark:text-gray-400"><span class="mr-0.5">↳</span>{{ row.upstream_model }}</div>
          </div>
          <span v-else-if="displayModel(row)" class="text-sm font-medium text-gray-900 dark:text-white">{{ displayModel(row) }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-group="{ row }">
          <span
            v-if="row.group_id"
            class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200"
            :title="t('admin.ops.errorLog.id') + ' ' + row.group_id"
          >
            {{ row.group_name || '#' + row.group_id }}
          </span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-user="{ row }">
          <button v-if="(recordOf(row)?.user_count ?? 0) > 1" type="button" class="text-sm text-primary-600 dark:text-primary-400" @click.stop="emit('openErrorDetail', row.id)">{{ t('admin.ops.records.users', { count: recordOf(row)?.user_count }) }}</button>
          <div v-else-if="row.user_id" class="text-sm">
            <button
              v-if="userClickable && row.user_email"
              class="font-medium text-primary-600 underline decoration-dashed underline-offset-2 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              :class="recordOf(row) ? 'ops-record-identity max-w-[140px] truncate align-middle' : ''"
              :title="recordOf(row) ? row.user_email : t('admin.usage.clickToViewBalance')"
              @click.stop="emit('userClick', row.user_id, row.user_email)"
            >
              {{ row.user_email }}
            </button>
            <span v-else class="font-medium text-gray-900 dark:text-white">{{ row.user_email || '-' }}</span>
            <span class="ml-1 text-gray-500 dark:text-gray-400">#{{ row.user_id }}</span>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          <div v-if="recordOf(row)?.has_unknown_user" class="text-xs text-gray-400">{{ t('admin.ops.records.unknownUser') }}</div>
          <div v-if="recordOf(row) && !columns.some(c => c.key === 'api_key')" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            <span v-if="(recordOf(row)?.api_key_count ?? 0) > 1">{{ t('admin.ops.records.keys', { count: recordOf(row)?.api_key_count }) }}</span>
            <span v-else-if="row.api_key_id"><span :class="recordOf(row) ? 'ops-record-identity inline-block max-w-[140px] truncate align-middle' : ''" :title="row.api_key_name">{{ row.api_key_name || '#' + row.api_key_id }}</span><span v-if="row.api_key_deleted" class="ml-1 text-rose-500">{{ t('admin.ops.errorLog.keyDeletedBadge') }}</span></span>
            <span v-if="recordOf(row)?.has_unknown_api_key" class="ml-1">{{ t('admin.ops.records.unknownKey') }}</span>
          </div>
        </template>

        <template #cell-api_key="{ row }">
          <button v-if="(recordOf(row)?.api_key_count ?? 0) > 1" type="button" class="text-sm text-primary-600 dark:text-primary-400" @click.stop="emit('openErrorDetail', row.id)">{{ t('admin.ops.records.keys', { count: recordOf(row)?.api_key_count }) }}</button>
          <div v-else-if="row.api_key_id || row.api_key_name" class="text-sm">
            <span class="text-gray-900 dark:text-white">{{ row.api_key_name || '#' + row.api_key_id }}</span>
            <span
              v-if="row.api_key_deleted"
              class="ml-1 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30"
            >{{ t('admin.ops.errorLog.keyDeletedBadge') }}</span>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          <div v-if="recordOf(row)?.has_unknown_api_key" class="text-xs text-gray-400">{{ t('admin.ops.records.unknownKey') }}</div>
        </template>

        <template #cell-account="{ row }">
          <span
            v-if="row.account_id"
            class="text-sm text-gray-900 dark:text-white"
            :class="recordOf(row) ? 'ops-record-identity inline-block max-w-[140px] truncate align-middle' : ''"
            :title="(row.account_name || '') + ' · ' + t('admin.ops.errorLog.accountId') + ' ' + row.account_id"
          >{{ row.account_name || '#' + row.account_id }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-category="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">
            {{ t('usage.errors.categories.' + mapErrorCategory(row.phase, row.type)) }}
          </span>
        </template>

        <template #cell-status="{ row }">
          <div class="flex items-center gap-1.5">
            <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="getStatusClass(row.status_code)">
              {{ row.status_code }}
            </span>
            <span
              v-if="row.severity"
              :class="['rounded px-1.5 py-0.5 text-[10px] font-medium', getSeverityClass(row.severity)]"
            >{{ row.severity }}</span>
            <span
              v-if="row.request_type != null && row.request_type > 0"
              class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-gray-100 text-gray-800 dark:bg-dark-700 dark:text-gray-200"
            >{{ formatRequestType(row.request_type) }}</span>
          </div>
        </template>

        <template #cell-message="{ row }">
          <div :class="recordOf(row) ? ['rounded border-l-2 px-2 py-1', messageTone(row.severity)] : ''">
            <div v-if="recordOf(row)" class="mb-1 flex flex-wrap items-center gap-1 text-[11px]">
              <span class="rounded px-1.5 py-0.5 font-medium" :class="getStatusClass(row.status_code)">{{ row.status_code }}</span>
              <span v-if="recordOf(row)?.is_request_error" class="text-rose-600 dark:text-rose-400">{{ t('admin.ops.records.request') }}</span>
              <span v-if="recordOf(row)?.is_upstream_error" class="text-amber-700 dark:text-amber-400">{{ t('admin.ops.records.upstream') }}</span>
              <button v-if="(recordOf(row)?.count ?? 0) > 1" type="button" class="ml-1 rounded bg-primary-50 px-1.5 py-0.5 font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300" @click.stop="emit('openErrorDetail', row.id)">{{ t('admin.ops.records.count', { count: recordOf(row)?.count }) }}</button>
            </div>
            <span v-if="row.message" class="block text-sm text-gray-700 dark:text-gray-300" :class="recordOf(row) ? 'ops-record-message line-clamp-2 max-w-[340px] break-words' : 'max-w-[280px] truncate'" :title="row.message">{{ formatSmartMessage(row.message) || '-' }}</span>
            <span v-else class="text-sm text-gray-400">-</span>
          </div>
        </template>

        <template #cell-user_agent="{ row }">
          <span v-if="(recordOf(row)?.user_agent_count ?? 0) > 1" class="text-xs text-gray-500">{{ t('admin.ops.records.multiple') }}</span>
          <span
            v-else-if="row.user_agent"
            class="block max-w-[320px] truncate text-sm text-gray-600 dark:text-gray-400"
            :title="row.user_agent"
          >{{ row.user_agent }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          <div v-if="recordOf(row)?.has_unknown_user_agent" class="text-xs text-gray-400">{{ t('admin.ops.records.unknownValue') }}</div>
        </template>

        <template #cell-client_ip="{ row }">
          <div @click.stop>
            <button v-if="(recordOf(row)?.client_ip_count ?? 0) > 1" class="text-xs text-primary-600" @click="emit('openErrorDetail', row.id)">{{ t('admin.ops.records.multiple') }}</button>
            <div v-else-if="row.client_ip">
              <span class="text-sm font-mono text-gray-600 dark:text-gray-400">{{ row.client_ip }}</span>
              <IpGeoCell :ip="row.client_ip" />
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
            <div v-if="recordOf(row)?.has_unknown_client_ip" class="text-xs text-gray-400">{{ t('admin.ops.records.unknownValue') }}</div>
          </div>
        </template>

        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1">
            <button
              type="button"
              class="shrink-0 rounded p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-600 dark:hover:text-primary-400"
              :title="t('admin.ops.errorLog.details')"
              @click.stop="emit('openErrorDetail', row.id)"
            >
              <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>
            </button>
            <button
              v-if="row.user_id && row.account_id"
              type="button"
              class="shrink-0 rounded p-1 text-gray-400 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:hover:bg-red-900/30 dark:hover:text-red-400"
              :title="blockAccountLabel(row)"
              :aria-label="blockAccountLabel(row)"
              :disabled="blocking"
              @click.stop="blockAccount(row)"
            >
              <Icon name="ban" size="sm" />
            </button>
          </div>
        </template>

        <template #empty><EmptyState :message="emptyMessage || t('admin.ops.errorLog.noErrors')" /></template>
      </DataTable>
    </div>

    <div class="flex-shrink-0">
      <Pagination
        v-if="total > 0"
        :total="total"
        :page="page"
        :page-size="pageSize"
        :page-size-options="pageSizeOptions"
        :persist-page-size="pageSizeOptions === undefined"
        @update:page="emit('update:page', $event)"
        @update:pageSize="emit('update:pageSize', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import IpGeoBatchToolbar from '@/components/common/IpGeoBatchToolbar.vue'
import { createUserAccountBlock } from '@/api/admin/settings'
import { useAppStore } from '@/stores/app'
import { formatDateTime as formatBlockDateTime } from '@/utils/format'
import { blockWriteErrorMessage } from '@/views/admin/settings/userAccountBlockErrors'
import type { OpsErrorLog, OpsErrorRecord } from '@/api/admin/ops'
import type { Column } from '@/components/common/types'
import { getSeverityClass, formatDateTime } from '../utils/opsFormatters'
import { mapErrorCategory } from '@/utils/errorCategory'
import { mapErrorSortKey, statusCodeBadgeClass } from '@/utils/errorBadges'

const { t } = useI18n()
const appStore = useAppStore()
const blocking = ref(false)

function blockAccountLabel(row: OpsErrorLog) {
  return t('admin.ops.errorLog.blockAccount', {
    user: `${row.user_email ? `${row.user_email} ` : ''}#${row.user_id}`,
    account: `${row.account_name ? `${row.account_name} ` : ''}#${row.account_id}`,
  })
}

async function blockAccount(row: OpsErrorLog) {
  if (blocking.value || !row.user_id || !row.account_id) return
  blocking.value = true
  try {
    const item = await createUserAccountBlock(row.user_id, row.account_id)
    appStore.showSuccess(t('admin.ops.errorLog.blockAccountSuccess', { time: formatBlockDateTime(item.expires_at) }))
  } catch (error) {
    const message = blockWriteErrorMessage(error, t, 'admin.ops.errorLog.blockAccountFailed')
    if (message) appStore.showError(message)
  } finally {
    blocking.value = false
  }
}


// 列序对齐管理端用量明细:身份(用户→Key→账号)→ 请求形态(平台→模型→端点→分组→类型)
// → 结果(状态→消息)→ 时间→UA→IP→操作
const allColumns = computed<Column[]>(() => [
  { key: 'user', label: t('admin.ops.errorLog.user') },
  { key: 'api_key', label: t('admin.ops.errorLog.apiKey') },
  { key: 'account', label: t('admin.ops.errorLog.account') },
  { key: 'platform', label: t('admin.ops.errorLog.platform') },
  { key: 'model', label: t('admin.ops.errorLog.model'), sortable: true },
  { key: 'endpoint', label: t('admin.ops.errorLog.endpoint') },
  { key: 'group', label: t('admin.ops.errorLog.group') },
  { key: 'type', label: t('admin.ops.errorLog.type') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('admin.ops.errorLog.status'), sortable: true },
  { key: 'message', label: t('admin.ops.errorLog.message') },
  { key: 'created_at', label: t('admin.ops.errorLog.time'), sortable: true },
  { key: 'user_agent', label: t('usage.userAgent') },
  { key: 'client_ip', label: t('admin.ops.errorLog.ip') },
  { key: 'actions', label: t('admin.ops.errorLog.action'), class: '!px-2' },
])

// 传入 visibleColumnKeys 时按其过滤(列设置);未传则全量(Ops 弹窗等使用方)
const columns = computed<Column[]>(() => {
  const visibleColumns = props.visibleColumnKeys
    ? allColumns.value.filter((c) => props.visibleColumnKeys!.includes(c.key))
    : allColumns.value
  if (props.columnOrder) return [...visibleColumns].sort((a, b) => props.columnOrder!.indexOf(a.key) - props.columnOrder!.indexOf(b.key))
  if (!props.summaryFirst) return visibleColumns

  return [
    ...visibleColumns.filter((c) => c.key === 'created_at'),
    ...visibleColumns.filter((c) => c.key === 'message'),
    ...visibleColumns.filter((c) => c.key !== 'created_at' && c.key !== 'message'),
  ]
})

function isUpstreamRow(log: OpsErrorLog): boolean {
  const phase = String(log.phase || '').toLowerCase()
  const owner = String(log.error_owner || '').toLowerCase()
  return phase === 'upstream' && owner === 'provider'
}

function hasModelMapping(log: OpsErrorLog): boolean {
  const requested = String(log.requested_model || '').trim()
  const upstream = String(log.upstream_model || '').trim()
  return !!requested && !!upstream && requested !== upstream
}

function displayModel(log: OpsErrorLog): string {
  const upstream = String(log.upstream_model || '').trim()
  if (upstream) return upstream
  const requested = String(log.requested_model || '').trim()
  if (requested) return requested
  return String(log.model || '').trim()
}

function formatRequestType(type: number | null | undefined): string {
  switch (type) {
    case 1: return t('admin.ops.errorLog.requestTypeSync')
    case 2: return t('admin.ops.errorLog.requestTypeStream')
    case 3: return t('admin.ops.errorLog.requestTypeWs')
    default: return ''
  }
}

// 徽章配色对齐用量明细(UsageTable)的 bg-X-100/text-X-800 体系
function getTypeBadge(log: OpsErrorLog): { label: string; className: string } {
  const phase = String(log.phase || '').toLowerCase()
  const owner = String(log.error_owner || '').toLowerCase()

  if (isUpstreamRow(log)) {
    return { label: t('admin.ops.errorLog.typeUpstream'), className: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200' }
  }
  if (phase === 'request' && owner === 'client') {
    return { label: t('admin.ops.errorLog.typeRequest'), className: 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200' }
  }
  if (phase === 'auth' && owner === 'client') {
    return { label: t('admin.ops.errorLog.typeAuth'), className: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200' }
  }
  if (phase === 'account_auth') {
    return { label: t('admin.ops.errorLog.typeAccountAuth'), className: 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200' }
  }
  if (phase === 'routing' && owner === 'platform') {
    return { label: t('admin.ops.errorLog.typeRouting'), className: 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200' }
  }
  if (phase === 'internal' && owner === 'platform') {
    return { label: t('admin.ops.errorLog.typeInternal'), className: 'bg-gray-100 text-gray-800 dark:bg-dark-700 dark:text-gray-200' }
  }

  const fallback = phase || owner || t('common.unknown')
  return { label: fallback, className: 'bg-gray-100 text-gray-800 dark:bg-dark-700 dark:text-gray-200' }
}

interface Props {
  recordMeta?: Record<number, OpsErrorRecord>
  columnOrder?: string[]
  pageSizeOptions?: number[]
  columnWidthStorageKey?: string
  sortBy?: string
  sortOrder?: 'asc' | 'desc'
  emptyMessage?: string
  rows: OpsErrorLog[]
  total: number
  loading: boolean
  page: number
  pageSize: number
  /** 用户邮箱可点击(emit userClick),仅在有弹窗承接的使用方开启 */
  userClickable?: boolean
  /** 列设置:仅显示这些 key 的列;不传则全量 */
  visibleColumnKeys?: string[]
  /** 运维弹窗优先展示时间和响应内容 */
  summaryFirst?: boolean
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

interface Emits {
  (e: 'openErrorDetail', id: number): void
  (e: 'update:page', value: number): void
  (e: 'update:pageSize', value: number): void
  (e: 'ipGeoBatchFailed'): void
  (e: 'sort', sortBy: string, sortOrder: 'asc' | 'desc'): void
  (e: 'userClick', userId: number, email?: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const recordOf = (row: OpsErrorLog) => props.recordMeta?.[row.id]
function formatSpan(record?: OpsErrorRecord): string {
  if (!record) return ''
  const seconds = Math.max(0, Math.floor((Date.parse(record.last_at) - Date.parse(record.first_at)) / 1000))
  return seconds >= 60 ? t('admin.ops.records.minutes', { count: Math.floor(seconds / 60) }) : t('admin.ops.records.seconds', { count: seconds })
}
function messageTone(severity: string): string {
  return ({ P0: 'border-red-400 bg-red-50/40 dark:bg-red-900/10', P1: 'border-orange-400 bg-orange-50/40 dark:bg-orange-900/10', P2: 'border-yellow-400 bg-yellow-50/30 dark:bg-yellow-900/10', P3: 'border-blue-300 bg-blue-50/30 dark:bg-blue-900/10' } as Record<string, string>)[severity] || 'border-gray-300 bg-gray-50/40 dark:bg-dark-800'
}

function onSort(key: string, order: 'asc' | 'desc') {
  emit('sort', mapErrorSortKey(key), order)
}

const getStatusClass = statusCodeBadgeClass

function formatSmartMessage(msg: string): string {
  if (!msg) return ''

  if (msg.startsWith('{') || msg.startsWith('[')) {
    try {
      const obj = JSON.parse(msg)
      if (obj?.error?.message) return String(obj.error.message)
      if (obj?.message) return String(obj.message)
      if (obj?.detail) return String(obj.detail)
      if (typeof obj === 'object') return JSON.stringify(obj).substring(0, 150)
    } catch {
      // ignore parse error
    }
  }

  if (msg.includes('context deadline exceeded')) return t('admin.ops.errorLog.commonErrors.contextDeadlineExceeded')
  if (msg.includes('connection refused')) return t('admin.ops.errorLog.commonErrors.connectionRefused')
  if (msg.toLowerCase().includes('rate limit')) return t('admin.ops.errorLog.commonErrors.rateLimit')

  return msg.length > 200 ? msg.substring(0, 200) + '...' : msg
}
</script>

<style scoped>
/* Auto layout stays compact; explicit column widths control the full cell. */
:deep(table[style*="table-layout: fixed"] .ops-record-identity),
:deep(table[style*="table-layout: fixed"] .ops-record-message) {
  max-width: 100%;
}
</style>
