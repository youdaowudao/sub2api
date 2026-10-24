<template>
  <section
    data-testid="fork-email-signup-filter"
    aria-labelledby="fork-email-signup-filter-title"
    :aria-busy="loading || saving"
    class="mt-4 space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
  >
    <div>
      <h4 id="fork-email-signup-filter-title" class="font-medium text-gray-900 dark:text-white">
        {{ t(`${ns}.title`) }}
      </h4>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.description`) }}</p>
    </div>

    <p v-if="loading" class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.loading`) }}</p>
    <p
      v-if="loadError"
      data-testid="fork-email-signup-filter-load-error"
      role="alert"
      class="text-sm text-red-600 dark:text-red-400"
    >{{ loadError }}</p>
    <p
      v-if="loaded && configInvalid"
      data-testid="fork-email-signup-filter-invalid-warning"
      role="alert"
      class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
    >{{ t(`${ns}.invalidWarning`) }}</p>

    <div class="flex items-center justify-between gap-4">
      <div>
        <div class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t(`${ns}.enabled`) }}</div>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.enabledHint`) }}</p>
      </div>
      <Toggle
        v-model="form.enabled"
        data-testid="fork-email-signup-filter-enabled"
        :aria-label="t(`${ns}.enabled`)"
        :disabled="controlsDisabled"
      />
    </div>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.aliasHint`) }}</p>

    <div>
      <label for="fork-email-signup-filter-domains" class="input-label">{{ t(`${ns}.trustedDomains`) }}</label>
      <textarea
        id="fork-email-signup-filter-domains"
        v-model="form.domainText"
        data-testid="fork-email-signup-filter-domains"
        rows="4"
        class="input w-full"
        :disabled="controlsDisabled"
      />
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.trustedDomainsHint`) }}</p>
    </div>

    <div class="flex items-center justify-between gap-4">
      <div>
        <div class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t(`${ns}.timeWindowEnabled`) }}</div>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.timeWindowHint`) }}</p>
      </div>
      <Toggle
        v-model="form.timeWindowEnabled"
        data-testid="fork-email-signup-filter-time-enabled"
        :aria-label="t(`${ns}.timeWindowEnabled`)"
        :disabled="controlsDisabled"
      />
    </div>
    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div>
        <label for="fork-email-signup-filter-start" class="input-label">{{ t(`${ns}.windowStart`) }}</label>
        <input
          id="fork-email-signup-filter-start"
          v-model="form.windowStart"
          data-testid="fork-email-signup-filter-start"
          type="time"
          step="60"
          class="input"
          :disabled="controlsDisabled"
        />
      </div>
      <div>
        <label for="fork-email-signup-filter-end" class="input-label">{{ t(`${ns}.windowEnd`) }}</label>
        <input
          id="fork-email-signup-filter-end"
          v-model="form.windowEnd"
          data-testid="fork-email-signup-filter-end"
          type="time"
          step="60"
          class="input"
          :disabled="controlsDisabled"
        />
      </div>
    </div>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.windowHint`, { timezone: serverTimezone }) }}</p>

    <div class="flex flex-wrap items-center gap-3">
      <button
        type="button"
        data-testid="fork-email-signup-filter-save"
        class="btn btn-primary btn-sm"
        :disabled="controlsDisabled"
        @click="save"
      >{{ saving ? t('common.saving') : t(`${ns}.save`) }}</button>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.saveHint`) }}</p>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <button
        type="button"
        data-testid="fork-email-signup-filter-reload"
        class="btn btn-secondary btn-sm"
        :disabled="loading || saving"
        @click="load"
      >{{ t(`${ns}.reload`) }}</button>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.reloadHint`) }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getForkEmailSignupFilter, updateForkEmailSignupFilter } from '@/api/admin/forkEmailSignupFilter'
import type { ForkEmailSignupFilterResponse } from '@/api/admin/forkEmailSignupFilter'
import { useAppStore } from '@/stores'
import { extractI18nErrorMessage } from '@/utils/apiError'

const ns = 'admin.forkEmailSignupFilter'
const { t } = useI18n()
const appStore = useAppStore()
const form = reactive({ enabled: false, domainText: '', timeWindowEnabled: false, windowStart: '09:00', windowEnd: '18:00' })
const loading = ref(true)
const saving = ref(false)
const loaded = ref(false)
const loadError = ref('')
const configInvalid = ref(false)
const serverTimezone = ref('')
const controlsDisabled = computed(() => loading.value || saving.value || !loaded.value)

function applyResponse(data: ForkEmailSignupFilterResponse) {
  form.enabled = data.enabled
  form.domainText = data.trusted_domains.join('\n')
  form.timeWindowEnabled = data.time_window_enabled
  form.windowStart = data.window_start
  form.windowEnd = data.window_end
  serverTimezone.value = data.server_timezone
  configInvalid.value = data.config_invalid
}

function errorReason(error: unknown): string {
  return extractI18nErrorMessage(error, t, `${ns}.errors`, t('common.unknownError'))
}

async function load() {
  if (saving.value) return
  loading.value = true
  loaded.value = false
  loadError.value = ''
  try {
    applyResponse(await getForkEmailSignupFilter())
    loaded.value = true
  } catch (error) {
    loadError.value = t(`${ns}.loadFailed`, { reason: errorReason(error) })
  } finally {
    loading.value = false
  }
}

async function save() {
  if (controlsDisabled.value) return
  saving.value = true
  try {
    const data = await updateForkEmailSignupFilter({
      enabled: form.enabled,
      trusted_domains: form.domainText.split(/\r?\n/),
      time_window_enabled: form.timeWindowEnabled,
      window_start: form.windowStart,
      window_end: form.windowEnd,
    })
    applyResponse(data)
    appStore.showSuccess(t(`${ns}.saved`))
  } catch (error) {
    const status = (error as { status?: number } | null)?.status
    appStore.showError(t(`${ns}.${status === 400 ? 'invalidSave' : 'uncertainSave'}`, { reason: errorReason(error) }))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
