<template>
  <BaseDialog :show="true" :title="t(`${prefix}.addFromUser`)" width="narrow"
    :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <div class="space-y-3">
      <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <p class="break-all">{{ t('common.email') }}: {{ user.email }}</p>
        <p>{{ t(`${prefix}.accountCreatedAt`) }}: {{ formatDateTime(user.created_at) }}</p>
      </div>
      <p v-if="loading" class="text-sm text-gray-500" role="status">{{ t(`${prefix}.lookupLoading`) }}</p>
      <p v-if="notice" class="break-words text-sm text-red-600 dark:text-red-400" role="alert" data-test="registration-ip-error">{{ notice }}</p>
      <p v-if="!loading && !loadFailed && !candidates.length" class="text-sm text-gray-500" data-test="registration-ip-empty">{{ t(`${prefix}.noRegistrationIP`) }}</p>
      <fieldset v-if="candidates.length" class="space-y-2" :disabled="saving" data-test="registration-ip-candidates">
        <legend class="input-label">{{ t(`${prefix}.registrationIP`) }}</legend>
        <label v-for="candidate in candidates" :key="candidate.ip" class="flex items-start gap-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <input v-model="selectedIP" type="radio" :value="candidate.ip" name="registration-ip" class="mt-1" />
          <span class="min-w-0">
            <span class="block break-all font-mono text-sm text-gray-900 dark:text-white">{{ candidate.ip }}</span>
            <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.registrationRecordTime`) }}：{{ formatDateTime(candidate.createdAt) }}</span>
          </span>
        </label>
      </fieldset>
      <p v-if="candidates.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${prefix}.checkRegistrationRecord`) }}</p>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${prefix}.addEffect`) }}</p>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <button v-if="loadFailed" type="button" class="btn btn-secondary" :disabled="loading" @click="load">{{ t('common.tryAgain') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="saving" data-test="registration-ip-cancel" @click="close">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading || saving || loadFailed || !selectedIP" data-test="registration-ip-confirm" @click="confirm">{{ saving ? t('common.saving') : t(`${prefix}.confirmAdd`) }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import type { AuditLog } from '@/api/admin/audit'
import type { AdminUser } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ user: AdminUser }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const prefix = 'admin.settings.registration.ipBlacklist'
const candidates = ref<Array<{ ip: string; createdAt: string }>>([])
const selectedIP = ref('')
const loading = ref(false)
const saving = ref(false)
const loadFailed = ref(false)
const notice = ref('')
let disposed = false

// The server filters the registration request email exactly; list results
// omit the request body. Check the remaining record identity and status here.
function matchesUserRegistration(log: AuditLog, user: AdminUser): boolean {
  return log.action === 'auth.register' && log.method === 'POST' && log.path === '/api/v1/auth/register' &&
    log.status_code >= 200 && log.status_code < 300 && !!log.client_ip?.trim() &&
    (log.actor_user_id == null || log.actor_user_id === user.id) &&
    Date.parse(log.created_at) >= Date.parse(user.created_at)
}

async function load() {
  if (loading.value || saving.value) return
  loading.value = true
  loadFailed.value = false
  notice.value = ''
  candidates.value = []
  selectedIP.value = ''
  const found = new Map<string, string>()
  try {
    if (!props.user.email.trim() || !Number.isFinite(Date.parse(props.user.created_at))) return
    for (let page = 1; ; page++) {
      const result = await adminAPI.audit.list({
        registration_email: props.user.email, start_time: props.user.created_at,
        success: 'true', page, page_size: 200
      })
      if (disposed) return
      for (const log of result.items || []) {
        if (!matchesUserRegistration(log, props.user)) continue
        const address = log.client_ip.trim()
        if (!found.has(address)) found.set(address, log.created_at)
      }
      if (page >= result.pages) break
    }
    candidates.value = [...found].map(([ip, createdAt]) => ({ ip, createdAt }))
    if (candidates.value.length === 1) selectedIP.value = candidates.value[0].ip
  } catch (error) {
    if (disposed) return
    loadFailed.value = true
    notice.value = `${t(`${prefix}.lookupFailed`)} ${extractApiErrorMessage(error, t('common.error'))}`
  } finally {
    loading.value = false
  }
}

async function confirm() {
  if (loading.value || saving.value || loadFailed.value || !candidates.value.some(candidate => candidate.ip === selectedIP.value)) return
  saving.value = true
  notice.value = ''
  try {
    const result = await adminAPI.settings.appendRegistrationIPBlacklist(selectedIP.value)
    appStore.showSuccess(t(`${prefix}.${result.added ? 'addSuccess' : 'alreadyAdded'}`))
    emit('close')
  } catch (error) {
    const code = extractApiErrorCode(error)
    const message = extractApiErrorMessage(error, t('common.error'))
    const key = code === 'REGISTRATION_IP_BLACKLIST_CHANGED' ? 'appendConflict'
      : code === 'REGISTRATION_IP_BLACKLIST_INVALID' ? 'appendInvalid' : 'appendFailed'
    notice.value = `${t(`${prefix}.${key}`)} ${message}`
  } finally {
    saving.value = false
  }
}

function close() {
  if (!saving.value) emit('close')
}

// Mounted only after the administrator clicks the user action, never during
// user-list loading or when simply opening the More menu.
onMounted(load)
onUnmounted(() => { disposed = true })
</script>
