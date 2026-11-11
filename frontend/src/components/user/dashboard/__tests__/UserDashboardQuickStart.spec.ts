import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserDashboardQuickStart from '../UserDashboardQuickStart.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'
import { buildQuickConnectLink, canQuickImport, currentTipIndex, importAppForGroup, quickConnect, quickStartTips } from '@/features/brand/config'
import { nextTestKeyName, pickFirstGptGroup } from '@/features/brand/quickKey'

const { list, create, getAvailable, showSuccess, showError, copyToClipboard, settings, push } = vi.hoisted(() => ({
  push: vi.fn(),
  list: vi.fn(),
  create: vi.fn(),
  getAvailable: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  copyToClipboard: vi.fn().mockResolvedValue(true),
  settings: { value: { payment_enabled: true } as { payment_enabled: boolean } | null }
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list, create } }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getAvailable } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError, get cachedPublicSettings() { return settings.value } }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key) })
}))

enableAutoUnmount(afterEach)

const key = (id: number, name: string, platform: string, created_at: string, status = 'active') => ({
  id, name, key: `sk-${id}`, status, created_at, group: { platform }
})

async function collapsedPanel() {
  const w = await panel({ totalRequests: 11 })
  await w.get('[data-testid="quick-start-collapse"]').trigger('click')
  return w
}

async function panel(props: { totalApiKeys?: number; totalRequests?: number; balance?: number } = {}) {
  const w = mount(UserDashboardQuickStart, {
    props: { totalApiKeys: 0, totalRequests: 0, balance: 0, ...props },
    global: { stubs: { Icon: true, QqGroupDialog: true, CreateKeyGuideDialog: true, RouterLink: { props: ['to'], template: '<a :data-to="typeof to === \'string\' ? to : JSON.stringify(to)"><slot /></a>' } } }
  })
  await flushPromises()
  return w
}

const done = (w: Awaited<ReturnType<typeof panel>>, step: number) =>
  w.get(`[data-testid="quick-start-step-${step}"] [data-done]`).attributes('data-done') === 'true'

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  settings.value = { payment_enabled: true }
  list.mockResolvedValue({ items: [] })
  getAvailable.mockResolvedValue([])
})

describe('quick start panel', () => {
  it('never collapses on its own, however many usage records there are', async () => {
    for (const totalRequests of [0, 10, 11, 5000]) {
      const w = await panel({ totalRequests })
      expect(w.find('[data-testid="quick-start"]').exists()).toBe(true)
      expect(w.find('[data-testid="quick-start-collapsed"]').exists()).toBe(false)
    }
  })

  it('collapses on click for this visit only', async () => {
    const w = await panel()
    await w.get('[data-testid="quick-start-collapse"]').trigger('click')
    expect(w.find('[data-testid="quick-start-collapsed"]').exists()).toBe(true)
    await w.get('[data-testid="quick-start-expand"]').trigger('click')
    expect(w.find('[data-testid="quick-start"]').exists()).toBe(true)
    expect((await panel()).find('[data-testid="quick-start"]').exists()).toBe(true)
  })

  it('ticks steps from keys, balance and usage; step 3 ticks with the first usage record', async () => {
    const fresh = await panel()
    expect([1, 2, 3, 4].map((s) => done(fresh, s))).toEqual([false, false, false, false])
    expect(list).not.toHaveBeenCalled()
    const w = await panel({ totalApiKeys: 1, balance: 2, totalRequests: 1 })
    expect([1, 2, 3, 4].map((s) => done(w, s))).toEqual([true, true, true, true])
    const noUsage = await panel({ totalApiKeys: 1, balance: 2 })
    expect(done(noUsage, 3)).toBe(false)
  })

  it('links recharge to purchase, or redeem when payment is off', async () => {
    expect((await panel()).get('[data-testid="quick-start-recharge"]').attributes('data-to')).toBe('/purchase')
    settings.value = { payment_enabled: false }
    expect((await panel()).get('[data-testid="quick-start-recharge"]').attributes('data-to')).toBe('/redeem')
  })

  it('imports the newest active OpenAI or Anthropic key and switches between Codex and Claude with the selected key', async () => {
    list.mockResolvedValue({ items: [
      key(1, '旧', 'openai', '2026-10-01'),
      key(2, '测试', 'openai', '2026-10-30'),
      key(3, 'claude', 'anthropic', '2026-10-31'),
      key(4, '停用', 'openai', '2026-10-29', 'inactive'),
      key(5, 'gemini', 'gemini', '2026-11-01')
    ] })
    const w = await panel({ totalApiKeys: 5 })
    expect(w.get('[data-testid="quick-start-step-1"]').text()).toContain('step1Tip')
    expect(w.get('[data-testid="quick-start-step-1"]').text()).not.toContain('测试')
    const select = w.get('[data-testid="quick-start-key-select"]')
    expect(select.findAll('option').map((o) => o.text())).toEqual(['claude', '测试', '旧'])
    // 页面上只有示意用的 sk-••••，不出现真实密钥
    expect(w.text()).not.toMatch(/sk-[0-9a-z]/i)

    const step3 = () => w.get('[data-testid="quick-start-step-3"]').text()
    const location = { href: '' }
    vi.stubGlobal('location', location)
    // 默认是最新的 Anthropic 密钥：同一个按钮变成导入 Claude
    expect(w.get('[data-testid="quick-start-import"]').attributes('data-app')).toBe('claude')
    expect(step3()).toContain('{"app":"Claude"}')
    expect(w.get('[data-testid="quick-start-step-4"]').text()).toContain('step4HintClaude')
    await w.get('[data-testid="quick-start-import"]').trigger('click')
    expect(location.href).toBe(buildQuickConnectLink('sk-3', 'claude', 'claude'))

    // 切到 OpenAI 密钥：按钮文字和链接立刻变回 Codex
    await select.setValue(2)
    expect(w.findAll('[data-testid="quick-start-import"]')).toHaveLength(1)
    expect(w.get('[data-testid="quick-start-import"]').attributes('data-app')).toBe('codex')
    expect(step3()).toContain('{"app":"Codex"}')
    expect(w.get('[data-testid="quick-start-step-4"]').text()).not.toContain('step4HintClaude')
    await w.get('[data-testid="quick-start-import"]').trigger('click')
    expect(location.href).toBe(buildQuickConnectLink('sk-2', '测试'))
    await select.setValue(1)
    await w.get('[data-testid="quick-start-import"]').trigger('click')
    expect(location.href).toBe(buildQuickConnectLink('sk-1', '旧'))
    vi.unstubAllGlobals()
  })

  it('always shows the key picker and import button, even with one key or none', async () => {
    list.mockResolvedValue({ items: [key(2, '测试', 'openai', '2026-10-30')] })
    const one = await panel({ totalApiKeys: 1 })
    expect(one.get('[data-testid="quick-start-key-select"]').findAll('option').map((o) => o.text())).toEqual(['测试'])
    expect(one.find('[data-testid="quick-start-import"]').exists()).toBe(true)

    list.mockResolvedValue({ items: [key(3, 'gemini', 'gemini', '2026-10-31')] })
    const none = await panel({ totalApiKeys: 1 })
    expect(none.find('[data-testid="quick-start-import"]').exists()).toBe(true)
    expect(none.get('[data-testid="quick-start-step-3"]').text()).not.toContain('step3Need')
    const location = { href: '' }
    vi.stubGlobal('location', location)
    await none.get('[data-testid="quick-start-import"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/keys')
    expect(location.href).toBe('')
    vi.unstubAllGlobals()

    const fresh = await panel()
    expect(fresh.find('[data-testid="quick-start-import"]').exists()).toBe(true)
    expect(fresh.get('[data-testid="quick-start-key-select"]').text()).toContain('step3NoKey')
    expect(none.find('[data-testid="quick-start-fallback"]').exists()).toBe(false)
  })

  it('copies the QQ number on desktop and links to the group on mobile', async () => {
    const w = await panel()
    await w.get('[data-testid="quick-start-qq-copy"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('1020015031', 'community.copied')
    expect(w.get('[data-testid="quick-start-qq-mobile"]').attributes('href')).toBe('https://qm.qq.com/q/o80PFAbey4')
  })

  it('shows the import guidance and QQ group entry while preserving the existing support actions', async () => {
    const w = await panel()
    expect(w.get('[data-testid="quick-start-tagline"]').text()).toContain('dashboard.quickStart.tagline')
    const step3 = w.get('[data-testid="quick-start-step-3"]')
    expect(step3.get('[data-testid="quick-start-step-badge"]').text()).toBe('dashboard.quickStart.step3Badge')
    expect(w.findAll('[data-testid="quick-start-step-badge"]')).toHaveLength(1)
    expect(step3.get('[data-testid="quick-start-step3-hint"]').text()).toBe('dashboard.quickStart.step3Hint')
    expect(step3.get('[data-testid="quick-start-group-lead"]').text()).toContain('dashboard.quickStart.step3GroupLead')
    expect(step3.get('[data-testid="quick-start-group-lead"]').text()).toContain('dashboard.quickStart.step3GroupTail')
    expect(step3.get('[data-testid="quick-start-group-join"]').text()).toContain('dashboard.quickStart.step3GroupLabel')
    expect(step3.get('[data-testid="quick-start-group-join"]').text()).toContain('1020015031')
    expect(step3.get('[data-testid="quick-start-group-join"] img').attributes('src')).toBe('/brand/qq-penguin.png')
    expect(step3.get('[data-testid="quick-start-import-lead"]').text()).toBe('dashboard.quickStart.step3ImportLead')
    expect(step3.get('[data-testid="quick-start-trust"]').text()).toBe('dashboard.quickStart.step3Trust')

    // 原求助入口保留，群文件说明复用同一二维码弹窗。
    expect(step3.find('[data-testid="quick-start-support"]').exists()).toBe(false)
    expect(w.get('[data-testid="quick-start-qq-help-mobile"]').attributes('href')).toBe('https://qm.qq.com/q/o80PFAbey4')
    const dialog = w.findAllComponents({ name: 'QqGroupDialog' }).at(-1)!
    expect(dialog.props('show')).toBe(false)
    await w.get('[data-testid="quick-start-qq-help"]').trigger('click')
    expect(dialog.props('show')).toBe(true)
  })
})

describe('help to keep newcomers', () => {
  it('asks for help from the header and the QQ bar, with the scan button left of the tutorial', async () => {
    const w = await panel()
    const dialog = w.findAllComponents({ name: 'QqGroupDialog' }).at(-1)!
    await w.get('[data-testid="quick-start-header-help"]').trigger('click')
    expect(dialog.props('show')).toBe(true)
    expect(w.get('[data-testid="quick-start-header-help"]').text()).toBe('dashboard.quickStart.headerHelp')
    expect(w.get('[data-testid="quick-start-qq-help"]').text()).toBe('dashboard.quickStart.qqHelp')
    expect(w.get('[data-testid="quick-start-qq-help-mobile"]').text()).toBe('dashboard.quickStart.qqHelp')
    const html = w.html()
    expect(html.indexOf('quick-start-qq-scan')).toBeLessThan(html.indexOf('quick-start-tutorial'))
    expect(w.get('[data-testid="quick-start-qq-scan"]').classes()).not.toContain('btn-sm')
  })
})

describe('rotating tips on the collapsed bar', () => {
  const H = 3600_000
  it('switches every 5 hours by absolute time, so everyone sees the same tip', () => {
    expect(quickStartTips.map((x) => x.id)).toEqual(['invite', 'newChat', 'oneAccount', 'keySafe', 'error'])
    expect(currentTipIndex(0)).toBe(0)
    expect(currentTipIndex(5 * H - 1)).toBe(0)
    expect(currentTipIndex(5 * H)).toBe(1)
    expect(currentTipIndex(4 * 5 * H)).toBe(4)
    expect(currentTipIndex(5 * 5 * H)).toBe(0)
  })

  it('shows the current tip with its action on the collapsed bar only', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    try {
      vi.setSystemTime(0)
      const invite = await collapsedPanel()
      expect(invite.get('[data-testid="quick-start-tip"]').attributes('data-tip')).toBe('invite')
      expect(invite.get('[data-testid="quick-start-tip"]').text()).toContain('dashboard.quickStart.tips.invite')
      expect(invite.get('[data-testid="quick-start-tip-action"]').attributes('data-to')).toBe('/affiliate')
      expect((await panel()).find('[data-testid="quick-start-tip"]').exists()).toBe(false)

      vi.setSystemTime(5 * H)
      expect((await collapsedPanel()).find('[data-testid="quick-start-tip-action"]').exists()).toBe(false)

      vi.setSystemTime(4 * 5 * H)
      const error = await collapsedPanel()
      const dialog = error.findAllComponents({ name: 'QqGroupDialog' }).at(0)!
      await error.get('[data-testid="quick-start-tip-action"]').trigger('click')
      expect(dialog.props('show')).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('newcomer guidance', () => {
  const guided = (w: Awaited<ReturnType<typeof panel>>) =>
    [1, 2, 3, 4].filter((n) => w.get(`[data-testid="quick-start-step-${n}"]`).classes().includes('step-guide'))
  const bubbleIn = (w: Awaited<ReturnType<typeof panel>>, selector: string) =>
    w.get(selector).element.parentElement!.querySelector('[data-testid="guide-bubble"]')

  it('lights up the next unfinished step and points a bubble at what to click', async () => {
    const fresh = await panel()
    expect(guided(fresh)).toEqual([1])
    expect(fresh.findAll('[data-testid="guide-bubble"]')).toHaveLength(1)
    expect(bubbleIn(fresh, '[data-testid="quick-start-create"]')?.textContent).toContain('dashboard.quickStart.guide.step1')

    const noBalance = await panel({ totalApiKeys: 1 })
    expect(guided(noBalance)).toEqual([2])
    expect(bubbleIn(noBalance, '[data-testid="quick-start-reward"]')?.textContent).toContain('dashboard.quickStart.guide.step2')
    // 第一、二步的气泡允许折行，收在卡片内；指引第二步时底部按钮行带上窄卡片重排用的标记
    expect(bubbleIn(fresh, '[data-testid="quick-start-create"]')?.classList.contains('gb-wrap')).toBe(true)
    expect(bubbleIn(noBalance, '[data-testid="quick-start-reward"]')?.classList.contains('gb-wrap')).toBe(true)
    expect(noBalance.get('[data-testid="quick-start-recharge"]').element.parentElement!.classList.contains('step2-foot-guided')).toBe(true)
    expect(fresh.get('[data-testid="quick-start-recharge"]').element.parentElement!.classList.contains('step2-foot-guided')).toBe(false)

    const done = await panel({ totalApiKeys: 1, balance: 3, totalRequests: 2 })
    expect(guided(done)).toEqual([])
    expect(done.find('[data-testid="guide-bubble"]').exists()).toBe(false)
  })

  it('points at one-click import 10 seconds after copying the helper command and restores the existing record', async () => {
    vi.useFakeTimers()
    try {
      const w = await panel({ totalApiKeys: 1, balance: 3 })
      expect(guided(w)).toEqual([3])
      expect(w.find('[data-testid="guide-bubble"]').exists()).toBe(false)
      await w.get('[data-testid="quick-start-helper-command"]').trigger('click')
      await flushPromises()
      vi.advanceTimersByTime(9_000)
      await flushPromises()
      expect(w.find('[data-testid="guide-bubble"]').exists()).toBe(false)
      vi.advanceTimersByTime(1_000)
      await flushPromises()
      expect(bubbleIn(w, '[data-testid="quick-start-import"]')?.textContent).toContain('guide.import')

      // 刷新后直接指一键导入
      const again = await panel({ totalApiKeys: 1, balance: 3 })
      expect(bubbleIn(again, '[data-testid="quick-start-import"]')).not.toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('step 1 opens the keys page with the create dialog', async () => {
    const w = await panel()
    expect(JSON.parse(w.get('[data-testid="quick-start-create"]').attributes('data-to')!)).toEqual({ path: '/keys', query: { create: 'quick' } })
  })
})

describe('PowerShell helper instructions', () => {
  const command = 'irm https://docs.usegoodai.com/install/helper.ps1 | iex'

  it('names the one-click import button and the Windows Run anyway action', () => {
    expect(zh.dashboard.quickStart.step3Hint).toBe('打开助手后，点一下网站一键导入按钮就好（浏览器询问时点「打开」）')
    expect(zh.dashboard.quickStart.step3Trust).toBe('Windows 保护阻止运行时，点「更多信息」→「仍要运行」。')
    expect(en.dashboard.quickStart.step3Trust).toBe('If Windows Security blocks the helper, click "More info" → "Run anyway".')
  })

  it('opens the existing QQ group QR dialog from the inline mascot and colored group number', async () => {
    const w = await panel()
    const dialog = w.findAllComponents({ name: 'QqGroupDialog' }).at(-1)!
    expect(dialog.props('show')).toBe(false)
    await w.get('[data-testid="quick-start-group-join"]').trigger('click')
    expect(dialog.props('show')).toBe(true)
  })

  it('replaces download links with the QQ note and a closed three-step disclosure below the import controls', async () => {
    const w = await panel()
    const step = w.get('[data-testid="quick-start-step-3"]')
    expect(quickConnect).not.toHaveProperty('downloads')
    expect(quickConnect.helperCommand).toBe(command)
    expect(step.find('[data-testid="quick-start-download"]').exists()).toBe(false)
    expect(step.find('[data-testid="quick-start-download-other"]').exists()).toBe(false)
    expect(step.find('[data-testid="quick-start-fallback"]').exists()).toBe(false)
    const help = step.get<HTMLDetailsElement>('[data-testid="quick-start-helper-help"]')
    expect(help.element.open).toBe(false)
    expect(help.findAll('ol > li')).toHaveLength(3)
    expect(help.get('summary').text()).toBe('dashboard.quickStart.step3HelperTitle')
    expect(help.text()).not.toContain('1020015031')
    expect(step.html().indexOf('quick-start-import')).toBeLessThan(step.html().indexOf('quick-start-helper-help'))
    expect(help.element.parentElement?.lastElementChild).toBe(help.element)
    expect(step.get('[data-testid="quick-start-step3-hint"]').element.compareDocumentPosition(help.element) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
    expect(step.get('[data-testid="quick-start-trust"]').element.compareDocumentPosition(help.element) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
    expect(help.get('[data-testid="quick-start-helper-command"] code').text()).toBe(command)
  })

  it.each(['quick-start-helper-command', 'quick-start-helper-copy'])('copies the exact command and toast message when clicking %s', async (testId) => {
    const w = await panel()
    await w.get(`[data-testid="${testId}"]`).trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledTimes(1)
    expect(copyToClipboard).toHaveBeenCalledWith(command, 'dashboard.quickStart.step3HelperCopied')
    expect(localStorage.getItem('quick-start-helper-downloaded-at')).not.toBeNull()
  })

  it('does not advance the helper guidance after a failed copy and allows retrying', async () => {
    copyToClipboard.mockResolvedValueOnce(false)
    const w = await panel()
    await w.get('[data-testid="quick-start-helper-copy"]').trigger('click')
    await flushPromises()
    expect(localStorage.getItem('quick-start-helper-downloaded-at')).toBeNull()
    await w.get('[data-testid="quick-start-helper-command"]').trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledTimes(2)
    expect(localStorage.getItem('quick-start-helper-downloaded-at')).not.toBeNull()
  })

  it('hides PowerShell on Mac while preserving the QQ note, key picker, import and system hint', async () => {
    const userAgent = vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')
    try {
      const w = await panel()
      expect(w.find('[data-testid="quick-start-helper-help"]').exists()).toBe(false)
      expect(w.find('[data-testid="quick-start-group-lead"]').exists()).toBe(true)
      expect(w.get('[data-testid="quick-start-group-join"]').text()).toContain('1020015031')
      expect(w.find('[data-testid="quick-start-key-select"]').exists()).toBe(true)
      expect(w.find('[data-testid="quick-start-import"]').exists()).toBe(true)
      expect(w.get('[data-testid="quick-start-trust"]').text()).toBe('dashboard.quickStart.step3TrustMac')
      expect(w.find('[data-testid="quick-start-fallback"]').exists()).toBe(false)
    } finally {
      userAgent.mockRestore()
    }
  })
})

describe('one-click import without a key', () => {
  const groups = [
    { id: 1, name: 'Claude', platform: 'anthropic', sort_order: 1 },
    { id: 7, name: 'GPT 备用', platform: 'openai', sort_order: 5 },
    { id: 3, name: 'GPT 满血', platform: 'openai', sort_order: 2 }
  ]

  it('creates a test key in the first GPT group, shows it in the picker and continues importing', async () => {
    getAvailable.mockResolvedValue(groups)
    create.mockResolvedValue({ id: 99, name: 'keys.namePlaceholder', key: 'sk-new', status: 'active', created_at: '2026-11-03', group: { id: 3, platform: 'openai' } })
    const location = { href: '' }
    vi.stubGlobal('location', location)
    try {
      const w = await panel()
      await w.get('[data-testid="quick-start-import"]').trigger('click')
      await flushPromises()
      expect(create).toHaveBeenCalledWith('keys.namePlaceholder', 3)
      expect(push).not.toHaveBeenCalled()
      expect(location.href).toBe(buildQuickConnectLink('sk-new', 'keys.namePlaceholder', 'codex'))
      expect(w.get('[data-testid="quick-start-key-select"]').findAll('option').map((o) => o.text())).toEqual(['keys.namePlaceholder'])
      // 建好密钥后第一步也算完成
      expect(done(w, 1)).toBe(true)
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('names the new key after existing test keys and falls back to the keys page without a GPT group', async () => {
    list.mockResolvedValue({ items: [key(5, 'gemini', 'gemini', '2026-10-31'), key(6, 'keys.namePlaceholder', 'gemini', '2026-10-30')] })
    getAvailable.mockResolvedValue(groups)
    create.mockResolvedValue({ id: 100, name: 'x', key: 'sk-x', status: 'active', created_at: '2026-11-03', group: { platform: 'openai' } })
    const location = { href: '' }
    vi.stubGlobal('location', location)
    try {
      const w = await panel({ totalApiKeys: 2 })
      await w.get('[data-testid="quick-start-import"]').trigger('click')
      await flushPromises()
      expect(create).toHaveBeenCalledWith('keys.namePlaceholder1', 3)

      getAvailable.mockResolvedValue([groups[0]])
      create.mockClear()
      const none = await panel()
      await none.get('[data-testid="quick-start-import"]').trigger('click')
      await flushPromises()
      expect(create).not.toHaveBeenCalled()
      expect(push).toHaveBeenCalledWith('/keys')
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('picks names and the default group the same way everywhere', () => {
    expect(nextTestKeyName([], '测试')).toBe('测试')
    expect(nextTestKeyName(['测试', '测试1', ' 测试3 '], '测试')).toBe('测试2')
    expect(pickFirstGptGroup(groups as never)?.id).toBe(3)
    expect(pickFirstGptGroup([groups[0]] as never)).toBeNull()
  })
})

describe('create key picture guide', () => {
  it('opens the picture guide from step 1 whether or not a key exists', async () => {
    for (const totalApiKeys of [0, 1]) {
      const w = await panel({ totalApiKeys })
      const dialog = w.getComponent({ name: 'CreateKeyGuideDialog' })
      expect(dialog.props('show')).toBe(false)
      await w.get('[data-testid="quick-start-step-1"] [data-testid="quick-start-key-guide"]').trigger('click')
      expect(dialog.props('show')).toBe(true)
    }
  })
})

describe('buildQuickConnectLink', () => {
  it('only changes app between Codex and Claude links', () => {
    expect(buildQuickConnectLink('sk-a+b', '测试', 'claude')).toBe('usegoodai://import?v=1&app=claude&key=sk-a%2Bb&name=%E6%B5%8B%E8%AF%95')
    expect(buildQuickConnectLink('sk-x', undefined, 'claude')).toBe('usegoodai://import?v=1&app=claude&key=sk-x')
  })

  it('picks Claude only for anthropic groups and Codex for everything else', () => {
    expect(importAppForGroup({ platform: 'anthropic' })).toBe('claude')
    for (const group of [{ platform: 'openai' }, { platform: 'gemini' }, { platform: 'antigravity' }, { platform: 'composite' }, {}, null, undefined]) {
      expect(importAppForGroup(group)).toBe('codex')
    }
    expect(canQuickImport({ platform: 'openai' })).toBe(true)
    expect(canQuickImport({ platform: 'anthropic' })).toBe(true)
    expect(canQuickImport({ platform: 'gemini' })).toBe(false)
    expect(canQuickImport(null)).toBe(false)
  })

  it('matches the helper link format', () => {
    expect(buildQuickConnectLink('sk-a+b', '测试')).toBe('usegoodai://import?v=1&app=codex&key=sk-a%2Bb&name=%E6%B5%8B%E8%AF%95')
    expect(buildQuickConnectLink('sk-x')).toBe('usegoodai://import?v=1&app=codex&key=sk-x')
  })

  it('RMB quick start balance excludes dollars', async () => {
    const wrapper = await panel({ balance: 12.34 })
    const balance = wrapper.get('[data-testid="quick-start-balance"]').text()
    expect.soft(balance).toBe('¥12.34')
    expect.soft(balance).not.toContain('$')
  })

})
