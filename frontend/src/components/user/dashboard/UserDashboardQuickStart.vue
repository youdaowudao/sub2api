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
    <div class="flex items-center justify-between px-5 pb-1 pt-4 sm:px-6">
      <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        <h2 class="text-lg font-bold text-gray-900 dark:text-white">{{ t('dashboard.quickStart.title') }}</h2>
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

    <!-- 四个步骤做成独立小卡片：已完成 / 当前 / 未开始三种状态一眼区分 -->
    <ol class="quick-start-steps grid grid-cols-1 gap-3 p-4 sm:gap-4 sm:p-5 md:grid-cols-2 xl:grid-cols-[1fr_1fr_1.45fr_1fr]">
      <!-- ① 创建密钥 -->
      <li class="step" :class="stepClass(1, hasKey)" data-testid="quick-start-step-1">
        <div class="step-inner">
          <StepHead :index="1" :done="hasKey" :active="guideStep === 1" :title="t('dashboard.quickStart.step1')" />
          <p class="step-text">{{ hasKey ? t('dashboard.quickStart.step1Tip') : t('dashboard.quickStart.step1Todo') }}</p>
          <!-- 示意密钥长什么样：和第四步的对话框呼应 -->
          <div class="chat-mock" aria-hidden="true">
            <Icon name="key" size="sm" class="flex-none text-gray-400" />
            <span class="min-w-0 flex-1 truncate font-mono text-[13px] text-gray-500 dark:text-dark-400">sk-••••••••••••••••</span>
            <span class="flex-none rounded-md px-1.5 py-0.5 text-xs font-medium" :class="hasKey ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400' : 'bg-gray-200/70 text-gray-500 dark:bg-dark-700 dark:text-dark-400'">{{ hasKey ? t('dashboard.quickStart.step1MockReady') : t('dashboard.quickStart.step1MockTodo') }}</span>
          </div>
          <!-- 有指引气泡时上方多留空间，气泡不压住上面的内容 -->
          <div class="step-foot flex-wrap gap-x-3 gap-y-2" :class="{ '!pt-14': guideStep === 1 }">
            <router-link v-if="hasKey" to="/keys" class="step-link">{{ t('dashboard.quickStart.step1Manage') }} →</router-link>
            <!-- 带上 create=quick：密钥页直接打开创建窗口，并默认选好 GPT 第一个分组 -->
            <span v-else class="relative inline-flex">
              <router-link :to="quickCreateRoute" class="btn" :class="guideStep === 1 ? 'btn-primary' : 'btn-secondary'" data-testid="quick-start-create">{{ t('dashboard.quickStart.step1Action') }}</router-link>
              <GuideBubble v-if="guideStep === 1" :text="t('dashboard.quickStart.guide.step1')" wrap class="card-bubble" />
            </span>
            <button type="button" class="btn btn-secondary whitespace-nowrap" data-testid="quick-start-key-guide" @click="showKeyGuide = true">
              <Icon name="eye" size="sm" />{{ t('dashboard.quickStart.step1Guide') }}
            </button>
          </div>
        </div>
      </li>

      <!-- ② 充值 -->
      <li class="step" :class="stepClass(2, hasBalance)" data-testid="quick-start-step-2">
        <div class="step-inner step2-inner">
          <StepHead :index="2" :done="hasBalance" :active="guideStep === 2" :title="t('dashboard.quickStart.step2')" />
          <div class="step-body">
            <p class="step-label">{{ t('dashboard.quickStart.step2Label') }}</p>
            <p class="step-value tabular-nums" :class="hasBalance ? 'text-emerald-600 dark:text-emerald-400' : ''" data-testid="quick-start-balance">${{ balance.toFixed(2) }}</p>
          </div>
          <!-- 指引第二步时卡片又窄、一行放不下：「进群领 2 元」排上面、「去充值」排下面，气泡在上方留出的空位里，不压住按钮 -->
          <div class="step-foot flex-wrap gap-x-3 gap-y-2" :class="{ '!pt-14 step2-foot-guided': guideStep === 2 }">
            <router-link :to="rechargePath" class="btn" :class="guideStep === 2 ? 'btn-primary' : 'btn-secondary'" data-testid="quick-start-recharge">{{ t('dashboard.quickStart.step2Action') }}</router-link>
            <span class="relative inline-flex">
              <button type="button" class="inline-flex items-center gap-1.5 whitespace-nowrap text-[15px] font-medium text-primary-600 hover:underline dark:text-primary-400" data-testid="quick-start-reward" @click="showQr = true">
                <img :src="qqGroup.penguinImage" alt="" class="h-4 w-auto" />{{ t('dashboard.quickStart.step2Reward') }}
              </button>
              <GuideBubble v-if="guideStep === 2" :text="t('dashboard.quickStart.guide.step2')" align="end" wrap class="card-bubble" />
            </span>
          </div>
        </div>
      </li>

      <!-- ③ 连接 Codex：出现使用记录就算连接成功 -->
      <li class="step" :class="stepClass(3, hasUsage)" data-testid="quick-start-step-3">
        <div class="step-inner step3-inner">
          <StepHead :index="3" :done="hasUsage" :active="guideStep === 3" :title="t('dashboard.quickStart.step3', { app: appLabel })" :badge="t('dashboard.quickStart.step3Badge')" />
          <div class="mt-4 flex flex-col gap-3">
            <p class="step3-lead" data-testid="quick-start-group-lead">
              {{ t('dashboard.quickStart.step3GroupLead') }}
              <button type="button" class="step3-group-qq-link" :title="t('community.scanToJoin')" :aria-label="`${t('community.scanToJoin')} ${qqGroup.number}`" data-testid="quick-start-group-join" @click="showQr = true">
                <img :src="qqGroup.penguinImage" alt="" />
                <span>{{ t('dashboard.quickStart.step3GroupLabel', { number: qqGroup.number }) }}</span>
              </button>
              {{ t('dashboard.quickStart.step3GroupTail') }}
              <span class="helper-file-name">{{ t('dashboard.quickStart.step3HelperFile') }}</span>
            </p>

            <p class="step3-lead mt-1" data-testid="quick-start-import-lead">{{ t('dashboard.quickStart.step3ImportLead') }}</p>
            <!-- 手机上下拉框和一键导入上下排、各占满一行；电脑上同一行，卡片窄时下拉框自动变窄，再窄就上下排，按钮不伸出卡片 -->
            <div class="import-row -mt-1">
              <div class="import-select relative">
              <select v-model="selectedKeyId" class="h-11 w-full cursor-pointer appearance-none truncate rounded-xl border border-gray-200 bg-white py-0 pl-3 pr-7 text-[15px] text-gray-900 focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100" :aria-label="t('dashboard.quickStart.step3KeyLabel')" data-testid="quick-start-key-select">
                <option v-if="!importKeys.length" :value="null">{{ t('dashboard.quickStart.step3NoKey') }}</option>
                <option v-for="k in importKeys" :key="k.id" :value="k.id" :title="k.name">{{ k.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-gray-400" />
            </div>
              <!-- 还没有可导入的密钥时：自动建一把「测试」密钥（GPT 第一个分组）再继续导入；按选中密钥的分组切换 Codex / Claude -->
              <span class="import-btn-wrap relative flex">
                <CodexImportButton :api-key="selectedKey?.key" :name="selectedKey?.name" :app="importApp" :disabled="creatingKey" class="import-big w-full" data-testid="quick-start-import" @missing-key="createKeyAndImport" />
                <GuideBubble v-if="showImportGuide" :text="t('dashboard.quickStart.guide.import')" placement="bottom" xl="right" />
              </span>
            </div>
          </div>
          <p class="mt-3 max-w-md text-[13px] leading-relaxed text-gray-500 dark:text-dark-400" data-testid="quick-start-step3-hint">{{ t('dashboard.quickStart.step3Hint') }}</p>
          <p class="mt-1.5 flex max-w-md items-start gap-1.5 text-[13px] leading-relaxed text-red-600 dark:text-red-400" data-testid="quick-start-trust">
            <Icon name="shield" size="sm" class="mt-0.5 flex-none" />
            <span>{{ t(os === 'mac' ? 'dashboard.quickStart.step3TrustMac' : 'dashboard.quickStart.step3Trust') }}</span>
          </p>
          <details v-if="os === 'windows'" class="helper-help mt-3" data-testid="quick-start-helper-help">
            <summary class="helper-summary">
              <Icon name="chevronRight" size="sm" class="helper-chevron" />
              <span>{{ t('dashboard.quickStart.step3HelperTitle') }}</span>
            </summary>
            <ol class="helper-steps">
              <li>
                <span class="helper-number" aria-hidden="true">1.</span>
                <div>
                  <p class="helper-step-title">{{ t('dashboard.quickStart.step3HelperOpenTitle') }}</p>
                  <p>{{ t('dashboard.quickStart.step3HelperOpen') }}</p>
                </div>
              </li>
              <li>
                <span class="helper-number" aria-hidden="true">2.</span>
                <div>
                  <div class="helper-copy-row">
                    <p class="helper-step-title">{{ t('dashboard.quickStart.step3HelperRunTitle') }}</p>
                    <button type="button" class="helper-copy" data-testid="quick-start-helper-copy" @click="copyHelperCommand">
                      <Icon :name="helperCommandCopied ? 'check' : 'copy'" size="sm" />
                      {{ helperCommandCopied ? t('common.copied') : t('dashboard.quickStart.step3HelperCopy') }}
                    </button>
                  </div>
                  <button type="button" class="helper-command" :aria-label="t('dashboard.quickStart.step3HelperCopyLabel')" :title="t('dashboard.quickStart.step3HelperCopyLabel')" data-testid="quick-start-helper-command" @click="copyHelperCommand">
                    <code>{{ quickConnect.helperCommand }}</code>
                  </button>
                </div>
              </li>
              <li>
                <span class="helper-number" aria-hidden="true">3.</span>
                <div>
                  <p class="helper-step-title">{{ t('dashboard.quickStart.step3HelperImportTitle') }}</p>
                  <p>{{ t('dashboard.quickStart.step3HelperImport', { app: appLabel }) }}</p>
                </div>
              </li>
            </ol>
          </details>
        </div>
      </li>

      <!-- ④ 发送第一条消息 -->
      <li class="step" :class="stepClass(4, hasUsage)" data-testid="quick-start-step-4">
        <div class="step-inner">
          <StepHead :index="4" :done="hasUsage" :title="t('dashboard.quickStart.step4')" />
          <p class="step-text">{{ t(importApp === 'claude' ? 'dashboard.quickStart.step4HintClaude' : 'dashboard.quickStart.step4Hint') }}</p>
          <!-- 示意要发什么：一个迷你对话输入框 -->
          <div class="chat-mock" aria-hidden="true">
            <span class="min-w-0 flex-1 truncate">{{ t('dashboard.quickStart.step4Sample') }}</span>
            <span class="chat-send"><Icon name="arrowUp" size="sm" /></span>
          </div>
          <div class="step-foot">
            <span class="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm font-medium" :class="hasUsage ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'">
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
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import QqGroupDialog from '@/features/brand/components/QqGroupDialog.vue'
import { buildQuickConnectLink, canQuickImport, currentTipIndex, importAppForGroup, qqGroup, quickConnect, quickStartTips } from '@/features/brand/config'
import CodexImportButton from '@/features/brand/components/CodexImportButton.vue'
import CreateKeyGuideDialog from './CreateKeyGuideDialog.vue'
import GuideBubble from '@/features/brand/components/GuideBubble.vue'
import { nextTestKeyName, pickFirstGptGroup } from '@/features/brand/quickKey'
import { keysAPI } from '@/api/keys'
import { userGroupsAPI } from '@/api/groups'
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

/** 默认始终展开，不因使用记录自动收起；只有用户手动收起，且只对本次访问有效 */
const expanded = ref(true)
const showQr = ref(false)
const showKeyGuide = ref(false)
const keys = ref<ApiKey[]>([])
/** 收起横条的小提示：按时间每 5 小时轮换，打开页面时取一次 */
const tip = quickStartTips[currentTipIndex()]

// 在这里自动建过密钥也算完成第一步
const hasKey = computed(() => props.totalApiKeys > 0 || keys.value.length > 0)
const hasBalance = computed(() => props.balance > 0)
const hasUsage = computed(() => props.totalRequests > 0)

const rechargePath = computed(() => (appStore.cachedPublicSettings?.payment_enabled === false ? '/redeem' : '/purchase'))

type Os = 'windows' | 'mac'
const os: Os = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/i.test(navigator.userAgent) ? 'mac' : 'windows'
const { copied: helperCommandCopied, copyToClipboard: copyHelperText } = useClipboard()

async function copyHelperCommand() {
  const copied = await copyHelperText(quickConnect.helperCommand, t('dashboard.quickStart.step3HelperCopied'))
  if (copied) markDownloaded()
}

const selectedKeyId = ref<number | null>(null)
const byNewest = (a: ApiKey, b: ApiKey) => (b.created_at || '').localeCompare(a.created_at || '') || b.id - a.id
/** 能一键导入的密钥：启用中的 OpenAI（导入 Codex）和 Anthropic（导入 Claude）分组密钥，最新的在前 */
const importKeys = computed(() => keys.value.filter((k) => k.status === 'active' && canQuickImport(k.group)).sort(byNewest))

onMounted(async () => {
  restoreDownloaded()
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

/** 第一步「去创建」：到密钥页并直接打开创建窗口 */
const quickCreateRoute = { path: '/keys', query: { create: 'quick' } }

/** 步骤卡片的三种状态：已完成、当前（新人指引）、未开始 */
function stepClass(index: number, done: boolean) {
  return { 'step-done': done, 'step-guide': guideStep.value === index, 'step-todo': !done && guideStep.value !== index }
}

/** 新人指引：当前该做哪一步（全部完成后不再指引） */
const guideStep = computed<1 | 2 | 3 | null>(() => (!hasKey.value ? 1 : !hasBalance.value ? 2 : !hasUsage.value ? 3 : null))

// 复用原下载指引记录：复制助手命令成功 10 秒后指「一键导入」。
const DOWNLOADED_KEY = 'quick-start-helper-downloaded-at'
const IMPORT_HINT_DELAY = 10_000
const importHintReady = ref(false)
let importHintTimer: ReturnType<typeof setTimeout> | undefined
function scheduleImportHint(downloadedAt: number) {
  clearTimeout(importHintTimer)
  const wait = downloadedAt + IMPORT_HINT_DELAY - Date.now()
  if (wait <= 0) importHintReady.value = true
  else importHintTimer = setTimeout(() => (importHintReady.value = true), wait)
}
function restoreDownloaded() {
  try {
    const at = Number(localStorage.getItem(DOWNLOADED_KEY))
    if (at > 0) scheduleImportHint(at)
  } catch {
    // 存储不可用时从头指引
  }
}
function markDownloaded() {
  const at = Date.now()
  try {
    if (!localStorage.getItem(DOWNLOADED_KEY)) localStorage.setItem(DOWNLOADED_KEY, String(at))
  } catch {
    // ignore
  }
  if (!importHintReady.value) scheduleImportHint(at)
}
onUnmounted(() => clearTimeout(importHintTimer))
const showImportGuide = computed(() => guideStep.value === 3 && importHintReady.value)

/** 第三步没有可导入的密钥时：自动建一把 GPT 第一个分组的「测试」密钥，选中后继续导入 */
const creatingKey = ref(false)
async function createKeyAndImport() {
  if (creatingKey.value) return
  creatingKey.value = true
  try {
    const group = pickFirstGptGroup(await userGroupsAPI.getAvailable())
    if (!group) {
      router.push('/keys')
      return
    }
    const name = nextTestKeyName(keys.value.map((k) => k.name), t('keys.namePlaceholder'))
    const created = await keysAPI.create(name, group.id)
    const withGroup = { ...created, group: created.group ?? ({ ...group } as ApiKey['group']) }
    keys.value = [withGroup, ...keys.value]
    selectedKeyId.value = withGroup.id
    appStore.showSuccess(t('dashboard.quickStart.autoKeyCreated', { name }))
    window.location.href = buildQuickConnectLink(withGroup.key, withGroup.name, importAppForGroup(withGroup.group))
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('keys.failedToSave'))
  } finally {
    creatingKey.value = false
  }
}

function copyQq() {
  void useClipboard().copyToClipboard(qqGroup.number, t('community.copied'))
}

const StepHead = defineComponent({
  props: { index: { type: Number, required: true }, done: Boolean, active: Boolean, title: { type: String, required: true }, badge: { type: String, default: '' } },
  setup(p) {
    return () =>
      h('div', { class: 'flex flex-wrap items-center gap-x-3 gap-y-1.5' }, [
        h(
          'span',
          {
            class: [
              'flex h-9 w-9 flex-none items-center justify-center rounded-full text-base font-bold',
              p.done
                ? 'bg-emerald-500 text-white'
                : p.active
                  ? 'guide-dot bg-primary-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
            ],
            'data-done': p.done ? 'true' : 'false'
          },
          p.done ? '✓' : String(p.index)
        ),
        h('h3', { class: ['text-[17px] font-semibold', p.active ? 'guide-title' : 'text-gray-900 dark:text-white'] }, p.title),
        // 这里由子组件渲染，父组件的 scoped 样式够不到，只能用工具类；
        // 用浅橙色描边小胶囊，和青蓝渐变的“一键导入”按钮区分开
        p.badge
          ? h(
              'span',
              {
                class:
                  'whitespace-nowrap rounded-full bg-orange-50 px-2.5 py-0.5 text-xs font-semibold leading-5 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/30',
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
/* 步骤小卡片：内容靠左铺满，不再在宽列里居中漂着 */
.step {
  @apply relative flex min-w-0 rounded-2xl border border-gray-200 bg-white p-5 transition-colors dark:border-dark-700 dark:bg-dark-800 xl:p-6;
}

.step-done {
  @apply border-emerald-200/70 dark:border-emerald-900/50;
}

/* 大屏时卡片之间画个小箭头，表示先后顺序 */
@media (min-width: 1280px) {
  .step:not(:last-child)::after {
    content: '';
    position: absolute;
    top: 50%;
    right: -11px;
    width: 7px;
    height: 7px;
    border-top: 2px solid rgb(203 213 225);
    border-right: 2px solid rgb(203 213 225);
    transform: translateY(-50%) rotate(45deg);
  }
  .dark .step:not(:last-child)::after {
    border-color: rgb(71 85 105);
  }
}

.step-inner {
  @apply flex h-full min-w-0 flex-1 flex-col;
}

.step-text {
  @apply mt-3 text-[15px] leading-relaxed text-gray-600 dark:text-dark-300;
}

/* 第四步的迷你对话框 */
.chat-mock {
  @apply mt-4 flex max-w-xs items-center gap-2 rounded-xl border border-gray-200 bg-gray-50 py-2 pl-3.5 pr-2 text-[15px] text-gray-700 dark:border-dark-600 dark:bg-dark-900/60 dark:text-dark-200;
}

.chat-send {
  @apply flex h-7 w-7 flex-none items-center justify-center rounded-lg bg-gray-900 text-white dark:bg-white dark:text-gray-900;
}

/* 第三步的一键导入是整个流程的重点，做大 */
.import-big {
  height: 44px !important;
  padding: 0 20px !important;
  font-size: 15px !important;
  border-radius: 12px !important;
}

.step-body {
  @apply mt-3 min-w-0;
}

.step-label {
  @apply text-[13px] text-gray-500 dark:text-dark-400;
}

.step-value {
  @apply mt-1 text-3xl font-bold text-gray-900 dark:text-white;
}

.step-foot {
  @apply mt-auto flex items-center pt-5;
}

.step-link {
  @apply text-[15px] font-medium text-primary-600 hover:underline dark:text-primary-400;
}

/* 每张卡片都是尺寸容器：气泡和排法按卡片自身宽度来 */
.step-inner {
  container-type: inline-size;
}

/* 第一、二步的气泡最宽不超过卡片内容宽度（可伸进内边距 8px），放不下就折成两行，不伸出卡片 */
.card-bubble {
  --gb-max-width: calc(100cqw + 8px);
}

.step2-inner {
  container: step2 / inline-size;
}
@container step2 (max-width: 239.98px) {
  .step2-foot-guided {
    flex-direction: column-reverse;
    align-items: flex-start;
  }
}

/* 第三步按卡片自身宽度决定下拉框和一键导入怎么排（容器查询） */
.step3-inner {
  container: step3 / inline-size;
}

/* 第三步里群文件说明与导入引导 */
.step3-lead {
  @apply text-[15px] font-medium text-gray-700 dark:text-dark-200;
}
.helper-file-name {
  display: inline-block;
  max-width: 100%;
  overflow-wrap: anywhere;
  vertical-align: top;
}
.step3-group-qq-link {
  @apply inline-flex max-w-full items-center gap-1 rounded text-[15px] font-medium text-primary-600 hover:underline dark:text-primary-400;
  overflow-wrap: anywhere;
  vertical-align: baseline;
}
.step3-group-qq-link img {
  @apply h-4 w-auto flex-none;
}
.step3-group-qq-link span {
  @apply min-w-0 tabular-nums;
}

/* 下拉框 + 一键导入：手机上下排、各占满一行 */
.import-row {
  @apply flex flex-col gap-2.5 whitespace-nowrap;
}

/* 电脑上同一行：下拉框原本 10rem，卡片窄时最窄缩到 5.5rem；连这样都放不下就保持上下排 */
@media (min-width: 640px) {
  @container step3 (min-width: 274px) {
    .import-row {
      flex-direction: row;
      align-items: center;
    }
    .import-select {
      flex: 0 1 10rem;
      min-width: 5.5rem;
    }
    .import-btn-wrap {
      flex: none;
    }
  }
}

/* 展开帮助只增加当前卡片高度；临时隐藏导入气泡，避免遮住三步说明。 */
.quick-start-steps:has(.helper-help[open]) {
  align-items: start;
}
.step3-inner:has(.helper-help[open]) .import-btn-wrap :deep(.guide-bubble) {
  display: none;
}

.helper-help {
  @apply min-w-0 border-t border-gray-100 pt-3 dark:border-dark-700;
}
.helper-summary {
  @apply flex cursor-pointer list-none items-start gap-1.5 rounded text-sm leading-6 text-primary-600 dark:text-primary-400;
  overflow-wrap: anywhere;
}
.helper-summary::-webkit-details-marker {
  display: none;
}
.helper-chevron {
  @apply mt-1 flex-none transition-transform;
}
.helper-help[open] .helper-chevron {
  transform: rotate(90deg);
}
.helper-steps {
  @apply mt-4 grid gap-4 text-sm leading-6 text-gray-600 dark:text-dark-300;
}
.helper-steps > li {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  gap: 6px;
  overflow-wrap: anywhere;
}
.helper-number,
.helper-step-title {
  @apply font-medium text-gray-800 dark:text-dark-100;
}
.helper-copy-row {
  @apply flex flex-wrap items-start justify-between gap-x-2 gap-y-1;
}
.helper-copy {
  @apply inline-flex min-w-[88px] flex-none items-center justify-center gap-1 rounded py-0.5 text-[13px] leading-5 text-primary-600 hover:underline dark:text-primary-400;
}
.helper-command {
  @apply mt-2 block w-full min-w-0 rounded-md border border-gray-200 bg-gray-50 p-1.5 text-left text-[12px] leading-6 text-gray-800 hover:border-primary-400 dark:border-dark-600 dark:bg-dark-900/60 dark:text-dark-100;
  white-space: normal;
  overflow-wrap: anywhere;
}
.helper-command code {
  font: inherit;
  word-break: break-all;
  @apply font-mono;
}
.helper-summary:focus-visible,
.helper-copy:focus-visible,
.helper-command:focus-visible {
  @apply outline-none ring-2 ring-primary-500 ring-offset-2 dark:ring-offset-dark-800;
}

.support-link {
  @apply items-center gap-1 whitespace-nowrap text-sm font-medium text-amber-600 hover:underline dark:text-amber-400;
}

/* 新人指引：当前步骤卡片淡青底 + 缓慢呼吸的描边，序号外圈轻扩散，标题颜色缓慢明暗变化 */
.step-guide {
  @apply border-primary-300 dark:border-primary-700;
  background-image: linear-gradient(180deg, rgba(240, 253, 250, 1) 0%, rgba(255, 255, 255, 1) 70%);
  box-shadow: 0 14px 34px -18px rgba(13, 148, 136, 0.55);
}

.dark .step-guide {
  background-image: linear-gradient(180deg, rgba(20, 184, 166, 0.14) 0%, rgba(20, 184, 166, 0.02) 70%);
}

.step-guide::before {
  content: '';
  position: absolute;
  inset: -1px;
  border-radius: inherit;
  pointer-events: none;
  box-shadow: 0 0 0 3px rgba(20, 184, 166, 0.28);
  animation: guide-breathe 2.8s ease-in-out infinite;
}

.step-guide :deep(.guide-dot) {
  animation: guide-halo 2.2s ease-out infinite;
}

.step-guide :deep(.guide-title) {
  @apply text-primary-700 dark:text-primary-300;
  animation: guide-title 2.8s ease-in-out infinite;
}

@keyframes guide-breathe {
  0%,
  100% {
    opacity: 0.25;
  }
  50% {
    opacity: 1;
  }
}

@keyframes guide-halo {
  0% {
    box-shadow: 0 0 0 0 rgba(20, 184, 166, 0.45);
  }
  80%,
  100% {
    box-shadow: 0 0 0 8px rgba(20, 184, 166, 0);
  }
}

@keyframes guide-title {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.65;
  }
}

.header-help {
  @apply min-w-0 items-center gap-1.5 text-[15px] font-medium text-amber-600 hover:underline dark:text-amber-400;
}

/* 标题旁的卖点：渐变色与“一键导入”按钮一致，缓慢流光 */
.tagline {
  @apply inline-flex min-w-0 items-center gap-1.5 rounded-full bg-primary-50/80 px-3 py-1 text-xs font-semibold ring-1 ring-inset ring-primary-100 dark:bg-primary-900/20 dark:ring-primary-800/50 sm:text-sm;
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
  .tagline-text,
  .step-guide::before,
  .step-guide :deep(.guide-dot),
  .step-guide :deep(.guide-title) {
    animation: none;
  }
}
</style>
