<template>
  <BaseDialog :key="group?.representative_id || 0" keep-mounted prevent-scroll-on-focus :show="showGroup" :title="t('admin.ops.records.members')" width="full" @close="closeGroup">
    <div v-if="group && !memberLoading && !memberError && memberTotal !== group.count" class="mb-2 text-sm text-amber-700">{{ t('admin.ops.records.membersUpdated', { count: memberTotal }) }}</div>
    <div v-if="memberError" role="alert" class="mb-3 text-sm text-rose-600">{{ memberError }} <button class="ml-2 underline" @click="loadMembers(true)">{{ t('admin.ops.records.retry') }}</button></div>
    <OpsErrorLogTable v-if="group && !memberError" flat top-horizontal-scroll :rows="memberRows" :record-meta="memberMeta" :sort-by="memberSortBy" :sort-order="memberSortOrder" :loading="memberLoading" :total="memberTotal" :page="memberPage" :page-size="memberPageSize" :page-size-options="[10, 20, 50]" :column-width-storage-key="columnWidthStorageKey" user-clickable :quick-filters="quickFilters" @quick-filter="emit('quickFilter', $event)" @openErrorDetail="emit('openErrorDetail', $event)" @userClick="(id, email) => emit('userClick', id, email)" @sort="sortMembers" @update:page="memberPage = $event" @update:pageSize="memberPageSize = $event" />
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import OpsErrorLogTable from './OpsErrorLogTable.vue'
import { errorRecordRows, errorRecordMeta } from '../useOpsErrorRecords'
import type { useErrorRecordMembers } from '../useErrorRecordMembers'
import type { UsageQuickFilter } from '@/utils/usageQuickFilter'
const props = defineProps<{ members: ReturnType<typeof useErrorRecordMembers>; columnWidthStorageKey?: string; quickFilters?: boolean }>()
const emit = defineEmits<{ openErrorDetail: [id: number]; userClick: [id: number, email?: string]; quickFilter: [filter: UsageQuickFilter] }>()
const { t } = useI18n()
const { group, showGroup, memberItems, memberTotal, memberPage, memberPageSize, memberSortBy, memberSortOrder, memberLoading, memberError, closeGroup, loadMembers, sortMembers } = props.members
const memberRows = computed(() => errorRecordRows(memberItems.value))
const memberMeta = computed(() => errorRecordMeta(memberItems.value))
</script>
