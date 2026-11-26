<template>
  <section v-if="checkin.showLayout" ref="host" data-testid="checkin-card" class="daily-sign-card card min-w-0" :class="{ wide }">
    <header class="daily-sign-head">
      <div class="daily-sign-title">
        <span class="daily-title-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5" width="17" height="16" rx="3"/><path d="M7.5 3v4M16.5 3v4M3.5 10h17m-12 5 2.7 2.7 5.3-5.1"/></svg></span>
        <h2>{{ t('checkin.title') }}</h2>
      </div>
      <span v-if="streak" class="daily-streak"><svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M13.9 2.8c.4 4.5-1.7 5.9-3.1 7.4-.3-1.8-1.6-2.6-2.8-3.4C8.3 10 4.7 12 4.7 16.1A7.3 7.3 0 0 0 12 23a7.3 7.3 0 0 0 7.3-6.9c0-5.1-2.3-9.1-5.4-13.3ZM12 20.4c-1.6 0-2.8-1.2-2.8-2.8 0-1.9 1.8-3.5 3-4.9 1.5 1.8 2.6 3.3 2.6 4.9 0 1.6-1.2 2.8-2.8 2.8Z"/></svg>{{ t('checkin.streak', {count: streak}) }}</span>
    </header>
    <div v-if="checkin.loadError && !checkin.loading" class="daily-checkin-error-only" role="alert">{{ t('checkin.loadFailed') }} <button class="daily-sign-button compact" @click="checkin.load()">{{ t('checkin.retry') }}</button></div>
    <div v-else-if="checkin.loading && !details" class="daily-checkin-error-only"><LoadingSpinner /></div>
    <div v-else-if="!details" class="daily-checkin-error-only">{{ t('checkin.unavailableShort') }}</div>
    <template v-else>
      <div class="daily-sign-body">
        <div class="daily-action-slot" data-testid="checkin-action">
          <div v-if="details.status === 'done'" class="daily-reward-hero is-claimed">
            <div class="daily-hero-content"><div class="daily-hero-kicker"><span class="daily-success-check">✓</span> {{ t('checkin.done') }}</div>
              <strong v-if="details.today_amount !== null" class="daily-reward-number" data-testid="checkin-amount">+¥{{ displayAmount.toFixed(2) }}</strong>
              <span v-if="details.today_amount !== null" class="daily-hero-caption">{{ t('checkin.todayReward') }}</span>
            </div>
            <div class="daily-reward-art" aria-hidden="true"><svg viewBox="0 0 120 112"><defs><linearGradient id="checkin-claim-badge" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#c2b5ff"/><stop offset="1" stop-color="#7e68e6"/></linearGradient></defs><circle cx="65" cy="55" r="47" fill="none" stroke="currentColor" stroke-opacity=".10" stroke-width="1.3"/><circle cx="65" cy="55" r="36" fill="none" stroke="currentColor" stroke-opacity=".14" stroke-width="1.3"/><path d="M46 75 37 101l17-5 8 10 5-28M73 75l6 29 9-11 15 3-12-23" fill="#a69af5" opacity=".8"/><circle cx="66" cy="54" r="29" fill="url(#checkin-claim-badge)"/><circle cx="66" cy="54" r="23" fill="none" stroke="#fff" stroke-opacity=".48" stroke-width="1.5"/><path d="m54 54 8 8 16-17" stroke="white" stroke-width="4" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg></div>
          </div>
          <div v-else-if="details.status === 'bind_email'" class="daily-state-note">
            <p>{{ t('checkin.errors.CHECKIN_EMAIL_BIND_REQUIRED') }}</p>
            <router-link class="daily-sign-button" to="/profile">{{ t('checkin.bindEmail') }}</router-link>
          </div>
          <div v-else-if="details.status === 'too_new'" class="daily-state-note"><button class="daily-sign-button" disabled>{{ t('checkin.sign') }}</button><p>{{ t('checkin.availableAt', {date: availableAt}) }}</p></div>
          <div v-else-if="details.status === 'unavailable'" class="daily-state-note">{{ t('checkin.errors.CHECKIN_UNAVAILABLE') }}</div>
          <div v-else-if="details.needs_email" v-show="!challengeActive" class="daily-verification daily-email">
            <p class="daily-email-message">{{ t('checkin.errors.CHECKIN_EMAIL_REQUIRED') }}</p>
            <div class="daily-email-row"><span class="daily-email-mask">{{ checkin.maskedEmail }}</span><button data-testid="checkin-send" class="daily-email-send" :disabled="checkin.sending || checkin.cooldown > 0 || checkin.submitting" @click="sendCode">{{ checkin.cooldown ? t('checkin.cooldown', {seconds:checkin.cooldown}) : t('checkin.sendCode') }}</button></div>
            <div class="daily-email-row"><input v-model="emailCode" data-testid="checkin-email-code" class="daily-email-input" inputmode="numeric" maxlength="6" :placeholder="t('checkin.emailCode')" :aria-label="t('checkin.emailCode')" /><button data-testid="checkin-submit" class="daily-sign-button compact" :disabled="checkin.submitting || checkin.loading" @click="startSign">{{ t('checkin.verifySign') }}</button></div>
            <p class="daily-email-support">{{ app.contactInfo ? t('checkin.emailSupport', {contact: app.contactInfo}) : t('checkin.emailSupportFallback') }}</p>
          </div>
          <div v-else v-show="!challengeActive" class="daily-reward-hero">
            <div class="daily-hero-content"><span class="daily-hero-kicker">{{ t('checkin.dailyReward') }} ✦</span><strong class="daily-hero-heading">{{ t('checkin.todaySign') }}</strong>
              <button data-testid="checkin-submit" type="button" class="daily-sign-button" :disabled="checkin.loading || checkin.submitting" @click="startSign">{{ checkin.submitting ? t('checkin.submitting') : t('checkin.sign') }} <svg viewBox="0 0 20 20" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 10h12m-5-5 5 5-5 5"/></svg></button>
            </div>
            <div class="daily-reward-art" aria-hidden="true"><svg viewBox="0 0 120 112"><defs><linearGradient id="checkin-reward-box" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#c9bdff"/><stop offset="1" stop-color="#8a72ed"/></linearGradient><linearGradient id="checkin-reward-lid" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#eee8ff"/><stop offset="1" stop-color="#ac98fb"/></linearGradient></defs><circle cx="65" cy="56" r="47" fill="none" stroke="currentColor" stroke-opacity=".10" stroke-width="1.2"/><circle cx="65" cy="56" r="36" fill="none" stroke="currentColor" stroke-opacity=".14" stroke-width="1.2"/><g transform="rotate(-10 66 61)"><rect x="37" y="46" width="57" height="45" rx="8" fill="url(#checkin-reward-box)"/><path d="M61 47h11v44H61z" fill="#e9deff" opacity=".9"/><rect x="32" y="39" width="67" height="15" rx="5" fill="url(#checkin-reward-lid)"/><path d="M61 39h11v15H61z" fill="#f3ecff"/><path d="M67 39c-22 2-26-14-16-17 8-3 16 8 16 17Zm0 0c21 2 25-14 15-17-7-3-15 8-15 17Z" stroke="#997fea" stroke-width="4" fill="#eae1ff" stroke-linecap="round" stroke-linejoin="round"/></g><path d="M107 21v10m-5-5h10M21 44v8m-4-4h8M102 80v7m-3.5-3.5h7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg></div>
          </div>
          <div v-if="details.status === 'available' && details.needs_captcha && challengeMounted && (details.captcha_provider !== 'turnstile' || challengeActive)" v-show="challengeActive" class="daily-state-note">
            <p>{{ t('checkin.errors.CHECKIN_CAPTCHA_REQUIRED') }}</p>
            <div class="daily-captcha-host">
              <CaptchaChallenge :key="captchaKey" ref="captcha" v-bind="captchaProps" @verify="verifyTurnstile" @expire="captchaFailed" @error="captchaFailed" />
            </div>
            <LoadingSpinner v-if="checkin.submitting" />
          </div>
        </div>
        <p v-if="errorText" class="daily-status-message" role="alert">{{ errorText }}</p>
        <div class="daily-month">
          <button type="button" data-testid="checkin-prev" :aria-label="t('checkin.prev')" :disabled="checkin.loading || checkin.viewingMonth <= details.registered_month" @click="changeMonth(-1)">‹</button>
          <strong>{{ t('checkin.month', {year, month: monthNumber}) }}</strong>
          <button type="button" data-testid="checkin-next" :aria-label="t('checkin.next')" :disabled="checkin.loading || checkin.viewingMonth >= details.today.slice(0,7)" @click="changeMonth(1)">›</button>
        </div>
        <div class="daily-week"><span v-for="day in 7" :key="day">{{ t('checkin.week.'+day) }}</span></div>
        <div v-if="checkin.monthLoading" class="daily-placeholder"><LoadingSpinner /></div>
        <div v-else-if="checkin.monthError" class="daily-placeholder">{{ t('checkin.loadFailed') }}<button class="daily-sign-button" @click="checkin.showMonth(checkin.viewingMonth)">{{ t('checkin.retry') }}</button></div>
        <div v-else class="daily-days">
          <component :is="cell.record ? 'button':'div'" v-for="cell in cells" :key="cell.date" class="daily-cell" :class="{'is-signed':!!cell.record,'is-today':cell.isToday && cell.inMonth,'is-future':!cell.inMonth||cell.isFuture,'is-signed-today':!!cell.record&&cell.isToday}" :title="cell.record ? recordDetail(cell.record) : undefined" :data-date="cell.date" :data-signed="!!cell.record" @click="cell.record && (selectedDetail=recordDetail(cell.record))"><span class="daily-day-number">{{cell.day}}</span><span v-if="cell.record" class="daily-cell-amount">+¥{{cell.record.amount.toFixed(2)}}</span><span v-if="cell.record" class="daily-cell-dot" aria-hidden="true"/></component>
        </div>
        <div class="daily-legend"><span><i class="daily-legend-dot signed"/>{{t('checkin.signed')}}</span><span><i class="daily-legend-dot today"/>{{t('checkin.today')}}</span></div>
      </div>
      <footer class="daily-metrics"><div><span>{{t('checkin.monthReward',{month:monthNumber})}}</span><strong>{{checkin.currentMonth ? '¥'+checkin.currentMonth.month_amount.toFixed(2) : '—'}}</strong></div><div><span>{{t('checkin.totalReward')}}</span><strong>¥{{details.stats.total_amount.toFixed(2)}}</strong></div><div><span>{{t('checkin.totalDays')}}</span><strong>{{details.stats.total_days}} <small>{{t('checkin.daysUnit')}}</small></strong></div></footer>
      <div v-if="selectedDetail" class="daily-record-detail" role="status">{{selectedDetail}}<button type="button" :aria-label="t('common.close')" @click="selectedDetail=''">×</button></div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import CaptchaChallenge from '@/components/CaptchaChallenge.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { CheckinProof, CheckinRecord } from '@/api/checkin'
import { checkinErrorReason, type DashboardCheckin } from '@/composables/useDashboardCheckin'
import { checkinCalendar } from '@/utils/checkinCalendar'
import './checkin-card.css'

const props = defineProps<{ checkin: DashboardCheckin }>()
const { t } = useI18n()
const app = useAppStore()
const details = computed(() => props.checkin.details)
const host = ref<HTMLElement | null>(null), wide = ref(false)
let observer: ResizeObserver | undefined
watch(host, element => {
  observer?.disconnect()
  if (element && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(([entry]) => { wide.value = entry.target.getBoundingClientRect().width >= 360 })
    observer.observe(element)
  }
})
const selectedDetail = ref('')
watch(() => props.checkin.viewingMonth, () => { selectedDetail.value = '' })
const monthNumber = computed(() => Number(props.checkin.viewingMonth.slice(5)))
const year = computed(() => Number(props.checkin.viewingMonth.slice(0,4)))
const streak = computed(() => details.value?.status === 'done' && details.value.today_amount === null ? 0 : details.value?.stats.streak || 0)
const availableAt = computed(() => (details.value?.available_at || '').slice(5,16).replace('T',' '))
const cells = computed(() => {
  const d = details.value
  if (!d) return []
  const records = new Map((props.checkin.currentMonth?.records || []).map(r => [r.date,r]))
  return checkinCalendar(props.checkin.viewingMonth,d.today,d.registered_month).map(cell => ({...cell,record:cell.inMonth ? records.get(cell.date) : undefined}))
})
function changeMonth(delta: number) { selectedDetail.value = ''; props.checkin.changeMonth(delta) }
function recordDetail(record: CheckinRecord) { return t('checkin.record',{month:Number(record.date.slice(5,7)),day:Number(record.date.slice(8,10)),amount:record.amount.toFixed(2),time:record.time}) }

const emailCode = ref(''), errorText = ref('')
const challengeActive = ref(false), challengeMounted = ref(false), captcha = ref<InstanceType<typeof CaptchaChallenge> | null>(null)
const captchaKey = computed(() => JSON.stringify([details.value?.captcha_provider, details.value?.captcha_public]))
const captchaProps = computed(() => {
  const provider = details.value?.captcha_provider, p = details.value?.captcha_public || {}
  return { turnstileEnabled: provider==='turnstile', turnstileSiteKey: p.site_key || '', tencentEnabled: provider==='tencent', tencentAppId: p.app_id || '', tencentRegion:p.region, aliyunEnabled:provider==='aliyun', aliyunSceneId:p.scene_id, aliyunPrefix:p.prefix, aliyunRegion:p.region }
})
const actionBusy = computed(() => challengeActive.value || props.checkin.submitting)
const reloadReasons = new Set(['CHECKIN_DISABLED','CHECKIN_UNAVAILABLE','CHECKIN_ALREADY_DONE','CHECKIN_ACCOUNT_TOO_NEW','CHECKIN_CAPTCHA_REQUIRED','CHECKIN_CAPTCHA_FAILED','CHECKIN_EMAIL_REQUIRED','CHECKIN_EMAIL_BIND_REQUIRED'])
const knownReasons = new Set([...reloadReasons,'CHECKIN_IP_UNKNOWN','CHECKIN_IP_LIMIT','CHECKIN_FAILED','INVALID_VERIFY_CODE','VERIFY_CODE_MAX_ATTEMPTS','VERIFY_CODE_TOO_FREQUENT','CHECKIN_EMAIL_NOT_REQUIRED'])
function translateError(reason: string) {
  if (!knownReasons.has(reason)) return t('checkin.requestUnknown')
  return reason==='CHECKIN_ACCOUNT_TOO_NEW' ? t('checkin.availableAt', {date:availableAt.value}) : t('checkin.errors.'+reason)
}
function captchaFailed() {
  if (!challengeActive.value || props.checkin.submitting) return
  challengeActive.value = false
  captcha.value?.reset()
  errorText.value = t('checkin.errors.CHECKIN_CAPTCHA_FAILED')
}
async function submit(proof: CheckinProof) {
  if (props.checkin.submitting) return
  const d = details.value
  if (!d || d.status !== 'available') return
  if (d.needs_email) proof.email_code = emailCode.value
  errorText.value = ''
  try {
    if (await props.checkin.submit(proof)) emailCode.value = ''
  } catch (error) {
    captcha.value?.reset()
    const reason = checkinErrorReason(error)
    if (reason === 'VERIFY_CODE_MAX_ATTEMPTS') emailCode.value = ''
    const metadata = (error as { metadata?: Record<string, string> } | null)?.metadata
    errorText.value = reason === 'CHECKIN_FAILED' && metadata?.result_unknown === 'true'
      ? t('checkin.requestUnknown')
      : translateError(reason)
    if (reason === 'CHECKIN_DISABLED') app.showWarning(errorText.value)
    if (reloadReasons.has(reason)) {
      await props.checkin.load()
      if (reason === 'CHECKIN_ACCOUNT_TOO_NEW') errorText.value = translateError(reason)
    }
  } finally { challengeActive.value = false }
}
async function startSign() {
  const d = details.value
  if (!d || d.status !== 'available' || actionBusy.value || props.checkin.loading) return
  errorText.value = ''
  if (d.needs_email && !/^\d{6}$/.test(emailCode.value)) { errorText.value = t('checkin.emailCode'); return }
  if (!d.needs_captcha) { await submit({}); return }
  challengeMounted.value = true
  challengeActive.value = true
  if (d.captcha_provider === 'turnstile') return
  await nextTick()
  const widget = captcha.value
  const result = await widget?.verifyAction()
  if (captcha.value !== widget || !challengeActive.value) return
  if (result) await submit({captcha_token: result.token, captcha_randstr:result.randstr})
  else challengeActive.value = false
}
function verifyTurnstile(token: string) {
  if (challengeActive.value && details.value?.captcha_provider === 'turnstile') void submit({captcha_token:token})
}
async function sendCode() {
  errorText.value = ''
  try { await props.checkin.sendCode() }
  catch (error) {
    const reason = checkinErrorReason(error)
    errorText.value = reason === 'CHECKIN_EMAIL_NOT_REQUIRED' || reason === 'VERIFY_CODE_TOO_FREQUENT' ? translateError(reason) : t('checkin.sendFailed')
    if (reason === 'CHECKIN_EMAIL_NOT_REQUIRED') await props.checkin.load()
  }
}
function fromHeader() {
  if (details.value?.status !== 'available' || details.value.needs_email || details.value.needs_captcha) {
    host.value?.scrollIntoView?.({block:'center', behavior:'instant'})
    if (details.value?.status !== 'available' || details.value.needs_email) return
  }
  void startSign()
}
watch(captchaKey, () => { if (!props.checkin.submitting) challengeActive.value=false })
watch(() => details.value?.status, status => { if (status!=='available') challengeActive.value=false })
const displayAmount = ref(details.value?.today_amount || 0)
let frame = 0
watch(() => details.value?.today_amount, (value, previous) => {
  cancelAnimationFrame(frame)
  if (value == null || previous !== null || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) { displayAmount.value=value||0; return }
  const start = performance.now()
  const tick = (now: number) => {
    const p = Math.min(1, (now-start)/600)
    displayAmount.value = value*p
    if (p<1) frame=requestAnimationFrame(tick)
  }
  displayAmount.value=0; frame=requestAnimationFrame(tick)
})
onBeforeUnmount(() => {observer?.disconnect(); cancelAnimationFrame(frame); captcha.value?.reset()})
defineExpose({ fromHeader, actionBusy })
</script>
