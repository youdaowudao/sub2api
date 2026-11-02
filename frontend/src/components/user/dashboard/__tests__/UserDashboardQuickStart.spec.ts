import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserDashboardQuickStart from '../UserDashboardQuickStart.vue'
import { buildQuickConnectLink } from '@/features/brand/config'

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
    global: { stubs: { Icon: true, QqGroupDialog: true, RouterLink: { props: ['to'], template: '<a :data-to="to"><slot /></a>' } } }
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

  it('imports the newest active OpenAI key by default', async () => {
    list.mockResolvedValue({ items: [
      key(1, '旧', 'openai', '2026-10-01'),
      key(2, '测试', 'openai', '2026-10-30'),
      key(3, 'claude', 'anthropic', '2026-10-31'),
      key(4, '停用', 'openai', '2026-10-29', 'inactive')
    ] })
    const w = await panel({ totalApiKeys: 4 })
    expect(w.get('[data-testid="quick-start-step-1"]').text()).toContain('step1Tip')
    expect(w.get('[data-testid="quick-start-step-1"]').text()).not.toContain('测试')
    const select = w.get('[data-testid="quick-start-key-select"]')
    expect(select.findAll('option').map((o) => o.text())).toEqual(['测试', '旧'])
    expect(w.text()).not.toContain('sk-')

    const location = { href: '' }
    vi.stubGlobal('location', location)
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

    list.mockResolvedValue({ items: [key(3, 'claude', 'anthropic', '2026-10-31')] })
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
})

describe('buildQuickConnectLink', () => {
  it('matches the helper link format', () => {
    expect(buildQuickConnectLink('sk-a+b', '测试')).toBe('usegoodai://import?v=1&app=codex&key=sk-a%2Bb&name=%E6%B5%8B%E8%AF%95')
    expect(buildQuickConnectLink('sk-x')).toBe('usegoodai://import?v=1&app=codex&key=sk-x')
  })
})
