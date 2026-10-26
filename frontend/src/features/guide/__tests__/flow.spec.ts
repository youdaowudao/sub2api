import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createFlow } from '../flow'
import { beginGuideAction, completeGuideAction } from '../businessEvents'
import { steps } from '../steps'
import { resolveMoreTargets } from '../moreTargets'
import type { GuideModule } from '../config'
import { fakeDriver, installGuideDOM, keyPage, nextGuideStep, settle } from './fixture'

describe('ordinary user guide flows', () => {
  let flow: ReturnType<typeof createFlow>
  const stopped = vi.fn()
  beforeEach(() => { installGuideDOM(); sessionStorage.clear(); history.replaceState({}, '', '/keys'); keyPage(); stopped.mockReset() })
  afterEach(() => { flow?.stop(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
  it('allows a new automatic registration only after the old parent flow is clean', async () => {
    flow = createFlow(fakeDriver, { modules: ['more'] })
    expect(await flow.start({ userId: 7 })).toBe(true)
    expect(flow.resetForRegistration()).toBe(false)
    flow.stop(); expect(flow.clean).toBe(true)
    expect(flow.resetForRegistration()).toBe(true)
    expect(sessionStorage.getItem('s2o:visit-suppressed:v1')).toBeNull()
    expect(await flow.start({ userId: 8 })).toBe(true)
    expect(flow.step).toBe('welcome')
  })
  it('keeps a paused parent registration intact when its display layer is already idle', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    flow = createFlow(fakeDriver, { modules: ['more', 'keys'], getPublicSettings: () => ({}) })
    await flow.start({ userId: 7 }, { manual: true })
    await nextGuideStep(); await nextGuideStep()
    expect(flow.paused).toBe(true)
    expect(flow.resetForRegistration()).toBe(false)
    expect(flow.paused).toBe(true)
    expect(await flow.continue()).toBe(true)
  })
  async function createToSubmit() {
    flow = createFlow(fakeDriver, { modules: ['keys'], onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true, module: 'keys' })
    await nextGuideStep(); expect(flow.step).toBe('name')
    await nextGuideStep(); expect(flow.step).toBe('provider')
    await nextGuideStep(); expect(flow.step).toBe('group')
    await nextGuideStep(); expect(flow.step).toBe('submit')
  }
  async function continueChapter(expected: string) {
    expect(flow.step).toBe('chapter-break'); expect(flow.paused).toBe(true)
    expect(document.querySelector('.s2o-popover, #s2o-exit, #s2o-attention-wait')).toBeNull()
    expect(Object.values(flow.resources.engineSession)).toEqual([0, 0, 0, 0, 0])
    expect(await flow.continue()).toBe(true); await settle(); expect(flow.step).toBe(expected)
  }
  it('allows immediate continuation while business controls work and ignores old success events', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    document.body.insertAdjacentHTML('beforeend', '<input id="practice" value="draft" /><div data-tour="header-balance"></div>')
    flow = createFlow(fakeDriver, { modules: ['more', 'recharge'], getPublicSettings: () => ({ payment_enabled: false }), onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true }); await nextGuideStep(); await nextGuideStep()
    expect(flow.step).toBe('chapter-break')
    const practice = document.querySelector<HTMLInputElement>('#practice')!
    practice.focus(); practice.value = 'user edit'
    history.replaceState({}, '', '/profile'); await vi.advanceTimersByTimeAsync(1000)
    completeGuideAction(beginGuideAction('key-create'), { keyId: 999 }); await settle()
    expect(flow.step).toBe('chapter-break'); expect(practice.value).toBe('user edit')
    expect(document.activeElement).toBe(practice); expect(stopped).not.toHaveBeenCalled()
    const first = flow.continue(), repeated = flow.continue()
    expect(await repeated).toBe(false); expect(await first).toBe(true)
    expect(flow.step).toBe('balance')
    await vi.advanceTimersByTimeAsync(6000); expect(flow.step).toBe('balance')
    flow.stop(); expect(flow.clean).toBe(true)
  })
  it('waits at least five seconds then starts the next chapter exactly once using the existing session tick', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    document.body.insertAdjacentHTML('beforeend', '<div data-tour="header-balance"></div>')
    let boundary = 0
    const shown: string[] = []
    flow = createFlow(fakeDriver, { modules: ['more', 'recharge'], getPublicSettings: () => ({ payment_enabled: false }), onStep: (step: string) => {
      shown.push(step); if (step === 'chapter-break') boundary = Date.now()
    } })
    await flow.start({ userId: 7 }, { manual: true }); await nextGuideStep(); await nextGuideStep()
    expect(flow.step).toBe('chapter-break'); expect(flow.resources.timers).toBe(1)
    await vi.advanceTimersByTimeAsync(4999 - (Date.now() - boundary))
    expect(flow.step).toBe('chapter-break')
    expect(document.querySelector('.s2o-popover')).toBeNull()
    await vi.advanceTimersByTimeAsync(101)
    expect(flow.step).toBe('balance')
    await vi.advanceTimersByTimeAsync(6000)
    expect(shown.filter(step => step === 'balance')).toHaveLength(1)
    flow.stop(); expect(flow.clean).toBe(true)
  })
  it('points to all six real sidebar entries in sequence with an empty right pane', async () => {
    history.replaceState({}, '', '/profile')
    const ids = ['onboarding-quick-start', 'guide-other-page', 'c906a4e429133b3b', '7490c34132b758a2', 'c601441edd4d3c0d']
    const labels = ['邀请返利', '新用户快速开始', '使用教程', '联系客服（新用户福利）', '模型价格', '智商雷达']
    const selectors = ['[data-tour="sidebar-affiliate"]', ...ids.map(id => `[data-tour="sidebar-custom-${id}"]`)]
    document.body.innerHTML = '<aside class="sidebar">' + selectors.map((selector, index) => `<a ${selector.slice(1, -1)}>${labels[index]}</a>`).join('') + '</aside><main></main>'
    const navigate = vi.spyOn(history, 'pushState'), fetch = vi.fn(), open = vi.spyOn(window, 'open')
    vi.stubGlobal('fetch', fetch)
    const factory = vi.fn(fakeDriver)
    flow = createFlow(factory, { modules: ['more'], moduleConfig: [
      { id: 'quick-start', enabled: false, menuId: ids[0] },
      { id: 'more', enabled: true, menuIds: { tutorial: ids[1], support: ids[2], pricing: ids[3], radar: ids[4] } },
    ], getPublicSettings: () => ({ affiliate_enabled: true, model_plaza_enabled: false, custom_menu_items: ids.map((id, i) => ({ id, label: labels[i + 1], visibility: 'user' })) }) })
    expect(await flow.start({ userId: 7 }, { manual: true, module: 'more' })).toBe(true)
    for (const [index, selector] of selectors.entries()) {
      const target = document.querySelector(selector)
      expect(factory.mock.results[0].value.getActiveElement(), labels[index]).toBe(target)
      expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1), labels[index]).toBe(target)
      expect(document.querySelector('.s2o-popover')?.textContent).toContain(labels[index])
      expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('GPT生图')
      if (index === 1) {
        document.querySelector<HTMLButtonElement>('.s2o-driver-popover-prev-btn')!.click(); await settle()
        expect(factory.mock.results[0].value.getActiveElement()).toBe(document.querySelector(selectors[0]))
        await nextGuideStep()
      }
      await nextGuideStep()
    }
    expect(flow.step).toBe('finish')
    await nextGuideStep(); expect(flow.clean).toBe(true)
    expect(document.querySelector('main')?.childElementCount).toBe(0)
    expect(location.pathname).toBe('/profile')
    expect(navigate).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled(); expect(open).not.toHaveBeenCalled()
  })
  it('waits for unknown settings and stops after eight seconds instead of claiming entries are disabled', async () => {
    flow = createFlow(fakeDriver, { modules: ['more'], onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true, module: 'more' })
    expect(flow.step).toBe('more-loading')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('没有可介绍')
    await vi.advanceTimersByTimeAsync(8100)
    expect(stopped).toHaveBeenCalledWith('error:more-settings', 'more-loading')
    expect(flow.clean).toBe(true)
  })
  it('continues when cached settings arrive without making a request', async () => {
    let settings: any = null
    document.body.innerHTML = '<aside class="sidebar"><a data-tour="sidebar-affiliate">邀请返利</a></aside><main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => settings })
    await flow.start({ userId: 7 }, { manual: true, module: 'more' })
    expect(flow.step).toBe('more-loading')
    settings = { affiliate_enabled: true }; await settle()
    expect(flow.step).toBe('more-affiliate')
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('[data-tour="sidebar-affiliate"]'))
  })
  it('opens and reopens the mobile sidebar at the same entry without visiting its page', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) { return this.matches('.s2o-popover') ? new DOMRect(12, 80, 300, 180) : new DOMRect(12, 400, 220, 32) })
    document.body.innerHTML = '<button data-tour="header-menu">菜单</button><aside class="sidebar -translate-x-full"><a data-tour="sidebar-affiliate">邀请返利</a></aside><main></main>'
    const sidebar = document.querySelector('.sidebar')!
    document.querySelector('button')!.onclick = () => sidebar.classList.remove('-translate-x-full')
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => ({ affiliate_enabled: true }) })
    await flow.start({ userId: 7 }, { manual: true, module: 'more' })
    expect(flow.step).toBe('more-open-affiliate')
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('[data-tour="header-menu"]'))
    document.querySelector<HTMLButtonElement>('[data-tour="header-menu"]')!.click(); await settle()
    expect(flow.step).toBe('more-affiliate')
    sidebar.classList.add('-translate-x-full'); await settle()
    expect(flow.step).toBe('more-open-affiliate')
    document.querySelector<HTMLButtonElement>('[data-tour="header-menu"]')!.click(); await settle()
    expect(flow.step).toBe('more-affiliate'); expect(location.pathname).toBe('/keys')
    flow.stop(); expect(flow.clean).toBe(true)
  })
  it.each(['manual', 'continue'])('returns to the same opener when the sidebar closes during the opening wait via %s, then reopens cleanly', async kind => {
    vi.stubGlobal('innerWidth', 390)
    let left = -40
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      if (this.matches('.sidebar')) return new DOMRect(left, 0, 256, 844)
      return this.matches('.s2o-popover') ? new DOMRect(12, 80, 300, 180) : new DOMRect(12, 400, 220, 32)
    })
    document.body.innerHTML = '<button data-tour="header-menu">菜单</button><aside class="sidebar -translate-x-full"><a data-tour="sidebar-affiliate"></a></aside><main></main>'
    const sidebar = document.querySelector('.sidebar')!
    document.querySelector('button')!.onclick = () => sidebar.classList.remove('-translate-x-full')
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => ({ affiliate_enabled: true }), onStop: stopped })
    let continuing: Promise<boolean> | undefined
    if (kind === 'manual') {
      await flow.start({ userId: 7 }, { manual: true, module: 'more' })
      document.querySelector<HTMLButtonElement>('button')!.click(); await settle()
    } else {
      await flow.start({ userId: 7 }, { manual: true, resume: { moduleId: 'more', step: 'chapter-break' } })
      sidebar.classList.remove('-translate-x-full')
      continuing = flow.continue(); await settle()
    }
    expect(flow.step).toBe('more-affiliate') // Target wait is still pending during the slide.
    sidebar.classList.add('-translate-x-full'); await settle()
    expect(flow.step).toBe('more-open-affiliate')
    if (continuing) expect(await continuing).toBe(true)
    expect(stopped).not.toHaveBeenCalled()
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('[data-tour="header-menu"]'))
    left = 0
    document.querySelector<HTMLButtonElement>('[data-tour="header-menu"]')!.click(); await settle()
    expect(flow.step).toBe('more-affiliate')
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('[data-tour="sidebar-affiliate"]'))
    await vi.advanceTimersByTimeAsync(8100)
    expect(flow.active).toBe(true); expect(stopped).not.toHaveBeenCalled()
    flow.stop(); expect(flow.clean).toBe(true)
  })
  it.each(['missing', 'duplicate'])('stops on a %s expected sidebar target instead of presenting a centered substitute', async kind => {
    document.body.innerHTML = kind === 'missing' ? '<main></main>' : '<a data-tour="sidebar-affiliate"></a><a data-tour="sidebar-affiliate"></a><main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => ({ affiliate_enabled: true }), onStop: stopped })
    const pending = flow.start({ userId: 7 }, { manual: true, module: 'more' })
    await vi.advanceTimersByTimeAsync(8100)
    expect(await pending).toBe(false); expect(stopped).toHaveBeenCalledWith('error:more-target', 'more-affiliate')
    expect(document.querySelector('.s2o-popover')).toBeNull(); expect(flow.clean).toBe(true)
  })
  it('cancels a waiting menu target without a late highlight or affecting the next session', async () => {
    document.body.innerHTML = '<main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => ({ affiliate_enabled: true }) })
    const pending = flow.start({ userId: 7 }, { manual: true, module: 'more' })
    await vi.advanceTimersByTimeAsync(100)
    flow.stop(); expect(await pending).toBe(false); expect(flow.clean).toBe(true)
    document.body.insertAdjacentHTML('afterbegin', '<aside class="sidebar"><a data-tour="sidebar-affiliate"></a></aside>')
    expect(await flow.start({ userId: 8 }, { manual: true, module: 'more' })).toBe(true)
    await vi.advanceTimersByTimeAsync(8100); expect(flow.step).toBe('more-affiliate'); expect(flow.active).toBe(true)
  })
  it('keeps ID bindings through renaming and sorting, skips unavailable entries and never guesses a GPT entry', () => {
    const modules: GuideModule[] = [{ id: 'quick-start', enabled: false, menuId: 'quick' }, { id: 'more', enabled: true, menuIds: { tutorial: 'help', support: 'admin', pricing: 'price', radar: 'deleted' } }]
    const settings = { affiliate_enabled: false, model_plaza_enabled: false, custom_menu_items: [{ id: 'price', label: '改过的标题', visibility: 'user' }, { id: 'help', label: '改名', visibility: 'user' }, { id: 'admin', visibility: 'admin' }, { id: 'gpt', label: '智商雷达', visibility: 'user' }, { id: 'quick', visibility: 'user' }] }
    expect(resolveMoreTargets(modules, settings)).toEqual([
      { role: 'quick-start', selector: '[data-tour="sidebar-custom-quick"]' },
      { role: 'tutorial', selector: '[data-tour="sidebar-custom-help"]' },
      { role: 'pricing', selector: '[data-tour="sidebar-custom-price"]' },
    ])
    expect(() => resolveMoreTargets(modules, { ...settings, custom_menu_items: [...settings.custom_menu_items, { id: 'help', visibility: 'user' }] })).toThrow('ambiguous')
    expect(() => resolveMoreTargets(modules, { custom_menu_items: {} })).toThrow('invalid-menu-settings')
    expect(resolveMoreTargets(modules, null)).toBeNull()
    expect(resolveMoreTargets(modules, { custom_menu_items: [] })).toEqual([])
  })
  it('moves to the next available entry when current metadata is removed and restores a selected entry', async () => {
    const modules: GuideModule[] = [{ id: 'more', enabled: true, menuIds: { tutorial: 'help', support: 'contact', pricing: 'price' } }]
    const settings = { custom_menu_items: [{ id: 'help', visibility: 'user' }, { id: 'contact', visibility: 'user' }, { id: 'price', visibility: 'user' }] }
    document.body.innerHTML = '<aside class="sidebar"><a data-tour="sidebar-custom-help"></a><a data-tour="sidebar-custom-contact"></a><a data-tour="sidebar-custom-price"></a></aside><main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], moduleConfig: modules, getPublicSettings: () => settings })
    await flow.start({ userId: 7 }, { manual: true, resume: { moduleId: 'more', step: 'more-support' } })
    expect(flow.step).toBe('more-support')
    settings.custom_menu_items.splice(1, 1); document.querySelector('[data-tour="sidebar-custom-contact"]')!.remove(); await settle()
    expect(flow.step).toBe('more-pricing')
    document.querySelector<HTMLButtonElement>('.s2o-driver-popover-prev-btn')!.click(); await settle()
    expect(flow.step).toBe('more-tutorial')
  })
  it('does not skip an introduction when Next is clicked twice during a transition', async () => {
    document.body.innerHTML = '<aside class="sidebar"><a data-tour="sidebar-affiliate"></a><a data-tour="sidebar-custom-help"></a><a data-tour="sidebar-custom-price"></a></aside><main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], moduleConfig: [{ id: 'more', enabled: true, menuIds: { tutorial: 'help', pricing: 'price' } }], getPublicSettings: () => ({ affiliate_enabled: true, custom_menu_items: [{ id: 'help', visibility: 'user' }, { id: 'price', visibility: 'user' }] }) })
    await flow.start({ userId: 7 }, { manual: true, module: 'more' })
    const next = document.querySelector<HTMLButtonElement>('.s2o-driver-popover-next-btn')!
    next.click(); next.click(); await settle()
    expect(flow.step).toBe('more-tutorial')
  })
  it('honestly reports no available entries without inventing features or navigating', async () => {
    history.replaceState({}, '', '/profile'); document.body.innerHTML = '<main></main>'
    flow = createFlow(fakeDriver, { modules: ['more'], getPublicSettings: () => ({ affiliate_enabled: false, custom_menu_items: [] }), onStop: stopped })
    expect(await flow.start({ userId: 7 }, { manual: true, module: 'more' })).toBe(true)
    expect(flow.step).toBe('more-unavailable')
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('当前没有可介绍的入口')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('邀请返利')
    await nextGuideStep(); expect(flow.step).toBe('finish')
    await nextGuideStep(); expect(stopped).toHaveBeenCalledWith('completed:tour_viewed', 'finish')
    expect(flow.clean).toBe(true); expect(location.pathname).toBe('/profile')
  })
  it('hands creation to the overview without claiming the combined flow has ended', async () => {
    flow = createFlow(fakeDriver, { modules: ['keys', 'more'], getPublicSettings: () => ({}), onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true }); await nextGuideStep()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 72 })
    document.querySelector('[role="dialog"]')!.remove(); await settle()
    expect(flow.step).toBe('review')
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('请确认创建结果')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('结束引导')
    expect(document.querySelector('.s2o-driver-popover-next-btn')?.textContent).toBe('已创建，继续')
    await nextGuideStep(); await continueChapter('more-unavailable')
    await nextGuideStep(); expect(flow.step).toBe('finish')
    expect(stopped).not.toHaveBeenCalled()
    await nextGuideStep(); expect(stopped).toHaveBeenCalledWith('completed:user_confirmed', 'finish')
  })
  it('continues after the overview when another chapter is selected', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    document.body.insertAdjacentHTML('beforeend', '<div data-tour="header-balance"></div>')
    flow = createFlow(fakeDriver, { modules: ['more', 'recharge'], getPublicSettings: () => ({ payment_enabled: false }), onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true })
    expect(flow.step).toBe('welcome')
    await nextGuideStep(); expect(flow.step).toBe('more-unavailable')
    await nextGuideStep()
    expect(flow.moduleId).toBe('recharge'); await continueChapter('balance')
    expect(stopped).not.toHaveBeenCalled()
    await nextGuideStep(); expect(flow.step).toBe('recharge-unavailable')
    await nextGuideStep(); expect(flow.step).toBe('finish')
    await nextGuideStep(); expect(stopped).toHaveBeenCalledOnce(); expect(stopped).toHaveBeenCalledWith('completed:tour_viewed', 'finish')
  })
  it('plays all five selected chapters with one welcome, one finish and the original business confirmations', async () => {
    const visited: string[] = []
    document.body.insertAdjacentHTML('afterbegin', '<a data-tour="sidebar-custom-quick"></a><div data-tour="header-balance"></div>')
    document.querySelector('main')!.insertAdjacentHTML('beforeend', '<div class="s2o-quick-start"><section data-onboarding="windows-step"><div data-onboarding="windows-run"></div><div data-onboarding="windows-command"><textarea id="windows-command" readonly>command</textarea></div></section><section data-onboarding="codex-test"></section><section data-onboarding="models"></section><section data-onboarding="problems"></section><section data-onboarding="support"></section></div>')
    flow = createFlow(fakeDriver, {
      modules: ['keys', 'key-use', 'quick-start', 'recharge', 'more'],
      moduleConfig: [{ id: 'quick-start', menuId: 'quick' }],
      getPublicSettings: () => ({ payment_enabled: false, custom_menu_items: [{ id: 'quick', visibility: 'user' }] }),
      onStep: (step: string) => visited.push(step), onStop: stopped,
    })
    await flow.start({ userId: 7 }, { manual: true }); await nextGuideStep()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 71 })
    document.querySelector('[role="dialog"]')!.remove(); await settle()
    expect(flow.step).toBe('review')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('结束引导')
    await nextGuideStep(); await continueChapter('usage-row')
    for (const expected of ['usage-copy', 'usage-group', 'usage-open', 'usage-edit', 'usage-actions', 'chapter-break']) {
      await nextGuideStep(); expect(flow.step).toBe(expected)
    }
    await continueChapter('quick-nav')
    history.replaceState({}, '', '/custom/quick'); await settle(); expect(flow.step).toBe('quick-command')
    document.querySelector('#windows-command')!.dispatchEvent(new CustomEvent('s2o:command-copied', { bubbles: true })); await settle()
    await nextGuideStep(); await nextGuideStep(); expect(flow.step).toBe('quick-keys-nav')
    history.replaceState({}, '', '/keys'); await settle(); await settle(); expect(flow.step).toBe('quick-key-copy')
    completeGuideAction(beginGuideAction('key-copy', 71), { keyId: 71 }); await settle()
    await nextGuideStep(); await nextGuideStep(); expect(flow.step).toBe('quick-return-nav')
    history.replaceState({}, '', '/custom/quick'); await settle(); expect(flow.step).toBe('quick-test')
    for (const expected of ['quick-prices', 'quick-support', 'chapter-break']) {
      await nextGuideStep(); expect(flow.step).toBe(expected)
    }
    await continueChapter('balance')
    await nextGuideStep(); expect(flow.step).toBe('recharge-unavailable')
    await nextGuideStep(); await continueChapter('more-quick-start')
    await nextGuideStep(); expect(flow.step).toBe('finish')
    expect(visited.filter(step => step === 'welcome')).toHaveLength(1)
    expect(visited.filter(step => step === 'finish')).toHaveLength(1)
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('需要时，点右下角「使用引导」选择章节再次查看。')
    await nextGuideStep()
    expect(stopped).toHaveBeenCalledOnce(); expect(stopped).toHaveBeenCalledWith('completed:user_confirmed', 'finish')
    expect(flow.clean).toBe(true)
  })
  it('cannot skip creation, and a failed or merely submitted request never enters review', async () => {
    await createToSubmit()
    expect(document.querySelector('.s2o-driver-popover-next-btn')).toBeNull()
    expect(steps.submit.showButtons).toEqual(['previous'])
    document.querySelector<HTMLButtonElement>('[data-tour="key-form-submit"]')!.click()
    await settle()
    expect(flow.step).toBe('submit')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('密钥已创建')
    document.querySelector('[role="dialog"]')!.remove(); await settle()
    expect(stopped).toHaveBeenCalledWith('cancelled:dialog-closed', 'submit')
    expect(flow.clean).toBe(true)
  })
  it('registers success during a step transition and consumes it before the dialog-close check', async () => {
    await createToSubmit()
    document.querySelector<HTMLButtonElement>('.s2o-driver-popover-prev-btn')!.click() // Start the async transition.
    const ticket = beginGuideAction('key-create')
    completeGuideAction(ticket, { keyId: 72 })
    document.querySelector('[role="dialog"]')!.remove()
    await settle()
    expect(flow.step).toBe('review')
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('密钥已创建')
    expect(stopped).not.toHaveBeenCalled()
    await nextGuideStep()
    expect(stopped).toHaveBeenCalledWith('completed:user_confirmed', 'review')
  })
  it('does not reuse an earlier creation success after returning to inspect and opening a new form', async () => {
    await createToSubmit()
    const dialog = document.querySelector('[role="dialog"]')!
    completeGuideAction(beginGuideAction('key-create'), { keyId: 72 }); dialog.remove(); await settle()
    expect(flow.step).toBe('review')
    document.querySelector<HTMLButtonElement>('.s2o-driver-popover-prev-btn')!.click(); await settle()
    expect(flow.step).toBe('create')
    document.querySelector('[data-tour="keys-create-btn"]')!.addEventListener('click', () => document.querySelector('main')!.prepend(dialog), { once: true })
    document.querySelector<HTMLButtonElement>('[data-tour="keys-create-btn"]')!.click(); await settle(); await settle()
    expect(flow.step).toBe('name')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('密钥已创建')
  })
  it('hands an explicitly selected two-chapter subset over without another welcome or unrelated chapters', async () => {
    flow = createFlow(fakeDriver, { modules: ['keys', 'key-use', 'quick-start', 'recharge'], registrationModules: ['keys', 'quick-start'], moduleConfig: [{ id: 'quick-start', menuId: 'quick' }], getPublicSettings: () => ({ custom_menu_items: [{ id: 'quick', visibility: 'user' }] }), onStop: stopped })
    document.body.insertAdjacentHTML('afterbegin', '<a data-tour="sidebar-custom-quick"></a>')
    await flow.start({ userId: 7 })
    expect(flow.modules).toEqual(['keys', 'quick-start'])
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('快速了解本站的接入和使用方法')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('→')
    await nextGuideStep(); completeGuideAction(beginGuideAction('key-create'), { keyId: 71 })
    document.querySelector('[role="dialog"]')!.remove(); await settle()
    expect(flow.step).toBe('review')
    expect(document.querySelector('.s2o-driver-popover-next-btn')?.textContent).toBe('已创建，继续')
    await nextGuideStep(); await continueChapter('quick-nav'); expect(flow.moduleId).toBe('quick-start')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('欢迎使用')
  })
  it.each(['unavailable', 'available', 'busy'])('uses the recharge marker %s without touching page state', async marker => {
    history.replaceState({}, '', '/purchase')
    document.body.innerHTML = `<a data-tour="sidebar-my-keys"></a><div data-tour="header-balance"></div><main data-guide-recharge="${marker}"><div data-tour="recharge-amount"></div><div data-tour="recharge-method"></div><button data-tour="recharge-submit" disabled></button></main>`
    flow = createFlow(fakeDriver, { modules: ['recharge'], getPublicSettings: () => ({ payment_enabled: true }), onStop: stopped })
    await flow.start({ userId: 7 }, { manual: true, module: 'recharge' })
    await nextGuideStep(); await nextGuideStep()
    if (marker === 'busy') expect(stopped).toHaveBeenCalledWith('cancelled:recharge-busy', expect.any(String))
    else expect(flow.step).toBe(marker === 'available' ? 'recharge-amount' : 'recharge-unavailable')
    expect(document.querySelector('main')?.getAttribute('data-guide-recharge')).toBe(marker)
    if (marker === 'available') {
      await nextGuideStep(); await nextGuideStep(); expect(flow.step).toBe('recharge-confirm')
      document.querySelector('main')!.setAttribute('data-guide-recharge', 'busy'); await settle()
      expect(stopped).toHaveBeenCalledWith('cancelled:recharge-busy', 'recharge-confirm')
    }
  })
  it('recognizes disabled online payment from cached settings without network requests', async () => {
    document.querySelector('[role="dialog"]')!.remove()
    document.body.insertAdjacentHTML('beforeend', '<div data-tour="header-balance"></div>')
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    flow = createFlow(fakeDriver, { modules: ['recharge'], getPublicSettings: () => ({ payment_enabled: false }) })
    await flow.start({ userId: 7 }, { manual: true, module: 'recharge' }); await nextGuideStep(); await nextGuideStep()
    expect(flow.step).toBe('recharge-unavailable'); expect(fetch).not.toHaveBeenCalled()
  })
  it('completes both quick-start round trips, preselects the newly created key and accepts only its native copy', async () => {
    document.body.insertAdjacentHTML('afterbegin', '<a data-tour="sidebar-custom-quick"></a>')
    document.querySelector('tbody')!.insertAdjacentHTML('beforeend', '<tr data-row-id="72"><td><span data-tour="key-name">New</span></td><td><button data-tour="key-copy"></button><button data-tour="key-group"></button></td><td><div data-tour="key-actions"></div></td></tr>')
    document.querySelector('main')!.insertAdjacentHTML('beforeend', '<div class="s2o-quick-start"><section data-onboarding="windows-step"><div data-onboarding="windows-run"></div><div data-onboarding="windows-command"><textarea id="windows-command" readonly>command</textarea></div></section><section data-onboarding="codex-test"></section><section data-onboarding="models"></section><section data-onboarding="problems"></section><section data-onboarding="support"></section></div>')
    const article = document.querySelector<HTMLElement>('.s2o-quick-start')!
    Object.defineProperty(article, 'scrollTo', { configurable: true, value: vi.fn() })
    flow = createFlow(fakeDriver, { modules: ['keys', 'quick-start'], moduleConfig: [{ id: 'quick-start', menuId: 'quick' }], getPublicSettings: () => ({ custom_menu_items: [{ id: 'quick', visibility: 'user' }] }), onStop: stopped })
    await flow.start({ userId: 7 }); await nextGuideStep()
    completeGuideAction(beginGuideAction('key-create'), { keyId: 72 }); document.querySelector('[role="dialog"]')!.remove(); await settle()
    expect(flow.step).toBe('review'); await nextGuideStep(); await continueChapter('quick-nav')
    history.replaceState({}, '', '/custom/quick'); await settle(); expect(flow.step).toBe('quick-command')
    document.querySelector('#windows-command')!.dispatchEvent(new CustomEvent('s2o:command-copied', { bubbles: true })); await settle()
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('.s2o-driver-popover-next-btn'))
    await nextGuideStep(); expect(flow.step).toBe('quick-wait')
    await settle(); expect(flow.step).toBe('quick-wait') // Focus/time do not confirm external execution.
    await nextGuideStep(); expect(flow.step).toBe('quick-keys-nav')
    history.replaceState({}, '', '/keys'); await settle(); await settle(); expect(flow.step).toBe('quick-key-copy')
    const selectedCopy = document.querySelector('[data-row-id="72"] [data-tour="key-copy"]')
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(selectedCopy)
    completeGuideAction(beginGuideAction('key-copy', 71), { keyId: 71 }); await settle()
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(selectedCopy)
    completeGuideAction(beginGuideAction('key-copy', 72), { keyId: 72 }); await settle()
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(document.querySelector('.s2o-driver-popover-next-btn'))
    await nextGuideStep(); expect(flow.step).toBe('quick-paste-wait')
    await nextGuideStep(); expect(flow.step).toBe('quick-return-nav')
    history.replaceState({}, '', '/custom/quick'); await settle(); expect(flow.step).toBe('quick-test')
    await nextGuideStep(); expect(flow.step).toBe('quick-prices')
    await nextGuideStep(); expect(flow.step).toBe('quick-support')
    await nextGuideStep(); expect(flow.step).toBe('finish')
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('本次引导结束')
    expect(document.querySelector('.s2o-popover')?.textContent).toContain('右下角「使用引导」')
    expect(document.querySelector('.s2o-popover')?.textContent).not.toContain('余额充值')
    vi.mocked(article.scrollTo).mockClear(); await nextGuideStep()
    expect(article.scrollTo).not.toHaveBeenCalled(); expect(flow.clean).toBe(true)
  })
})
