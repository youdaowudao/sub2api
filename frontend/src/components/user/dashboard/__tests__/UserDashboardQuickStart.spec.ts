import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserDashboardQuickStart from '../UserDashboardQuickStart.vue'
import { buildQuickConnectLink, canQuickImport, importAppForGroup } from '@/features/brand/config'

const { list, copyToClipboard, settings, push } = vi.hoisted(() => ({
  push: vi.fn(),
  list: vi.fn(),
  copyToClipboard: vi.fn().mockResolvedValue(true),
  settings: { value: { payment_enabled: true } as { payment_enabled: boolean } | null }
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ get cachedPublicSettings() { return settings.value } }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key) })
}))

enableAutoUnmount(afterEach)

const key = (id: number, name: string, platform: string, created_at: string, status = 'active') => ({
  id, name, key: `sk-${id}`, status, created_at, group: { platform }
})

async function panel(props: { totalApiKeys?: number; totalRequests?: number; balance?: number } = {}) {
  const w = mount(UserDashboardQuickStart, {
    props: { totalApiKeys: 0, totalRequests: 0, balance: 0, ...props },
    global: { stubs: { Icon: true, QqGroupDialog: true, CreateKeyGuideDialog: true, RouterLink: { props: ['to'], template: '<a :data-to="to"><slot /></a>' } } }
  })
  await flushPromises()
  return w
}

const done = (w: Awaited<ReturnType<typeof panel>>, step: number) =>
  w.get(`[data-testid="quick-start-step-${step}"] [data-done]`).attributes('data-done') === 'true'

beforeEach(() => {
  vi.clearAllMocks()
  settings.value = { payment_enabled: true }
  list.mockResolvedValue({ items: [] })
})

describe('quick start panel', () => {
  it('stays expanded up to 10 usage records and collapses after that', async () => {
    expect((await panel({ totalRequests: 0 })).find('[data-testid="quick-start"]').exists()).toBe(true)
    expect((await panel({ totalRequests: 10 })).find('[data-testid="quick-start"]').exists()).toBe(true)
    const old = await panel({ totalRequests: 11 })
    expect(old.find('[data-testid="quick-start"]').exists()).toBe(false)
    expect(old.find('[data-testid="quick-start-collapsed"]').exists()).toBe(true)
    await old.get('[data-testid="quick-start-expand"]').trigger('click')
    expect(old.find('[data-testid="quick-start"]').exists()).toBe(true)
  })

  it('collapses on click for this visit only', async () => {
    const w = await panel()
    await w.get('[data-testid="quick-start-collapse"]').trigger('click')
    expect(w.find('[data-testid="quick-start-collapsed"]').exists()).toBe(true)
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
    expect(w.text()).not.toContain('sk-')

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
    expect(none.get('[data-testid="quick-start-fallback"]').attributes('href')).toContain('docs.usegoodai.com/quick-start#')
  })

  it('copies the QQ number on desktop and links to the group on mobile', async () => {
    const w = await panel()
    await w.get('[data-testid="quick-start-qq-copy"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('1020015031', 'community.copied')
    expect(w.get('[data-testid="quick-start-qq-mobile"]').attributes('href')).toBe('https://qm.qq.com/q/o80PFAbey4')
  })

  it('promises one-click import in the header and step 3, with free help only in the QQ bar', async () => {
    const w = await panel()
    expect(w.get('[data-testid="quick-start-tagline"]').text()).toContain('dashboard.quickStart.tagline')
    const step3 = w.get('[data-testid="quick-start-step-3"]')
    expect(step3.get('[data-testid="quick-start-step-badge"]').text()).toBe('dashboard.quickStart.step3Badge')
    expect(w.findAll('[data-testid="quick-start-step-badge"]')).toHaveLength(1)
    expect(step3.get('[data-testid="quick-start-step3-hint"]').text()).toBe('dashboard.quickStart.step3Hint:{"app":"Codex"}')

    // 第 3 步不再放进群链接，求助入口只留在底部 QQ 群栏
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
})
