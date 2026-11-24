<template>
  <section class="card" aria-labelledby="user-account-block-title">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="user-account-block-title" class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t(`${ns}.title`) }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.description`) }}</p>
    </div>

    <div class="space-y-4 px-6 py-4">
      <div class="max-w-sm">
        <label for="user-account-block-duration" class="input-label">{{ t(`${ns}.duration`) }}</label>
        <Select
          id="user-account-block-duration"
          :model-value="duration"
          :options="durationOptions"
          :aria-label="t(`${ns}.duration`)"
          @update:model-value="setDuration"
        />
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.durationHint`) }}</p>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="font-medium text-gray-900 dark:text-white">{{ t(`${ns}.listTitle`) }}</h3>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">
          {{ t(`${ns}.${loadError ? 'retry' : 'refresh'}`) }}
        </button>
      </div>

      <div :aria-busy="busy">
        <p v-if="loadError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
        <p v-else-if="busy && !loaded" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
        <p v-else-if="loaded && items.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t(`${ns}.empty`) }}</p>
        <div v-else-if="items.length" class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-left text-sm dark:divide-dark-700">
            <thead class="text-gray-500 dark:text-gray-400">
              <tr>
                <th v-for="column in columns" :key="column" scope="col" class="whitespace-nowrap px-2 py-2 font-medium">
                  {{ t(`${ns}.${column}`) }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 text-gray-900 dark:divide-dark-700 dark:text-white">
              <tr v-for="item in items" :key="`${item.user_id}:${item.account_id}`">
                <td class="whitespace-nowrap px-2 py-2">#{{ item.user_id }}</td>
                <td class="whitespace-nowrap px-2 py-2">#{{ item.account_id }}</td>
                <td class="whitespace-nowrap px-2 py-2">{{ formatDateTime(item.created_at) }}</td>
                <td class="whitespace-nowrap px-2 py-2">{{ formatDateTime(item.expires_at) }}</td>
                <td class="whitespace-nowrap px-2 py-2">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="remove(item)">
                    {{ t(`${ns}.remove`) }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { deleteUserAccountBlock, listUserAccountBlocks, type UserAccountBlockItem } from '@/api/admin/settings'
import Select from '@/components/common/Select.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import { blockWriteErrorMessage } from './userAccountBlockErrors'

const props = defineProps<{ active: boolean }>()
const duration = defineModel<number>({ required: true })
const { t } = useI18n()
const appStore = useAppStore()
const ns = 'admin.settings.userAccountBlock'
const columns = ['user', 'account', 'createdAt', 'expiresAt', 'action']
const durationOptions = computed(() => [6, 12].map(value => ({ value, label: t(`${ns}.hours`, { n: value }) })))

const busy = ref(false)
const loaded = ref(false)
const loadError = ref('')
const items = ref<UserAccountBlockItem[]>([])
let reloadOnEntry = false

function setDuration(value: string | number | boolean | null) {
  if (value === 6 || value === 12) duration.value = value
}

function finishRequest() {
  busy.value = false
  if (reloadOnEntry && props.active) void load()
}

async function load() {
  if (busy.value) return
  reloadOnEntry = false
  busy.value = true
  loadError.value = ''
  try {
    items.value = await listUserAccountBlocks()
    loaded.value = true
  } catch (error) {
    loadError.value = t(`${ns}.errors.${extractApiErrorCode(error) === 'RATE_LIMITED' ? 'readRateLimit' : 'loadFailed'}`)
  } finally {
    finishRequest()
  }
}

async function remove(item: UserAccountBlockItem) {
  if (busy.value) return
  busy.value = true
  try {
    await deleteUserAccountBlock(item.user_id, item.account_id)
    items.value = items.value.filter(existing => existing.user_id !== item.user_id || existing.account_id !== item.account_id)
    appStore.showSuccess(t(`${ns}.removed`))
  } catch (error) {
    const message = blockWriteErrorMessage(error, t, `${ns}.removeFailed`)
    if (message) appStore.showError(message)
  } finally {
    finishRequest()
  }
}

watch(() => props.active, (active, _, onCleanup) => {
  if (!active) return
  if (busy.value) {
    reloadOnEntry = true
    onCleanup(() => { reloadOnEntry = false })
  } else {
    void load()
  }
}, { immediate: true })
</script>
