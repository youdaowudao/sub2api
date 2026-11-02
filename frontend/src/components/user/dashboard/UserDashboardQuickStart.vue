<template>
  <!-- 收起：一条 QQ 群 + 文档 + 展开 -->
  <div v-if="!expanded" class="card flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3" data-testid="quick-start-collapsed">
    <img :src="qqGroup.penguinImage" alt="" class="h-6 w-auto" />
    <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('community.qqGroup') }}</span>
    <span class="text-base font-bold tabular-nums text-gray-900 dark:text-white">{{ qqGroup.number }}</span>
    <button type="button" class="btn btn-secondary btn-sm hidden sm:inline-flex" data-testid="quick-start-copy-qq" @click="copyQq">{{ t('community.copyNumber') }}</button>
    <button type="button" class="btn btn-secondary btn-sm hidden sm:inline-flex" @click="showQr = true">{{ t('community.scanToJoin') }}</button>
    <a :href="qqGroup.link" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm sm:hidden">{{ t('community.joinGroup') }}</a>
    <a :href="quickConnect.docsHome" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-primary-600 dark:text-dark-300">
      <Icon name="book" size="sm" />{{ t('dashboard.quickStart.tutorial') }}
    </a>
    <button type="button" class="ml-auto inline-flex items-center gap-1 text-sm text-primary-600 dark:text-primary-400" data-testid="quick-start-expand" @click="expanded = true">
      {{ t('dashboard.quickStart.expand') }}<Icon name="chevronDown" size="sm" />
    </button>
    <QqGroupDialog :show="showQr" @close="showQr = false" />
  </div>

  <section v-else class="card overflow-hidden" data-testid="quick-start">
    <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-dark-700">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('dashboard.quickStart.title') }}</h2>
      <button type="button" class="text-sm text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200" data-testid="quick-start-collapse" @click="expanded = false">
        {{ t('dashboard.quickStart.collapse') }}
      </button>
    </div>

    <ol class="grid grid-cols-1 gap-px bg-gray-100 dark:bg-dark-700 md:grid-cols-2 xl:grid-cols-[0.95fr_0.95fr_1.5fr_1fr]">
      <!-- ① 创建密钥 -->
      <li class="step" data-testid="quick-start-step-1">
        <div class="step-inner">
          <StepHead :index="1" :done="hasKey" :title="t('dashboard.quickStart.step1')" />
          <p class="step-text">{{ hasKey ? t('dashboard.quickStart.step1Tip') : t('dashboard.quickStart.step1Todo') }}</p>
          <div class="step-foot">
            <router-link v-if="hasKey" to="/keys" class="step-link">{{ t('dashboard.quickStart.step1Manage') }} →</router-link>
            <router-link v-else to="/keys" class="btn btn-primary btn-sm">{{ t('dashboard.quickStart.step1Action') }}</router-link>
          </div>
        </div>
      </li>

      <!-- ② 充值 -->
      <li class="step" data-testid="quick-start-step-2">
        <div class="step-inner">
          <StepHead :index="2" :done="hasBalance" :title="t('dashboard.quickStart.step2')" />
          <div class="step-body">
            <p class="step-label">{{ t('dashboard.quickStart.step2Label') }}</p>
            <p class="step-value tabular-nums" :class="hasBalance ? 'text-emerald-600 dark:text-emerald-400' : ''" data-testid="quick-start-balance">${{ balance.toFixed(2) }}</p>
          </div>
          <div class="step-foot flex-wrap gap-x-3 gap-y-2">
            <router-link :to="rechargePath" class="btn btn-sm" :class="hasBalance ? 'btn-secondary' : 'btn-primary'" data-testid="quick-start-recharge">{{ t('dashboard.quickStart.step2Action') }}</router-link>
            <button type="button" class="inline-flex items-center gap-1 whitespace-nowrap text-sm font-medium text-primary-600 hover:underline dark:text-primary-400" data-testid="quick-start-reward" @click="showQr = true">
              <img :src="qqGroup.penguinImage" alt="" class="h-4 w-auto" />{{ t('dashboard.quickStart.step2Reward') }}
            </button>
          </div>
        </div>
      </li>

      <!-- ③ 连接 Codex：出现使用记录就算连接成功 -->
      <li class="step" data-testid="quick-start-step-3">
        <div class="step-inner">
          <StepHead :index="3" :done="hasUsage" :title="t('dashboard.quickStart.step3')" />
          <div class="mt-3 flex flex-col gap-2.5">
            <div class="flex items-center gap-3 whitespace-nowrap">
              <a :href="quickConnect.downloads[os]" class="btn btn-secondary btn-sm whitespace-nowrap" data-testid="quick-start-download">
                <Icon name="download" size="sm" class="mr-1" />{{ t('dashboard.quickStart.step3Download', { os: osLabel[os] }) }}
              </a>
              <a :href="quickConnect.downloads[otherOs]" class="text-xs text-gray-500 hover:text-primary-600 hover:underline dark:text-dark-400" data-testid="quick-start-download-other">
                {{ t('dashboard.quickStart.step3OtherOs', { os: osLabel[otherOs] }) }}
              </a>
            </div>

            <div class="flex items-center gap-2 whitespace-nowrap">
              <div class="relative flex-none">
              <select v-model="selectedKeyId" class="h-[30px] w-[8.5rem] cursor-pointer appearance-none truncate rounded-lg border border-gray-200 bg-white py-0 pl-2.5 pr-6 text-sm text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100" :aria-label="t('dashboard.quickStart.step3KeyLabel')" data-testid="quick-start-key-select">
                <option v-if="!openaiKeys.length" :value="null">{{ t('dashboard.quickStart.step3NoKey') }}</option>
                <option v-for="k in openaiKeys" :key="k.id" :value="k.id" :title="k.name">{{ k.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-gray-400" />
            </div>
              <!-- 还没有 OpenAI 密钥时按钮照常显示，点了去创建密钥 -->
              <CodexImportButton :api-key="selectedKey?.key" :name="selectedKey?.name" compact data-testid="quick-start-import" @missing-key="router.push('/keys')" />
            </div>
          </div>
          <p class="mt-2.5 text-xs text-gray-400 dark:text-dark-500">{{ t('dashboard.quickStart.step3Hint') }}</p>
          <div class="step-foot">
            <a :href="quickConnect.fallbackDoc" target="_blank" rel="noopener noreferrer" class="step-link text-gray-500 dark:text-dark-400" data-testid="quick-start-fallback">
              {{ t(os === 'mac' ? 'dashboard.quickStart.step3NoResponseMac' : 'dashboard.quickStart.step3NoResponse') }} →
            </a>
          </div>
        </div>
      </li>

      <!-- ④ 发送第一条消息 -->
      <li class="step" data-testid="quick-start-step-4">
        <div class="step-inner">
          <StepHead :index="4" :done="hasUsage" :title="t('dashboard.quickStart.step4')" />
          <p class="step-text">{{ t('dashboard.quickStart.step4Hint') }}</p>
          <div class="step-foot">
            <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-sm" :class="hasUsage ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'">
              <span class="h-2 w-2 rounded-full" :class="hasUsage ? 'bg-emerald-500' : 'animate-pulse bg-gray-400 dark:bg-dark-500'"></span>
              {{ hasUsage ? t('dashboard.quickStart.step4Done') : t('dashboard.quickStart.step4Waiting') }}
            </span>
          </div>
        </div>
      </li>
    </ol>

    <!-- 底部 QQ 群：电脑点群号复制、扫码；手机整行跳转加群 -->
    <div class="border-t border-gray-100 bg-gray-50/70 px-5 py-4 dark:border-dark-700 dark:bg-dark-800/40">
      <a :href="qqGroup.link" target="_blank" rel="noopener noreferrer" class="flex items-center gap-3 sm:hidden" data-testid="quick-start-qq-mobile">
        <img :src="qqGroup.penguinImage" alt="" class="h-8 w-auto" />
        <span class="flex min-w-0 flex-col">
          <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">{{ t('community.qqGroup') }}</span>
          <span class="whitespace-nowrap text-xl font-extrabold tabular-nums text-gray-900 dark:text-white">{{ qqGroup.number }}</span>
        </span>
        <span class="btn btn-primary btn-sm ml-auto whitespace-nowrap">{{ t('dashboard.quickStart.qqTap') }}</span>
      </a>
      <div class="hidden items-center gap-4 sm:flex">
        <img :src="qqGroup.penguinImage" alt="" class="h-9 w-auto" />
        <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('community.qqGroup') }}</span>
        <button type="button" class="group inline-flex items-baseline gap-2" :title="t('dashboard.quickStart.qqCopyHint')" data-testid="quick-start-qq-copy" @click="copyQq">
          <span class="text-2xl font-extrabold tabular-nums tracking-wide text-gray-900 group-hover:text-primary-600 dark:text-white">{{ qqGroup.number }}</span>
          <Icon name="copy" size="sm" class="text-gray-400 group-hover:text-primary-600" />
        </button>
        <span class="text-sm text-amber-600 dark:text-amber-400">{{ t('community.newcomerReward') }}</span>
        <div class="ml-auto flex items-center gap-2">
          <a :href="quickConnect.docsHome" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm" data-testid="quick-start-tutorial">
            <Icon name="book" size="sm" class="mr-1" />{{ t('dashboard.quickStart.tutorial') }}
          </a>
          <button type="button" class="btn btn-primary btn-sm" data-testid="quick-start-qq-scan" @click="showQr = true">{{ t('community.scanToJoin') }}</button>
        </div>
      </div>
    </div>
    <QqGroupDialog :show="showQr" @close="showQr = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import QqGroupDialog from '@/features/brand/components/QqGroupDialog.vue'
import { qqGroup, quickConnect } from '@/features/brand/config'
import CodexImportButton from '@/features/brand/components/CodexImportButton.vue'
import { keysAPI } from '@/api/keys'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import type { ApiKey } from '@/types'

const props = defineProps<{
  totalApiKeys: number
  totalRequests: number
  balance: number
}>()

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

/** 使用记录不超过设定条数时一直展开；手动收起只对本次访问有效 */
const expanded = ref(props.totalRequests <= quickConnect.collapseAfterRequests)
const showQr = ref(false)

const hasKey = computed(() => props.totalApiKeys > 0)
const hasBalance = computed(() => props.balance > 0)
const hasUsage = computed(() => props.totalRequests > 0)

const rechargePath = computed(() => (appStore.cachedPublicSettings?.payment_enabled === false ? '/redeem' : '/purchase'))

type Os = 'windows' | 'mac'
const os: Os = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/i.test(navigator.userAgent) ? 'mac' : 'windows'
const otherOs: Os = os === 'mac' ? 'windows' : 'mac'
const osLabel: Record<Os, string> = { windows: 'Windows', mac: 'Mac' }

const keys = ref<ApiKey[]>([])
const selectedKeyId = ref<number | null>(null)
const byNewest = (a: ApiKey, b: ApiKey) => (b.created_at || '').localeCompare(a.created_at || '') || b.id - a.id
const openaiKeys = computed(() => keys.value.filter((k) => k.status === 'active' && k.group?.platform === 'openai').sort(byNewest))

onMounted(async () => {
  if (!hasKey.value) return
  try {
    const res = await keysAPI.list(1, 100)
    keys.value = res.items || []
    selectedKeyId.value = openaiKeys.value[0]?.id ?? null
  } catch {
    keys.value = []
  }
})

const selectedKey = computed(() => openaiKeys.value.find((k) => k.id === selectedKeyId.value) ?? openaiKeys.value[0] ?? null)

function copyQq() {
  void useClipboard().copyToClipboard(qqGroup.number, t('community.copied'))
}

const StepHead = defineComponent({
  props: { index: { type: Number, required: true }, done: Boolean, title: { type: String, required: true } },
  setup(p) {
    return () =>
      h('div', { class: 'flex items-center gap-2.5' }, [
        h(
          'span',
          {
            class: [
              'flex h-7 w-7 flex-none items-center justify-center rounded-full text-sm font-semibold',
              p.done ? 'bg-emerald-500 text-white' : 'bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300'
            ],
            'data-done': p.done ? 'true' : 'false'
          },
          p.done ? '✓' : String(p.index)
        ),
        h('h3', { class: 'text-sm font-semibold text-gray-900 dark:text-white' }, p.title)
      ])
  }
})
</script>

<style scoped>
.step {
  @apply flex min-w-0 justify-center bg-white px-5 py-4 dark:bg-dark-800;
}

/* 内容块在列里居中，块内文字靠左 */
.step-inner {
  @apply flex h-full w-fit min-w-0 max-w-full flex-col;
}

.step-text {
  @apply mt-3 text-sm leading-relaxed text-gray-500 dark:text-dark-400;
}

.step-body {
  @apply mt-3 min-w-0;
}

.step-label {
  @apply text-xs text-gray-400 dark:text-dark-500;
}

.step-value {
  @apply mt-0.5 text-2xl font-bold text-gray-900 dark:text-white;
}

.step-foot {
  @apply mt-auto flex items-center pt-4;
}

.step-link {
  @apply text-sm font-medium text-primary-600 hover:underline dark:text-primary-400;
}
</style>
