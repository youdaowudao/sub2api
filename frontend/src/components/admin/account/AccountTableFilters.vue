<template>
  <!-- 窄屏两列网格铺满宽度，sm 起恢复单行流式排布 -->
  <div class="grid w-full grid-cols-2 gap-3 sm:flex sm:w-auto sm:flex-wrap sm:items-center">
    <SearchInput
      :model-value="searchQuery"
      :placeholder="t('admin.accounts.searchAccounts')"
      class="col-span-2 sm:w-52"
      @update:model-value="$emit('update:searchQuery', $event)"
    />
    <Select :disabled="disabled" :model-value="filters.platform" class="min-w-0 sm:w-40" :options="pOpts" @update:model-value="updatePlatform" @change="$emit('change')" />
    <Select :disabled="disabled" :model-value="filters.type" class="min-w-0 sm:w-40" :options="tOpts" @update:model-value="updateType" @change="$emit('change')" />
    <Select :disabled="disabled" :model-value="filters.status" class="min-w-0 sm:w-40" :options="sOpts" @update:model-value="updateStatus" @change="$emit('change')" />
    <Select :disabled="disabled" :model-value="filters.privacy_mode" class="min-w-0 sm:w-48" :options="privacyOpts" @update:model-value="updatePrivacyMode" @change="$emit('change')" />
    <AdminGroupPicker :disabled="disabled" :model-value="filters.group" class="col-span-2 min-w-0 sm:w-40" :groups="groups ?? []" :special-options="specialGroupOptions" :options="gOpts" @update:model-value="updateGroup" @change="$emit('change')" />
    <button v-if="!located" type="button" class="btn btn-secondary text-xs" data-testid="account-default-group" :disabled="disabled" @click="$emit('set-default-group', filters.group)">
      {{ filters.group === defaultGroup && defaultGroup !== '' ? t('admin.accounts.cancelDefaultGroup') : t('admin.accounts.setDefaultGroup') }}
    </button>
    <span v-if="defaultGroup" class="text-xs text-gray-500">{{ t('admin.accounts.currentDefaultGroup', { name: defaultGroupLabel }) }}</span>
  </div>
</template>

<script setup lang="ts">
import { buildAdminGroupSelectOptions } from '@/utils/groupDisplay'

import { computed } from 'vue'; import { useI18n } from 'vue-i18n'; import Select from '@/components/common/Select.vue'; import SearchInput from '@/components/common/SearchInput.vue'
import type { AdminGroup } from '@/types'
import AdminGroupPicker from '@/components/admin/AdminGroupPicker.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
const props = defineProps<{ searchQuery: string; filters: Record<string, any>; groups?: AdminGroup[]; defaultGroup?: string; located?: boolean; disabled?: boolean }>()
const emit = defineEmits(['update:searchQuery', 'update:filters', 'change', 'set-default-group']); const { t } = useI18n()
const updatePlatform = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, platform: value }) }
const updateType = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, type: value }) }
const updateStatus = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, status: value }) }
const updatePrivacyMode = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, privacy_mode: value }) }
const updateGroup = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, group: value }) }
const pOpts = computed(() => [{ value: '', label: t('admin.accounts.allPlatforms') }, ...CONCRETE_PLATFORM_OPTIONS])
const tOpts = computed(() => [{ value: '', label: t('admin.accounts.allTypes') }, { value: 'oauth', label: t('admin.accounts.oauthType') }, { value: 'setup-token', label: t('admin.accounts.setupToken') }, { value: 'apikey', label: t('admin.accounts.apiKey') }, { value: 'bedrock', label: 'AWS Bedrock' }])
const sOpts = computed(() => [{ value: '', label: t('admin.accounts.allStatus') }, { value: 'active', label: t('admin.accounts.status.active') }, { value: 'inactive', label: t('admin.accounts.status.inactive') }, { value: 'error', label: t('admin.accounts.status.error') }, { value: 'rate_limited', label: t('admin.accounts.status.rateLimited') }, { value: 'temp_unschedulable', label: t('admin.accounts.status.tempUnschedulable') }, { value: 'unschedulable', label: t('admin.accounts.status.unschedulable') }])
const privacyOpts = computed(() => [
  { value: '', label: t('admin.accounts.allPrivacyModes') },
  { value: '__unset__', label: t('admin.accounts.privacyUnset') },
  { value: 'training_off', label: 'Privacy' },
  { value: 'training_set_cf_blocked', label: 'CF' },
  { value: 'training_set_failed', label: 'Fail' }
])
const specialGroupOptions = computed(() => [
  { value: '', label: t('admin.accounts.allGroups') },
  { value: 'ungrouped', label: t('admin.accounts.ungroupedGroup') },
])
const gOpts = computed(() => [
  ...specialGroupOptions.value,
  ...buildAdminGroupSelectOptions(props.groups || [], g => ({ value: String(g.id), label: g.name }), t)
])
const defaultGroupLabel = computed(() => props.defaultGroup === 'ungrouped' ? t('admin.accounts.ungroupedGroup') : props.groups?.find(g => String(g.id) === props.defaultGroup)?.name || '#' + props.defaultGroup)
</script>
