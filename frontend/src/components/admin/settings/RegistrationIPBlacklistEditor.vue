<template>
  <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <label class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.registration.ipBlacklist.title') }}</label>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.registration.ipBlacklist.hint') }}</p>
      </div>
      <button type="button" class="btn btn-primary btn-sm shrink-0" data-testid="ip-blacklist-edit" @click="openEditor">
        {{ t('admin.settings.registration.ipBlacklist.edit') }}
      </button>
    </div>

    <BaseDialog :show="editorOpen" :title="t('admin.settings.registration.ipBlacklist.editorTitle')" width="wide"
      :close-on-escape="!saving && !loading" :close-on-click-outside="!saving && !loading" @close="closeEditor">
      <div class="space-y-3">
        <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <p v-if="notice" class="break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ notice }}</p>
        <p v-if="unconfirmedSaveError" class="break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ unconfirmedSaveError }} {{ t('admin.settings.registration.ipBlacklist.unknownSave') }}</p>
        <p v-if="invalidStored" class="text-sm text-amber-600 dark:text-amber-400">{{ t('admin.settings.registration.ipBlacklist.invalidStored') }}</p>
        <label for="registration-ip-blacklist" class="input-label">{{ t('admin.settings.registration.ipBlacklist.label') }}</label>
        <textarea id="registration-ip-blacklist" v-model="draft" rows="7" class="input w-full font-mono text-sm"
          :disabled="!loaded || loading || saving" data-testid="ip-blacklist-draft" />
      </div>
      <template #footer>
        <div class="flex flex-wrap justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || saving" data-testid="ip-blacklist-reload" @click="load">
            {{ t('admin.settings.registration.ipBlacklist.reload') }}
          </button>
          <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="closeEditor">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!loaded || loading || saving || !!unconfirmedSaveError" data-testid="ip-blacklist-save" @click="save">
            {{ t('admin.settings.registration.ipBlacklist.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const editorOpen = ref(false)
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const invalidStored = ref(false)
const unconfirmedSaveError = ref('')
const draft = ref('')
const notice = ref('')

async function load() {
  if (loading.value || saving.value) return
  loading.value = true
  try {
    const { ips } = await adminAPI.settings.getRegistrationIPBlacklist()
    invalidStored.value = ips === null
    draft.value = (ips ?? []).join('\n')
    loaded.value = true
    unconfirmedSaveError.value = ''
    notice.value = ''
  } catch (error) {
    const message = extractApiErrorMessage(error, t('common.error'))
    notice.value = t('admin.settings.registration.ipBlacklist.loadFailed') + ' ' + message
  } finally {
    loading.value = false
  }
}

function openEditor() {
  if (editorOpen.value) return
  loaded.value = false
  invalidStored.value = false
  unconfirmedSaveError.value = ''
  draft.value = ''
  notice.value = ''
  editorOpen.value = true
  void load()
}

function closeEditor() {
  if (!loading.value && !saving.value) editorOpen.value = false
}

async function save() {
  if (!loaded.value || loading.value || saving.value || unconfirmedSaveError.value) return
  saving.value = true
  notice.value = ''
  try {
    const ips = draft.value.split(/\r?\n/).map(ip => ip.trim()).filter(Boolean)
    const result = await adminAPI.settings.updateRegistrationIPBlacklist(ips)
    draft.value = result.ips.join('\n')
    invalidStored.value = false
    editorOpen.value = false
    appStore.showSuccess(t('admin.settings.registration.ipBlacklist.saved'))
  } catch (error) {
    const message = extractApiErrorMessage(error, t('common.error'))
    if (extractApiErrorCode(error) === 'REGISTRATION_IP_BLACKLIST_INVALID') {
      notice.value = t('admin.settings.registration.ipBlacklist.invalidInput') + ' ' + message
    } else {
      unconfirmedSaveError.value = message
    }
  } finally {
    saving.value = false
  }
}
</script>
