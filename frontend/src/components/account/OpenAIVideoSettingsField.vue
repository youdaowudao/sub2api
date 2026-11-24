<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { OPENAI_VIDEO_MAX_SECONDS, OPENAI_VIDEO_RESOLUTIONS, type OpenAIVideoSettings } from './openAIVideoSettings'

const settings = defineModel<OpenAIVideoSettings>({ required: true })
const { t } = useI18n()
const secondsFields = [
  { field: 'min_seconds', name: 'min', label: 'admin.accounts.openai.videoMinSeconds' },
  { field: 'default_seconds', name: 'default', label: 'admin.accounts.openai.videoDefaultSeconds' },
  { field: 'max_seconds', name: 'max', label: 'admin.accounts.openai.videoMaxSeconds' },
] as const
</script>

<template>
  <div class="space-y-3">
    <fieldset>
      <legend class="input-label mb-2">{{ t('admin.accounts.openai.videoResolutions') }}</legend>
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
        <label
          v-for="resolution in OPENAI_VIDEO_RESOLUTIONS"
          :key="resolution"
          class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"
        >
          <input
            v-model="settings.resolutions"
            type="checkbox"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            :data-testid="`openai-video-resolution-${resolution}`"
            :value="resolution"
            :disabled="settings.resolutions.length === 1 && settings.resolutions[0] === resolution"
          />
          <span class="text-gray-700 dark:text-gray-200">{{ resolution }}</span>
        </label>
      </div>
    </fieldset>
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
      <label v-for="item in secondsFields" :key="item.field" class="block">
        <span class="input-label">{{ t(item.label) }}</span>
        <input
          v-model.number="settings[item.field]"
          type="number"
          class="input"
          :data-testid="`openai-video-${item.name}-seconds`"
          min="1"
          :max="OPENAI_VIDEO_MAX_SECONDS"
          step="1"
          required
        />
      </label>
    </div>
    <div
      class="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
      data-testid="openai-video-requirements"
    >
      <p class="font-medium">{{ t('admin.accounts.openai.videoRequirementsTitle') }}</p>
      <ol class="mt-1 list-decimal space-y-1 ps-4">
        <li>{{ t('admin.accounts.openai.videoRequirementsKey') }}</li>
        <li>{{ t('admin.accounts.openai.videoRequirementsQueue') }}</li>
        <li>{{ t('admin.accounts.openai.videoRequirementsRejection') }}</li>
        <li>{{ t('admin.accounts.openai.videoRequirementsRefund') }}</li>
      </ol>
    </div>
  </div>
</template>
