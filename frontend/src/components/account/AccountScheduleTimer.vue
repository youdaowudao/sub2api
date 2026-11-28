<template>
  <div class="flex max-w-48 flex-col gap-1" :title="disabledReason">
    <Select
      :model-value="null"
      :options="options"
      :disabled="busy || disabled || !account.schedulable || !!account.schedule_resume_at"
      :searchable="false"
      :aria-label="t('admin.accounts.scheduleTimer.button')"
      @change="start"
    >
      <template #selected>{{ t('admin.accounts.scheduleTimer.button') }}</template>
    </Select>
    <span v-if="account.schedule_resume_at" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.scheduleTimer.resumeAt', { time: formatDateTime(account.schedule_resume_at) }) }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ account: Account; disabled?: boolean; busy?: boolean }>()
const emit = defineEmits<{ start: [hours: number] }>()
const { t } = useI18n()
const options = computed(() => [1, 2, 3, 6, 12, 24].map(hours => ({
  value: hours,
  label: t('admin.accounts.scheduleTimer.hours', { hours })
})))
const disabledReason = computed(() => props.account.schedule_resume_at
  ? t('admin.accounts.scheduleTimer.resumeAt', { time: formatDateTime(props.account.schedule_resume_at) })
  : !props.account.schedulable ? t('admin.accounts.scheduleTimer.alreadyClosed') : undefined)

function start(hours: string | number | boolean | null) {
  if (props.busy || props.disabled || !props.account.schedulable || props.account.schedule_resume_at ||
      typeof hours !== 'number' || ![1, 2, 3, 6, 12, 24].includes(hours)) return
  emit('start', hours)
}
</script>
