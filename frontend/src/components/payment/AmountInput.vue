<template>
  <div class="space-y-4">
    <!-- Quick Amount Buttons -->
    <div>
      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('payment.quickAmounts') }}
      </label>
      <div class="grid grid-cols-3 gap-x-4 gap-y-4 pt-2">
        <button
          v-for="amt in filteredAmounts"
          :key="amt"
          type="button"
          :class="[
            'relative rounded-lg border-2 px-3 py-3 text-center font-medium transition-colors',
            modelValue === amt
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/40 dark:text-primary-300'
              : quoteFor(amt).percent > 0
                ? 'border-red-200 bg-white text-gray-700 hover:border-red-300 dark:border-red-500/40 dark:bg-dark-800 dark:text-gray-200 dark:hover:border-red-400/60'
                : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:border-dark-500',
          ]"
          :data-testid="`quick-amount-${amt}`"
          :aria-label="hasGlassBonus(amt) ? t('payment.rechargeBonus.quickAmountLabel', { amount: formatPaymentAmount(amt, props.currency), percent: formatRechargeBonusNumber(quoteFor(amt).percent), credited: '¥' + quoteFor(amt).credited.toFixed(2) }) : undefined"
          @click="selectAmount(amt)"
        >
          <!-- 促销价签（单行）：仅命中档位的金额显示；红底白字、内圈点线、右侧圆孔、整体旋转 -->
          <span
            v-if="quoteFor(amt).percent > 0"
            class="pointer-events-none absolute -right-2 -top-3 z-10"
            :class="hasGlassBonus(amt) ? 'recharge-glass-badge' : 'rotate-12'"
            data-testid="quick-amount-bonus-badge"
          >
            <span
              :class="hasGlassBonus(amt) ? 'recharge-glass-label' : 'relative flex items-center gap-1 whitespace-nowrap rounded bg-red-600 py-0.5 pl-1.5 pr-1 text-[11px] font-extrabold leading-tight tracking-tight text-white shadow-md ring-2 ring-white before:pointer-events-none before:absolute before:inset-[2px] before:rounded-sm before:border before:border-dotted before:border-white/70 dark:bg-red-500 dark:ring-dark-800'"
            >
              <span>{{ badgeText(amt) }}</span>
              <span v-if="!hasGlassBonus(amt)" class="h-1 w-1 shrink-0 rounded-full bg-white"></span>
            </span>
          </span>
          <span class="block">{{ amt }}</span>
          <!-- 配置了优惠阶梯时，所有按钮都显示第二行，保持高度一致：赠金显示到账 USD，折扣显示折后实付 -->
          <span
            v-if="showSecondLine"
            :class="[
              'mt-0.5 block text-[11px] font-normal leading-tight',
              quoteFor(amt).percent > 0 ? 'text-red-600 dark:text-red-300' : 'text-gray-400 dark:text-gray-500',
            ]"
            data-testid="quick-amount-credited"
          >{{ secondLine(amt) }}</span>
        </button>
      </div>
    </div>

    <!-- Custom Amount Input -->
    <div>
      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('payment.customAmount') }}
      </label>
      <div class="relative">
        <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500">
          $
        </span>
        <input
          type="text"
          inputmode="decimal"
          :value="customText"
          :placeholder="placeholderText"
          class="input w-full py-3 pl-8 pr-4"
          @input="handleInput"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RechargeBonusTier } from '@/types/payment'
import { formatRechargeBonusNumber, quoteRechargeBonus, type RechargeBonusMode } from '@/utils/rechargeBonus'
import { formatPaymentAmount } from './currency'

const props = withDefaults(defineProps<{
  amounts?: number[]
  modelValue: number | null
  min?: number
  max?: number
  /** 充值优惠阶梯（按 min_amount 升序）；为空时不显示价签与第二行 */
  bonusTiers?: RechargeBonusTier[]
  /** 阶梯模式：bonus 赠金 / discount 折扣 */
  bonusMode?: RechargeBonusMode
  /** 充值倍率（1 支付币种 = multiplier USD），用于计算到账金额 */
  multiplier?: number
  /** 支付币种（折扣模式第二行实付金额的币种与精度） */
  currency?: string
}>(), {
  amounts: () => [1, 5, 10, 20, 50, 100, 200, 500, 1000],
  min: 0,
  max: 0,
  bonusTiers: () => [],
  bonusMode: 'bonus',
  multiplier: 1,
  currency: undefined,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t } = useI18n()

const customText = ref('')

// 0 = no limit
const filteredAmounts = computed(() =>
  props.amounts.filter((a) => (props.min <= 0 || a >= props.min) && (props.max <= 0 || a <= props.max))
)

const showSecondLine = computed(() => props.bonusTiers.length > 0)

function currencyDigits(): number {
  if (!props.currency) return 2
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency: props.currency }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function quoteFor(amt: number) {
  return quoteRechargeBonus(props.bonusTiers, amt, {
    multiplier: props.multiplier,
    mode: props.bonusMode,
    currencyDigits: currencyDigits(),
  })
}

function hasGlassBonus(amt: number): boolean {
  const quote = quoteFor(amt)
  return props.bonusMode === 'bonus' && quote.percent > 0 && quote.bonus > 0
}

// 价签文案：赠金「+20%」，折扣「20% OFF」
function badgeText(amt: number): string {
  const percent = formatRechargeBonusNumber(quoteFor(amt).percent)
  return props.bonusMode === 'discount' ? `${percent}% OFF` : `+${percent}%`
}

function secondLine(amt: number): string {
  const quote = quoteFor(amt)
  if (props.bonusMode === 'discount') {
    return t('payment.rechargeBonus.payShort', { amount: formatPaymentAmount(quote.payBase, props.currency) })
  }
  return t('payment.rechargeBonus.creditedShort', { amount: '¥' + quote.credited.toFixed(2) })
}

const placeholderText = computed(() => {
  if (props.min > 0 && props.max > 0) return `${props.min} - ${props.max}`
  if (props.min > 0) return `≥ ${props.min}`
  if (props.max > 0) return `≤ ${props.max}`
  return t('payment.enterAmount')
})

const AMOUNT_PATTERN = /^\d*(\.\d{0,2})?$/

function selectAmount(amt: number) {
  customText.value = String(amt)
  emit('update:modelValue', amt)
}

function handleInput(e: Event) {
  const input = e.target as HTMLInputElement
  const val = input.value
  if (!AMOUNT_PATTERN.test(val)) {
    input.value = customText.value
    return
  }
  customText.value = val
  if (val === '') {
    emit('update:modelValue', null)
    return
  }
  const num = parseFloat(val)
  if (!isNaN(num) && num > 0) {
    emit('update:modelValue', num)
  } else {
    emit('update:modelValue', null)
  }
}

watch(() => props.modelValue, (v) => {
  if (v !== null && String(v) !== customText.value) {
    customText.value = String(v)
  }
}, { immediate: true })
</script>

<style scoped>
.recharge-glass-badge { isolation:isolate; padding:1px; overflow:hidden; border:1px solid #7dd3fc; border-radius:999px; background:linear-gradient(120deg,#e0f7ffcc,#f7ffe5e6); box-shadow:0 2px 8px #38bdf82e,inset 0 1px #fff; backdrop-filter:blur(8px); }
.recharge-glass-badge::before { content:''; position:absolute; z-index:-1; inset:-90%; background:conic-gradient(from 0deg,transparent 65%,#38bdf8 80%,#fff 87%,#67e8f9 93%,transparent); animation:recharge-border-orbit 4s linear infinite; }
.recharge-glass-label { position:relative; display:flex; align-items:center; overflow:hidden; padding:3px 9px; border-radius:999px; background:linear-gradient(120deg,#edfbffff,#f7ffeaff); color:#075985; font-size:11px; font-weight:800; line-height:1.2; white-space:nowrap; }
.recharge-glass-label::after { content:''; position:absolute; inset:-50% -100%; background:linear-gradient(110deg,transparent 40%,#ffffffd9 49%,transparent 58%); transform:translateX(-70%); animation:recharge-glass-sweep 6s ease-in-out infinite; pointer-events:none; }
@keyframes recharge-border-orbit { to { transform:rotate(360deg); } }
@keyframes recharge-glass-sweep { 0%,70% { transform:translateX(-70%); opacity:0; } 73% { opacity:.8; } 88%,100% { transform:translateX(70%); opacity:0; } }
@supports not (backdrop-filter:blur(8px)) { .recharge-glass-badge { background:#e0f7ff; } }
@media (prefers-reduced-motion:reduce) { .recharge-glass-badge::before,.recharge-glass-label::after { animation:none; } .recharge-glass-label::after { display:none; } }
</style>
