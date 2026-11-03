<template>
  <!-- 收起：一条 QQ 群 + 文档 + 展开 -->
  <div v-if="!expanded" class="card flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3" data-testid="quick-start-collapsed">
    <img :src="qqGroup.penguinImage" alt="" class="h-6 w-auto" />
    <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('community.qqGroup') }}</span>
    <span class="text-base font-bold tabular-nums text-gray-900 dark:text-white">{{ qqGroup.number }}</span>
    <button type="button" class="btn btn-secondary hidden sm:inline-flex" data-testid="quick-start-copy-qq" @click="copyQq">{{ t('community.copyNumber') }}</button>
    <button type="button" class="btn btn-primary hidden sm:inline-flex" @click="showQr = true">{{ t('community.scanToJoin') }}</button>
    <a :href="qqGroup.link" target="_blank" rel="noopener noreferrer" class="btn btn-primary sm:hidden">{{ t('community.joinGroup') }}</a>
    <a :href="quickConnect.docsHome" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-primary-600 dark:text-dark-300">
      <Icon name="book" size="sm" />{{ t('dashboard.quickStart.tutorial') }}
    </a>
    <!-- 右侧空白放轮换小提示：大屏同一行靠右，小屏单独占最后一行 -->
    <p class="order-last flex w-full min-w-0 items-center gap-1.5 text-sm text-gray-600 dark:text-dark-300 md:order-none md:w-auto md:flex-1 md:justify-end" :data-tip="tip.id" data-testid="quick-start-tip">
      <Icon name="lightbulb" size="sm" class="flex-none text-amber-500" />
      <span class="flex-none font-medium text-amber-600 dark:text-amber-400">{{ t('dashboard.quickStart.tipsLabel') }}</span>
      <span class="min-w-0 md:truncate" :title="t(`dashboard.quickStart.tips.${tip.id}`)">{{ t(`dashboard.quickStart.tips.${tip.id}`) }}</span>
      <router-link v-if="'to' in tip" :to="tip.to" class="flex-none whitespace-nowrap font-medium text-primary-600 hover:underline dark:text-primary-400" data-testid="quick-start-tip-action">{{ t(`dashboard.quickStart.tipActions.${tip.id}`) }} →</router-link>
      <button v-else-if="'qr' in tip" type="button" class="flex-none whitespace-nowrap font-medium text-primary-600 hover:underline dark:text-primary-400" data-testid="quick-start-tip-action" @click="showQr = true">{{ t(`dashboard.quickStart.tipActions.${tip.id}`) }} →</button>
    </p>
    <button type="button" class="ml-auto inline-flex items-center gap-1 text-sm text-primary-600 dark:text-primary-400" data-testid="quick-start-expand" @click="expanded = true">
      {{ t('dashboard.quickStart.expand') }}<Icon name="chevronDown" size="sm" />
    </button>
    <QqGroupDialog :show="showQr" @close="showQr = false" />
  </div>

  <section v-else class="card overflow-hidden" data-testid="quick-start">
    <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-dark-700">
      <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('dashboard.quickStart.title') }}</h2>
        <span class="tagline" data-testid="quick-start-tagline">
          <Icon name="bolt" size="sm" class="tagline-icon" />
          <span class="tagline-text">{{ t('dashboard.quickStart.tagline') }}</span>
        </span>
      </div>
      <!-- 标题行右侧：劝留文案，点了弹出扫码加群；窄屏放不下就不显示 -->
      <button type="button" class="header-help ml-auto hidden lg:inline-flex" data-testid="quick-start-header-help" @click="showQr = true">
        <img :src="qqGroup.penguinImage" alt="" class="h-4 w-auto" />{{ t('dashboard.quickStart.headerHelp') }}
      </button>
      <button type="button" class="ml-4 flex-none text-sm text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200" data-testid="quick-start-collapse" @click="expanded = false">
        {{ t('dashboard.quickStart.collapse') }}
      </button>
    </div>

    <ol class="grid grid-cols-1 gap-px bg-gray-100 dark:bg-dark-700 md:grid-cols-2 xl:grid-cols-[0.95fr_0.95fr_1.5fr_1fr]">
      <!-- ① 创建密钥 -->
      <li class="step" data-testid="quick-start-step-1">
        <div class="step-inner">
          <StepHead :index="1" :done="hasKey" :title="t('dashboard.quickStart.step1')" />
          <p class="step-text">{{ hasKey ? t('dashboard.quickStart.step1Tip') : t('dashboard.quickStart.step1Todo') }}</p>
          <div class="step-foot flex-wrap gap-x-3 gap-y-2">
            <router-link v-if="hasKey" to="/keys" class="step-link">{{ t('dashboard.quickStart.step1Manage') }} →</router-link>
            <router-link v-else to="/keys" class="btn btn-primary">{{ t('dashboard.quickStart.step1Action') }}</router-link>
            <button type="button" class="btn btn-secondary whitespace-nowrap" data-testid="quick-start-key-guide" @click="showKeyGuide = true">
              <Icon name="eye" size="sm" />{{ t('dashboard.quickStart.step1Guide') }}
            </button>
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
            <router-link :to="rechargePath" class="btn" :class="hasBalance ? 'btn-secondary' : 'btn-primary'" data-testid="quick-start-recharge">{{ t('dashboard.quickStart.step2Action') }}</router-link>
            <button type="button" class="inline-flex items-center gap-1 whitespace-nowrap text-sm font-medium text-primary-600 hover:underline dark:text-primary-400" data-testid="quick-start-reward" @click="showQr = true">
              <img :src="qqGroup.penguinImage" alt="" class="h-4 w-auto" />{{ t('dashboard.quickStart.step2Reward') }}
            </button>
          </div>
        </div>
      </li>

      <!-- ③ 连接 Codex：出现使用记录就算连接成功 -->
      <li class="step" data-testid="quick-start-step-3">
        <div class="step-inner">
          <StepHead :index="3" :done="hasUsage" :title="t('dashboard.quickStart.step3', { app: appLabel })" :badge="t('dashboard.quickStart.step3Badge')" />
          <div class="mt-3 flex flex-col gap-2.5">
            <div class="flex items-center gap-3 whitespace-nowrap">
              <a :href="quickConnect.downloads[os]" class="download-btn" data-testid="quick-start-download">
                <Icon name="download" size="sm" class="mr-1" />{{ t('dashboard.quickStart.step3Download', { os: osLabel[os] }) }}
              </a>
              <a :href="quickConnect.downloads[otherOs]" class="text-xs text-gray-500 hover:text-primary-600 hover:underline dark:text-dark-400" data-testid="quick-start-download-other">
                {{ t('dashboard.quickStart.step3OtherOs', { os: osLabel[otherOs] }) }}
              </a>
            </div>

            <div class="flex items-center gap-2 whitespace-nowrap">
              <div class="relative flex-none">
              <select v-model="selectedKeyId" class="h-9 w-[8.5rem] cursor-pointer appearance-none truncate rounded-lg border border-gray-200 bg-white py-0 pl-2.5 pr-6 text-sm text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100" :aria-label="t('dashboard.quickStart.step3KeyLabel')" data-testid="quick-start-key-select">
                <option v-if="!importKeys.length" :value="null">{{ t('dashboard.quickStart.step3NoKey') }}</option>
                <option v-for="k in importKeys" :key="k.id" :value="k.id" :title="k.name">{{ k.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-gray-400" />
            </div>
              <!-- 还没有可导入的密钥时按钮照常显示，点了去创建密钥；按选中密钥的分组切换 Codex / Claude -->
              <CodexImportButton :api-key="selectedKey?.key" :name="selectedKey?.name" :app="importApp" data-testid="quick-start-import" @missing-key="router.push('/keys')" />
            </div>
          </div>
          <!-- w-0 min-w-full：说明文字不撑宽这一列，跟随上面按钮的宽度换行 -->
          <p class="mt-2.5 w-0 min-w-full text-xs leading-relaxed text-gray-500 dark:text-dark-400" data-testid="quick-start-step3-hint">{{ t('dashboard.quickStart.step3Hint', { app: appLabel }) }}</p>
          <div class="step-foot-stack">
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
          <p class="step-text">{{ t(importApp === 'claude' ? 'dashboard.quickStart.step4HintClaude' : 'dashboard.quickStart.step4Hint') }}</p>
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
        <span class="btn btn-primary ml-auto whitespace-nowrap">{{ t('dashboard.quickStart.qqTap') }}</span>
      </a>
      <a :href="qqGroup.link" target="_blank" rel="noopener noreferrer" class="support-link mt-2.5 inline-flex !items-start sm:hidden" data-testid="quick-start-qq-help-mobile">
        <img :src="qqGroup.penguinImage" alt="" class="mt-0.5 h-4 w-auto flex-none" /><span class="whitespace-normal">{{ t('dashboard.quickStart.qqHelp') }}</span>
      </a>
      <div class="hidden flex-wrap items-center gap-x-4 gap-y-3 sm:flex">
        <img :src="qqGroup.penguinImage" alt="" class="h-9 w-auto" />
        <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('community.qqGroup') }}</span>
        <button type="button" class="group inline-flex items-baseline gap-2" :title="t('dashboard.quickStart.qqCopyHint')" data-testid="quick-start-qq-copy" @click="copyQq">
          <span class="text-2xl font-extrabold tabular-nums tracking-wide text-gray-900 group-hover:text-primary-600 dark:text-white">{{ qqGroup.number }}</span>
          <Icon name="copy" size="sm" class="text-gray-400 group-hover:text-primary-600" />
        </button>
        <!-- 扫码按钮紧跟群号，放在左边 -->
        <button type="button" class="btn btn-primary" data-testid="quick-start-qq-scan" @click="showQr = true">{{ t('community.scanToJoin') }}</button>
        <span class="flex flex-col gap-0.5 text-sm">
          <span class="text-gray-500 dark:text-dark-400">{{ t('community.newcomerReward') }}</span>
          <!-- 求助文案带小企鹅，点了弹出扫码加群 -->
          <button type="button" class="support-link inline-flex" data-testid="quick-start-qq-help" @click="showQr = true">
            <img :src="qqGroup.penguinImage" alt="" class="h-4 w-auto" />{{ t('dashboard.quickStart.qqHelp') }}
          </button>
        </span>
        <a :href="quickConnect.docsHome" target="_blank" rel="noopener noreferrer" class="btn btn-secondary ml-auto" data-testid="quick-start-tutorial">
          <Icon name="book" size="sm" />{{ t('dashboard.quickStart.tutorial') }}
        </a>
      </div>
    </div>
    <QqGroupDialog :show="showQr" @close="showQr = false" />
    <CreateKeyGuideDialog :show="showKeyGuide" @close="showKeyGuide = false" />
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import QqGroupDialog from '@/features/brand/components/QqGroupDialog.vue'
import { canQuickImport, currentTipIndex, importAppForGroup, qqGroup, quickConnect, quickStartTips } from '@/features/brand/config'
import CodexImportButton from '@/features/brand/components/CodexImportButton.vue'
import CreateKeyGuideDialog from './CreateKeyGuideDialog.vue'
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
const showKeyGuide = ref(false)
/** 收起横条的小提示：按时间每 5 小时轮换，打开页面时取一次 */
const tip = quickStartTips[currentTipIndex()]

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
/** 能一键导入的密钥：启用中的 OpenAI（导入 Codex）和 Anthropic（导入 Claude）分组密钥，最新的在前 */
const importKeys = computed(() => keys.value.filter((k) => k.status === 'active' && canQuickImport(k.group)).sort(byNewest))

onMounted(async () => {
  if (!hasKey.value) return
  try {
    const res = await keysAPI.list(1, 100)
    keys.value = res.items || []
    selectedKeyId.value = importKeys.value[0]?.id ?? null
  } catch {
    keys.value = []
  }
})

const selectedKey = computed(() => importKeys.value.find((k) => k.id === selectedKeyId.value) ?? importKeys.value[0] ?? null)
/** 第 3、4 步的软件名和导入目标跟着选中的密钥变 */
const importApp = computed(() => importAppForGroup(selectedKey.value?.group))
const appLabel = computed(() => (importApp.value === 'claude' ? 'Claude' : 'Codex'))

function copyQq() {
  void useClipboard().copyToClipboard(qqGroup.number, t('community.copied'))
}

const StepHead = defineComponent({
  props: { index: { type: Number, required: true }, done: Boolean, title: { type: String, required: true }, badge: { type: String, default: '' } },
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
        h('h3', { class: 'text-sm font-semibold text-gray-900 dark:text-white' }, p.title),
        // 这里由子组件渲染，父组件的 scoped 样式够不到，只能用工具类；
        // 用浅橙色描边小胶囊，和青蓝渐变的“一键导入”按钮区分开
        p.badge
          ? h(
              'span',
              {
                class:
                  'whitespace-nowrap rounded-full bg-orange-50 px-2 py-0.5 text-[11px] font-semibold leading-4 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/30',
                'data-testid': 'quick-start-step-badge'
              },
              p.badge
            )
          : null
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

.step-foot-stack {
  @apply mt-auto flex flex-col items-start gap-1.5 pt-4;
}

/* 下载助手：浅靛蓝底色，和青蓝渐变的“一键导入”按钮区分开 */
.download-btn {
  @apply inline-flex h-9 items-center whitespace-nowrap rounded-[10px] bg-indigo-50 px-3.5 text-sm font-medium text-indigo-700 ring-1 ring-inset ring-indigo-200 transition-colors hover:bg-indigo-100 dark:bg-indigo-500/10 dark:text-indigo-300 dark:ring-indigo-500/30 dark:hover:bg-indigo-500/20;
}

.support-link {
  @apply items-center gap-1 whitespace-nowrap text-sm font-medium text-amber-600 hover:underline dark:text-amber-400;
}

.header-help {
  @apply min-w-0 items-center gap-1.5 text-sm font-medium text-amber-600 hover:underline dark:text-amber-400;
}

/* 标题旁的卖点：渐变色与“一键导入”按钮一致，缓慢流光 */
.tagline {
  @apply inline-flex min-w-0 items-center gap-1 text-xs font-semibold sm:text-sm;
}

.tagline-icon {
  color: #0891b2;
  animation: tagline-pulse 2.4s ease-in-out infinite;
}

.tagline-text {
  background: linear-gradient(90deg, #0d9488 0%, #0891b2 30%, #2563eb 50%, #0891b2 70%, #0d9488 100%);
  background-size: 200% auto;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: tagline-shine 4s linear infinite;
}

.dark .tagline-icon {
  color: #22d3ee;
}

.dark .tagline-text {
  background-image: linear-gradient(90deg, #2dd4bf 0%, #22d3ee 30%, #60a5fa 50%, #22d3ee 70%, #2dd4bf 100%);
}

@keyframes tagline-shine {
  to {
    background-position: -200% center;
  }
}

@keyframes tagline-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.6;
    transform: scale(1.15);
  }
}

@media (prefers-reduced-motion: reduce) {
  .tagline-icon,
  .tagline-text {
    animation: none;
  }
}
</style>
