import { computed, onBeforeUnmount, onMounted, ref, shallowRef, type UnwrapNestedRefs } from 'vue'
import { checkinAPI, type CheckinDetails, type CheckinMonth, type CheckinProof, type CheckinState } from '@/api/checkin'
import { useAuthStore } from '@/stores/auth'
import { shiftCheckinMonth } from '@/utils/checkinCalendar'

export function checkinErrorReason(error: unknown): string {
  return typeof error === 'object' && error !== null && 'reason' in error ? String(error.reason || '') : ''
}

export function useDashboardCheckin() {
  const auth = useAuthStore()
  const state = shallowRef<CheckinState | null>(null)
  const details = computed(() => state.value && 'today' in state.value ? state.value : null)
  const enabled = computed(() => state.value?.enabled === true)
  const loading = ref(false)
  const loadError = ref(false)
  const showLayout = computed(() => enabled.value || loadError.value)
  const submitting = ref(false)
  const viewingMonth = ref('')
  const months = shallowRef<Record<string, CheckinMonth>>({})
  const currentMonth = computed(() => months.value[viewingMonth.value])
  const monthLoading = ref(false)
  const monthError = ref(false)
  const cooldown = ref(0)
  const sending = ref(false)
  const maskedEmail = ref('')
  let resetTimer: ReturnType<typeof setTimeout> | undefined
  let cooldownTimer: ReturnType<typeof setInterval> | undefined
  let monthSeq = 0
  let active = true
  let resetDue = false

  function clearReset() { clearTimeout(resetTimer); resetTimer = undefined }
  function stopCooldown() { clearInterval(cooldownTimer); cooldownTimer = undefined; cooldown.value = 0 }
  function scheduleReset(data: CheckinDetails) {
    clearReset()
    resetTimer = setTimeout(() => {
      if (submitting.value) resetDue = true
      else void load()
    }, Math.max(0, data.seconds_until_reset * 1000))
  }

  async function load() {
    if (!active || loading.value || submitting.value) return
    clearReset()
    loading.value = true
    ++monthSeq
    monthLoading.value = false
    monthError.value = false
    try {
      const result = await checkinAPI.get()
      if (!active) return
      state.value = result
      loadError.value = false
      months.value = 'today' in result ? { [result.month]: result } : {}
      viewingMonth.value = 'today' in result ? result.month : ''
      maskedEmail.value = 'today' in result ? result.masked_email || '' : ''
      if ('today' in result) scheduleReset(result)
      else stopCooldown()
    } catch {
      if (active) {
        state.value = null
        loadError.value = true
      }
    } finally {
      if (active) loading.value = false
    }
  }

  async function showMonth(month: string) {
    const d = details.value
    if (!active || !d || month < d.registered_month || month > d.today.slice(0, 7)) return
    const seq = ++monthSeq
    viewingMonth.value = month
    monthLoading.value = false
    monthError.value = false
    if (months.value[month]) return
    monthLoading.value = true
    try {
      const result = await checkinAPI.records(month)
      if (active && seq === monthSeq) months.value = { ...months.value, [month]: result }
    } catch {
      if (active && seq === monthSeq) monthError.value = true
    } finally {
      if (active && seq === monthSeq) monthLoading.value = false
    }
  }
  function changeMonth(offset: number) { void showMonth(shiftCheckinMonth(viewingMonth.value, offset)) }

  async function submit(proof: CheckinProof) {
    if (!active || loading.value || submitting.value || details.value?.status !== 'available') return false
    submitting.value = true
    const userId = auth.user?.id
    try {
      const result = await checkinAPI.submit(proof)
      if (auth.user?.id === userId) auth.applyBalance(result.balance)
      if (!active) return true
      const d = details.value
      if (!d) return true
      const sameMonth = result.month === viewingMonth.value
      if (sameMonth) {
        ++monthSeq
        monthLoading.value = false
        monthError.value = false
      }
      const previous = months.value[result.month]
      const record = { date: result.checkin_date, time: result.time, amount: result.amount }
      if (previous) months.value = { ...months.value, [result.month]: {
        month: result.month, month_amount: result.month_amount,
        records: [...previous.records.filter(r => r.date !== record.date), record].sort((a, b) => a.date.localeCompare(b.date)),
      } }
      // A POST can cross midnight/month boundaries. Reload authoritative state if
      // the month/date changed, rather than fabricating an incomplete month.
      if (result.checkin_date !== d.today || !previous) resetDue = true
      state.value = { ...d, status: 'done', needs_email: false, needs_captcha: false, today_amount: result.amount,
        today: result.checkin_date, stats: { total_days: result.total_days, total_amount: result.total_amount, streak: result.streak } }
      return true
    } finally {
      submitting.value = false
      if (active && resetDue) {
        resetDue = false
        void load()
      }
    }
  }

  function startCooldown(seconds: number) {
    stopCooldown()
    const end = Date.now() + seconds * 1000
    cooldown.value = seconds
    cooldownTimer = setInterval(() => {
      cooldown.value = Math.max(0, Math.ceil((end - Date.now()) / 1000))
      if (cooldown.value === 0) stopCooldown()
    }, 250)
  }
  async function sendCode() {
    if (!active || sending.value || cooldown.value || details.value?.status !== 'available' || !details.value.needs_email) return
    sending.value = true
    try {
      const result = await checkinAPI.sendCode()
      if (active) {
        maskedEmail.value = result.masked_email
        startCooldown(result.cooldown_seconds)
      }
    } catch (error) {
      if (active && checkinErrorReason(error) === 'VERIFY_CODE_TOO_FREQUENT') startCooldown(60)
      throw error
    } finally {
      sending.value = false
    }
  }

  onMounted(() => { void load() })
  onBeforeUnmount(() => { active = false; ++monthSeq; clearReset(); stopCooldown() })
  return { state, details, enabled, showLayout, loading, loadError, submitting, viewingMonth, currentMonth,
    monthLoading, monthError, cooldown, sending, maskedEmail, load, showMonth, changeMonth, submit, sendCode }
}
export type DashboardCheckin = UnwrapNestedRefs<ReturnType<typeof useDashboardCheckin>>
