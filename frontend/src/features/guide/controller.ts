import { driver } from './driver/driver.mjs'
import { createFlow } from './flow'
import { configuration, configPreview, enabledModules, initialOptOut, registrationModules, type GuideConfig, type ModuleId } from './config'
import { steps } from './steps'
import { clearRegistrationProgress, historyKey, isRegistrationRecord, onGuideRegistration, progressKey, readGuideRegistration, readSessionValue, registrationKey, suppressedKey, writeSessionValue, type GuideUser, type RegistrationOwner, type RegistrationRecord } from './registration'

export interface GuideState {
  user: GuideUser | null
  authenticated: boolean
  simpleMode: boolean
  backendMode: boolean
  path: string
  requiresAuth: boolean
  requiresAdmin: boolean
}
export interface ProgressRecord extends RegistrationRecord {
  modules: ModuleId[]; configuration: string; moduleId: ModuleId; step: string; path: string
}
export interface ControllerOptions {
  getState: () => GuideState
  getPublicSettings: () => any
  onChange?: () => void
  config?: GuideConfig | null
  driverFactory?: typeof driver
}
export const stopMessages: Record<string, string> = {
  'error:more-settings': '菜单信息暂时没有读取成功，本章引导已停止，页面可以继续使用。可从右下角「使用引导」重试。',
  'error:more-target': '左侧菜单入口缺失、重复或暂时无法显示，本章引导已停止，页面可以继续使用。展开菜单后可从右下角「使用引导」重试；仍失败时请联系网站管理员。',
  'error:target-timeout': '等待页面控件超时，引导已停止。页面可以继续使用。',
  'error:engine-frame': '引导显示发生异常，已停止。页面可以继续使用。',
  'error:popover-position': '提示位置暂不可用，引导已停止。调整窗口后，可从右下角「使用引导」重新开始。',
  'cancelled:account-changed': '引导会话已失效，已停止。页面可以继续使用。',
  'cancelled:escape': '已按 Esc 退出引导。',
  'cancelled:dialog-open': '请先关闭当前窗口，再开始这段引导。',
  'error:recharge-entry': '暂时无法确认充值入口，引导已停止，页面可继续使用。',
  'error:quick-start-entry': '暂时找不到快速开始入口，引导已停止。页面可以继续使用；一直这样的话，请联系网站管理员。',
  'error:document-target': '快速开始页面的内容有变化，引导没法继续，已停止。页面可以正常阅读。',
  'cancelled:recharge-busy': '充值页现在没有显示充值金额（可能正在付款，或打开的是订阅页签），引导已停止。切回充值后，可从右下角「使用引导」查看本章。',
}

export function createGuideController(options: ControllerOptions) {
  const config = options.config === undefined ? configuration : options.config
  const modules = config ? enabledModules(config) : []
  const automaticModules = config ? registrationModules(config) : []
  const configIdentity = JSON.stringify(config?.modules || [])
  let destroyed = false, launching = false, pageHidden = false, suppressed = readSessionValue(suppressedKey) === '1'
  let proof: RegistrationRecord | null = null, manual = false, lastReason: string | null = null, note = ''
  let identity: string | null = null
  let acceptedRegistration: RegistrationOwner | null = null
  const registrationOwner = (): RegistrationOwner | null => proof || (identity ? { userId: Number(identity.split(':')[0]) } : null)
  const viewedModules = new Set<ModuleId>()
  const publish = () => { try { options.onChange?.() } catch { /* Optional UI must not affect the site. */ } }
  const flow = createFlow(options.driverFactory || driver, {
    modules, registrationModules: automaticModules, moduleConfig: config?.modules || [], getPublicSettings: options.getPublicSettings,
    onContinue: () => { void continueChapter() },
    isCurrentUser: (userId: number) => available() && options.getState().user?.id === userId,
    onStep(step: string, moduleId: ModuleId) {
      const restoringWelcome = launching && step === 'welcome' && proof !== null && 'step' in proof
      if (step === 'chapter-break') note = ''
      else if (!restoringWelcome) viewedModules.add(moduleId)
      // During resume, the temporary welcome must not replace the saved real step.
      if (!manual && proof && !restoringWelcome) {
        const progress: ProgressRecord = { ...proof, modules: automaticModules, configuration: configIdentity, moduleId, step, path: options.getState().path }
        writeSessionValue(progressKey, JSON.stringify(progress))
      }
      publish()
    },
    onStop(reason: string, step: string) {
      lastReason = reason
      if (reason === 'suspended') { proof = null; publish(); return }
      suppressed = true; writeSessionValue(suppressedKey, '1'); clearRegistrationProgress(registrationOwner())
      let history: any[] = []
      try { const saved = JSON.parse(readSessionValue(historyKey) || '[]'); if (Array.isArray(saved)) history = saved } catch { /* Ignore malformed diagnostics. */ }
      const result = reason.startsWith('completed') ? 'completed' : reason.startsWith('error') ? 'error' : 'cancelled'
      writeSessionValue(historyKey, JSON.stringify([...history.slice(-19), {
        at: Date.now(), result, reason, step, trigger: manual ? 'manual' : 'registration',
        ...(!manual && proof ? { registrationId: proof.registrationId } : {}),
        ...(reason === 'completed:user_confirmed' ? { confirmation: 'user_confirmed' } : reason === 'completed:quickstart_user_confirmed' ? { confirmation: 'quickstart_user_confirmed' } : {}),
      }]))
      note = stopMessages[reason] || (result === 'completed' ? reason === 'completed:tour_viewed' ? '已看完本次内容。' : '已记录你的确认。' : result === 'error' ? '引导已停止，页面可以继续使用。' : '已退出引导。')
      proof = null; publish()
    },
  })
  if (configPreview) note = modules.length ? '正在试用所选章节，仅当前浏览器生效。点击右下角「使用引导」开始。' : '试用配置未启用任何模块，引导已关闭。'

  function storedIdentityMatches(): boolean {
    try {
      const user = options.getState().user
      const storedUser = JSON.parse(localStorage.getItem('auth_user') || 'null')
      return !!localStorage.getItem('auth_token') && storedUser?.id === user?.id && storedUser?.role === user?.role
    } catch { return false }
  }
  function available(): boolean {
    if (destroyed || pageHidden || initialOptOut || new URLSearchParams(location.search).get('onboarding') === 'off' || !modules.length) return false
    const state = options.getState()
    return !!state.authenticated && state.user?.role === 'user' && storedIdentityMatches() && !state.simpleMode && !state.backendMode && state.requiresAuth && !state.requiresAdmin && !/^\/(login|register|email-verify|auth|setup|admin)(\/|$)/.test(state.path) && !!document.querySelector('[data-tour="sidebar-my-keys"]')
  }
  async function launch(record: RegistrationRecord | null, module?: ModuleId, resume?: ProgressRecord) {
    if (launching || flow.active || !available()) return false
    const user = options.getState().user!
    identity = `${user.id}:${user.role}`
    launching = true
    manual = !!module
    proof = record
    if (manual) { suppressed = true; writeSessionValue(suppressedKey, '1'); clearRegistrationProgress({ userId: user.id }) }
    else writeSessionValue(registrationKey, null)
    publish()
    try {
      return await flow.start({ userId: user.id }, { manual, module, resume })
    } catch { flow.stop('error:engine-frame'); return false }
    finally { launching = false; publish(); void update() }
  }
  function validProgress(value: unknown, userId: number): value is ProgressRecord {
    const progress = value as ProgressRecord | null
    return isRegistrationRecord(progress, userId) && progress.configuration === configIdentity && JSON.stringify(progress.modules) === JSON.stringify(automaticModules) && automaticModules.includes(progress.moduleId) && (progress.step === 'chapter-break' || Object.prototype.hasOwnProperty.call(steps, progress.step) || progress.moduleId === 'more' && ['more-help', 'more-tools'].includes(progress.step)) && typeof progress.path === 'string'
  }
  async function update() {
    if (destroyed) return
    try {
      const state = options.getState()
      // The signup callback runs after auth is persisted, even while the route is still an auth page.
      const record = state.authenticated && state.user?.role === 'user' && storedIdentityMatches() ? readGuideRegistration(state.user.id) : null
      const current = state.authenticated && state.user ? `${state.user.id}:${state.user.role}` : null
      if (identity && identity !== current) { viewedModules.clear(); clearRegistrationProgress(registrationOwner()); flow.stop('cancelled:account-changed'); proof = null }
      identity = current
      if (flow.active && !available()) {
        clearRegistrationProgress(registrationOwner())
        flow.stop(/^\/(login|register|email-verify|auth|setup)(\/|$)/.test(state.path) ? 'cancelled:account-route' : 'cancelled:account-changed')
      }
      if (record && (acceptedRegistration?.userId !== record.userId || acceptedRegistration.registrationId !== record.registrationId)) {
        const user = options.getState().user
        if (!options.getState().authenticated || user?.id !== record.userId || user.role !== 'user' || !storedIdentityMatches() || !isRegistrationRecord(record, user.id)) { publish(); return }
        const pending = readGuideRegistration(user.id)
        if (pending?.registrationId !== record.registrationId || !flow.resetForRegistration()) { publish(); return }
        // Stop callbacks above may have reinstated the old visit suppression. Reset only after they finish.
        suppressed = false; writeSessionValue(suppressedKey, null)
        acceptedRegistration = { userId: record.userId, registrationId: record.registrationId }
        lastReason = null; note = ''; viewedModules.clear()
        let progress: RegistrationRecord | null
        try { progress = JSON.parse(readSessionValue(progressKey) || 'null') } catch { progress = null }
        if (progress && (progress.userId !== record.userId || progress.registrationId !== record.registrationId)) writeSessionValue(progressKey, null)
      }
      publish()
      if (launching || flow.active || suppressed || !available()) return
      const stored = readSessionValue(progressKey)
      if (stored) {
        let progress: unknown
        try { progress = JSON.parse(stored) } catch { progress = null }
        if (!validProgress(progress, state.user!.id)) { writeSessionValue(progressKey, null) }
        else {
          const routeMatches = progress.path === state.path || (['menu', 'navigate', 'create'].includes(progress.step) && state.path === '/keys')
          if (routeMatches) { await launch(progress, undefined, progress); return }
          proof = progress; manual = false
          // Record a rejected resume through the same completion path, without starting Driver.
          lastReason = 'cancelled:route-changed'; suppressed = true; writeSessionValue(suppressedKey, '1'); clearRegistrationProgress(progress)
          let history: any[] = []
          try { const value = JSON.parse(readSessionValue(historyKey) || '[]'); if (Array.isArray(value)) history = value } catch { /* Ignore invalid history. */ }
          writeSessionValue(historyKey, JSON.stringify([...history.slice(-19), { at: Date.now(), result: 'cancelled', reason: lastReason, step: progress.step, trigger: 'registration', registrationId: progress.registrationId }]))
          proof = null; note = '已退出引导。'; publish(); return
        }
      }
      if (!record) return
      if (!automaticModules.length) { writeSessionValue(registrationKey, null); return }
      await launch(record)
    } catch { flow.stop('error:engine-frame') }
  }
  function storageChanged(event: StorageEvent) {
    if (event.storageArea && event.storageArea !== localStorage) return
    if (event.key !== null && !['auth_token', 'auth_user', 'refresh_token'].includes(event.key)) return
    if (storedIdentityMatches()) return
    viewedModules.clear(); clearRegistrationProgress(registrationOwner()); flow.stop('cancelled:account-changed'); publish()
  }
  async function continueChapter() {
    if (launching || !available() || !flow.paused) return false
    if (flow.dialogOpen) { note = '请先关闭当前窗口，再继续引导。'; publish(); return false }
    launching = true; note = ''; publish()
    try { return await flow.continue() }
    catch { flow.stop('error:engine-frame'); return false }
    finally { launching = false; publish(); void update() }
  }
  const registered = onGuideRegistration(() => { void update() })
  const observer = new MutationObserver(() => { void update() })
  observer.observe(document.body, { childList: true, subtree: true })
  const pageHide = () => { pageHidden = true; flow.stop('suspended') }
  const pageShow = () => { pageHidden = false; void update() }
  window.addEventListener('storage', storageChanged)
  window.addEventListener('pagehide', pageHide)
  window.addEventListener('pageshow', pageShow)
  return {
    update,
    available,
    replay(module: ModuleId) { return modules.includes(module) ? launch(null, module) : Promise.resolve(false) },
    continue: continueChapter,
    exit() { flow.stop() },
    status: () => ({ available: available(), active: flow.active, paused: flow.paused, pendingModule: flow.paused ? flow.moduleId : null, viewedModules: modules.filter(module => viewedModules.has(module)), launching, step: flow.active ? flow.step : null, module: flow.moduleId, modules: [...modules], registrationModules: [...automaticModules], activeModules: flow.modules, reason: lastReason, note, configPreview, clean: flow.clean, resources: flow.resources }),
    destroy() {
      if (destroyed) return
      destroyed = true; viewedModules.clear(); registered(); observer.disconnect()
      window.removeEventListener('storage', storageChanged); window.removeEventListener('pagehide', pageHide); window.removeEventListener('pageshow', pageShow)
      flow.stop('cancelled:unloaded')
    },
  }
}
