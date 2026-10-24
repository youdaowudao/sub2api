import { vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'

export function installGuideDOM() {
  vi.useFakeTimers()
  vi.stubGlobal('innerWidth', 1365); vi.stubGlobal('innerHeight', 900)
  vi.stubGlobal('scrollX', 0); vi.stubGlobal('scrollY', 0)
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() })
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const popover = this.matches('.s2o-popover')
    return new DOMRect(popover ? 900 : 200, popover ? 80 : 400, popover ? 400 : 160, popover ? 180 : 32)
  })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(180)
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(180)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(180)
  const animations = new Set<any>()
  Object.defineProperty(Element.prototype, 'animate', { configurable: true, value: vi.fn(() => {
    const animation = { id: '', playState: 'running', cancel: vi.fn(() => { animation.playState = 'idle'; animations.delete(animation) }) }
    animations.add(animation); return animation
  }) })
  return animations
}

export function fakeDriver(options: any) {
  let wrapper: HTMLElement | null = null, active: any
  function render(step: any) {
    active = step
    wrapper?.remove()
    wrapper = document.createElement('div'); wrapper.className = 's2o-popover s2o-driver-popover'
    const title = document.createElement('h2'); title.textContent = step.popover.title; wrapper.append(title)
    const description = document.createElement('p'); description.textContent = step.popover.description; wrapper.append(description)
    for (const type of step.popover.showButtons || options.showButtons || ['next']) {
      const button = document.createElement('button'); button.className = `s2o-driver-popover-${type === 'previous' ? 'prev' : 'next'}-btn`
      button.textContent = type === 'previous' ? step.popover.prevBtnText : step.popover.nextBtnText
      button.onclick = () => type === 'previous' ? options.onPrevClick() : options.onNextClick()
      wrapper.append(button)
    }
    document.body.append(wrapper)
    options.onPopoverRender?.({ wrapper })
  }
  return {
    drive() { render(options.steps[0]) }, highlight: render, refresh() {},
    destroy() { wrapper?.remove(); wrapper = null }, setConfig() {}, getActiveElement: () => active?.element,
  }
}
export async function settle() { await flushPromises(); await vi.advanceTimersByTimeAsync(110); await flushPromises() }
export async function nextGuideStep() {
  const button = document.querySelector<HTMLButtonElement>('.s2o-driver-popover-next-btn')
  if (!button) throw new Error('Guide Next is not available')
  button.click(); await settle()
}
export function keyPage() {
  document.body.innerHTML = `<a data-tour="sidebar-my-keys"></a><main>
    <button data-tour="keys-create-btn"></button>
    <div role="dialog" aria-modal="true"><div class="modal-content">
      <div class="modal-header"><button aria-label="Close modal"></button></div>
      <form id="key-form"><input data-tour="key-form-name" value="My key" />
        <div data-tour="key-form-provider"><label><input value="openai" type="radio" checked /><span>OpenAI</span></label></div>
        <div data-tour="key-form-group"><button aria-expanded="false"><span class="select-value"><span><span>Group</span></span></span></button></div>
        <div data-tour="key-form-quota"><input type="number" value="0" /></div>
      </form><div class="modal-footer"><button type="button" data-tour="key-form-submit" form="key-form">Create</button></div>
    </div></div>
    <div class="table-wrapper"><table><tbody><tr data-row-id="71"><td><span data-tour="key-name">Test</span></td><td><button data-tour="key-copy"></button><button data-tour="key-group"></button></td><td><div data-tour="key-actions"><button data-tour="key-use"></button><button data-tour="key-edit"></button></div></td></tr></tbody></table></div>
  </main>`
}
