import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createTourSession } from '../tourSession'
import { driver, getCleanupStats } from '../driver/driver.mjs'
import { createPopoverPosition } from '../popoverPosition'
import { fakeDriver, installGuideDOM, keyPage, settle } from './fixture'

describe('guide exit and resources', () => {
  let tour: ReturnType<typeof createTourSession>
  let animations: Set<any>
  const stopped = vi.fn()
  beforeEach(() => { animations = installGuideDOM(); sessionStorage.clear(); history.replaceState({}, '', '/keys'); keyPage(); stopped.mockReset() })
  afterEach(() => { tour?.stop(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
  it('requires an explicit new registration reset to allow automatic playback after an exit in the same tour', async () => {
    tour = createTourSession(fakeDriver, { onStop: stopped })
    expect(await tour.start(undefined, { id: 'welcome', manual: false })).toBe(true)
    expect(tour.resetForRegistration()).toBe(false)
    tour.stop('cancelled:exit-button')
    sessionStorage.removeItem('s2o:visit-suppressed:v1')
    expect(await tour.start(undefined, { id: 'welcome', manual: false })).toBe(false)
    const count = stopped.mock.calls.length
    expect(tour.resetForRegistration()).toBe(true)
    expect(stopped).toHaveBeenCalledTimes(count)
    expect(await tour.start(undefined, { id: 'welcome', manual: false })).toBe(true)
    tour.stop(); expect(tour.clean).toBe(true)
  })
  it('preserves suppression when the new registration reset cannot clear storage', async () => {
    tour = createTourSession(fakeDriver)
    await tour.start(undefined, { id: 'welcome', manual: false }); tour.stop()
    const remove = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new DOMException('Denied') })
    try { expect(tour.resetForRegistration()).toBe(false) } finally { remove.mockRestore() }
    sessionStorage.removeItem('s2o:visit-suppressed:v1')
    expect(await tour.start(undefined, { id: 'welcome', manual: false })).toBe(false)
  })
  it('pauses the real Driver without suppression or returning to the old scroll position, then can start again', async () => {
    const input = document.querySelector<HTMLInputElement>('[data-tour="key-form-name"]')!
    const container = input.parentElement!
    Object.defineProperty(container, 'scrollTo', { configurable: true, value: vi.fn((x: number, y: number) => { container.scrollLeft = x; container.scrollTop = y }) })
    input.focus()
    tour = createTourSession(driver, { onStop: stopped })
    await tour.start(() => input, { id: 'name', manual: false }); await settle()
    document.querySelector<HTMLButtonElement>('.s2o-driver-popover-next-btn')!.focus()
    container.scrollTop = 240
    vi.stubGlobal('scrollY', 550)
    vi.spyOn(input, 'focus').mockImplementation(() => { container.scrollTop = 0; window.scrollTo(0, 0) })
    expect(tour.pause()).toBe(true)
    expect(stopped).toHaveBeenCalledWith('paused:chapter')
    expect(sessionStorage.getItem('s2o:visit-suppressed:v1')).toBeNull()
    expect(container.scrollTop).toBe(240)
    expect(window.scrollTo).toHaveBeenLastCalledWith(0, 550)
    expect(getCleanupStats()).toEqual({ listeners: 0, frames: 0, targets: 0 })
    expect(tour.clean).toBe(true); expect(animations.size).toBe(0)
    expect(document.querySelector('.s2o-driver-overlay, .s2o-popover, #s2o-exit')).toBeNull()
    const escape = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true, bubbles: true })
    window.dispatchEvent(escape); expect(escape.defaultPrevented).toBe(false)
    expect(await tour.start(undefined, { id: 'welcome', manual: false })).toBe(true)
    tour.stop('cancelled'); expect(tour.clean).toBe(true)
  })
  it.each(['independent', 'nearby', 'escape'])('cleans listeners, timers, frames, observers, animation and temporary nodes on %s exit', async method => {
    tour = createTourSession(fakeDriver, { onStop: stopped })
    await tour.start('[data-tour="key-form-name"]', { id: 'name' })
    expect(animations.size).toBe(1)
    if (method === 'independent') document.querySelector<HTMLButtonElement>('#s2o-exit')!.click()
    else if (method === 'nearby') document.querySelector<HTMLButtonElement>('.s2o-popover-exit')!.click()
    else window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(stopped).toHaveBeenCalledOnce()
    expect(tour.clean).toBe(true)
    expect(Object.values(tour.resources)).toEqual([0, 0, 0, 0, 0])
    expect(animations.size).toBe(0)
    expect(document.querySelector('#s2o-exit, #s2o-attention-wait, .s2o-popover')).toBeNull()
  })
  it('keeps IME Escape native, preserves a draft, then allows Escape after composition ends', async () => {
    tour = createTourSession(fakeDriver)
    const input = document.querySelector<HTMLInputElement>('[data-tour="key-form-name"]')!
    input.value = '未完成草稿'
    await tour.start(input ? () => input : '', { id: 'name' })
    document.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
    const candidateEscape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    window.dispatchEvent(candidateEscape)
    expect(candidateEscape.defaultPrevented).toBe(false); expect(tour.state).toBe('active')
    document.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(tour.clean).toBe(true); expect(input.value).toBe('未完成草稿')
  })
  it('bounds missing targets to eight seconds and cancels a pending wait immediately', async () => {
    tour = createTourSession(fakeDriver, { onStop: stopped })
    const pending = tour.start('[data-tour="missing"]', { id: 'missing', timeout: 60_000 })
    await vi.advanceTimersByTimeAsync(8000)
    expect(await pending).toBe(false); expect(stopped).toHaveBeenCalledWith('error:target-timeout'); expect(tour.clean).toBe(true)
    const next = tour.start('[data-tour="missing"]', { id: 'missing' })
    tour.stop('cancelled:exit-button')
    expect(await next).toBe(false); expect(tour.clean).toBe(true)
  })
  it('keeps a narrow recharge guide clear of native tabs, its amount card and the independent exit', () => {
    vi.stubGlobal('innerWidth', 390); vi.stubGlobal('innerHeight', 844)
    document.body.innerHTML = '<div data-tour="recharge-tabs"><button id="topup">充值</button><button id="subscription">订阅</button></div><div id="amount"></div><button id="s2o-exit">退出引导</button><div class="s2o-popover"></div>'
    const wrapper = document.querySelector<HTMLElement>('.s2o-popover')!
    const geometry: Record<string, [number, number, number, number]> = { topup: [17,85,170,40], subscription: [193,85,169,40], amount: [17,260,345,370], 's2o-exit': [12,789,84,43] }
    vi.mocked(Element.prototype.getBoundingClientRect).mockImplementation(function (this: Element) {
      return new DOMRect(...(this === wrapper ? [12,13,366,234] as const : geometry[this.id] || [0,0,0,0]))
    })
    for (const key of ['offsetHeight','scrollHeight','clientHeight']) Object.defineProperty(wrapper, key, { configurable: true, value: 234 })
    const failed = vi.fn(), position = createPopoverPosition(failed)
    position.set(wrapper, { fallback: document.querySelector('#amount'), side: 'top' })
    const left = Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-x'))
    const top = Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-y'))
    const height = Math.min(234, Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-max-height')))
    expect(Number.isFinite(top)).toBe(true)
    for (const [x,y,width,h] of Object.values(geometry)) {
      expect(left < x + width && left + 366 > x && top < y + h && top + height > y).toBe(false)
    }
    expect(top).toBeGreaterThanOrEqual(12); expect(top + height).toBeLessThanOrEqual(832)
    expect(wrapper.hasAttribute('data-s2o-position-waiting')).toBe(false)
    expect(failed).not.toHaveBeenCalled(); position.destroy()
  })
  it('can place the balance introduction beyond native purchase tabs when every adjacent position is blocked', () => {
    document.body.innerHTML = '<div data-tour="recharge-tabs"><button id="topup">充值</button><button id="subscription">订阅</button></div><span id="balance"></span><button id="s2o-exit">退出引导</button><div class="s2o-popover"></div>'
    const wrapper = document.querySelector<HTMLElement>('.s2o-popover')!
    const geometry: Record<string, [number, number, number, number]> = { topup: [366.5,101,442,40], subscription: [812.5,101,442,40], balance: [1085,16,91,32], 's2o-exit': [12,845,84,43] }
    vi.mocked(Element.prototype.getBoundingClientRect).mockImplementation(function (this: Element) {
      return new DOMRect(...(this === wrapper ? [955,63,400,234] as const : geometry[this.id] || [0,0,0,0]))
    })
    for (const key of ['offsetHeight','scrollHeight','clientHeight']) Object.defineProperty(wrapper, key, { configurable: true, value: 234 })
    const failed = vi.fn(), position = createPopoverPosition(failed)
    position.set(wrapper, { fallback: document.querySelector('#balance'), side: 'bottom' })
    expect(wrapper.hasAttribute('data-s2o-position-waiting')).toBe(false)
    const left = Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-x'))
    const top = Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-y'))
    const height = Math.min(234, Number.parseFloat(wrapper.style.getPropertyValue('--s2o-popover-max-height')))
    for (const [x,y,width,h] of Object.values(geometry)) {
      expect(left < x + width && left + 400 > x && top < y + h && top + height > y).toBe(false)
    }
    expect(top).toBeGreaterThanOrEqual(147); expect(top + height).toBeLessThanOrEqual(888)
    expect(failed).not.toHaveBeenCalled(); position.destroy()
  })
  it('retains the prototype startup check for an existing unrelated Driver', async () => {
    document.body.insertAdjacentHTML('beforeend', '<div class="driver-overlay"></div>')
    tour = createTourSession(fakeDriver)
    expect(await tour.start(undefined, { id: 'welcome' })).toBe(false)
    expect(tour.clean).toBe(true); expect(document.querySelector('#s2o-exit')).toBeNull()
  })
  it('restores all scroll on cancellation and can retain only the article scroll on successful completion', async () => {
    document.body.insertAdjacentHTML('beforeend', '<div class="s2o-quick-start"><p data-tour="article-target"></p></div>')
    const article = document.querySelector<HTMLElement>('.s2o-quick-start')!
    Object.defineProperty(article, 'scrollTo', { configurable: true, value: vi.fn() })
    tour = createTourSession(fakeDriver)
    await tour.start('[data-tour="article-target"]', { id: 'quick-test' })
    tour.stop('cancelled')
    expect(article.scrollTo).toHaveBeenCalled()
    vi.mocked(article.scrollTo).mockClear()
    await tour.start('[data-tour="article-target"]', { id: 'quick-test' })
    tour.preserveQuickStartScroll(); tour.stop('completed:tour_viewed')
    expect(article.scrollTo).not.toHaveBeenCalled()
  })
  it('runs the real private Driver and restores borrowed attributes without touching host listeners', async () => {
    const input = document.querySelector<HTMLInputElement>('[data-tour="key-form-name"]')!
    input.setAttribute('aria-controls', 'original-list'); input.setAttribute('aria-expanded', 'false')
    const hostListener = vi.fn(); window.addEventListener('resize', hostListener)
    tour = createTourSession(driver)
    await tour.start('[data-tour="key-form-name"]', { id: 'name' }); await settle()
    expect(document.querySelector('.s2o-driver-popover')).not.toBeNull()
    tour.stop()
    expect(getCleanupStats()).toEqual({ listeners: 0, frames: 0, targets: 0 })
    expect(input.getAttribute('aria-controls')).toBe('original-list'); expect(input.getAttribute('aria-expanded')).toBe('false')
    expect(document.querySelector('.s2o-driver-overlay, .s2o-driver-popover')).toBeNull()
    window.dispatchEvent(new Event('resize')); expect(hostListener).toHaveBeenCalled()
    window.removeEventListener('resize', hostListener)
    expect(tour.clean).toBe(true)
  })
})
