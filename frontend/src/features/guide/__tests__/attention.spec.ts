import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAttention } from '../attention'
import { installGuideDOM } from './fixture'

describe('one guide attention focus', () => {
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
  it('keeps exactly one animation during targets, waiting, composition and reduced motion', () => {
    const animations = installGuideDOM()
    document.body.innerHTML = '<input data-tour="key-form-name" value="" /><div class="s2o-popover"><button class="s2o-driver-popover-next-btn"></button></div>'
    const counter = vi.fn(), failed = vi.fn()
    const attention = createAttention(counter, failed)
    attention.setStep('name'); expect(animations.size).toBe(1)
    const input = document.querySelector<HTMLInputElement>('input')!
    expect(vi.mocked(Element.prototype.animate).mock.instances.at(-1)).toBe(input)
    input.value = 'Ready'; attention.refresh(); expect(animations.size).toBe(1)
    attention.setComposing(true); expect(animations.size).toBe(1)
    attention.setStep(''); expect(animations.size).toBe(1); expect(document.querySelector('#s2o-attention-wait')).not.toBeNull()
    attention.destroy(); expect(animations.size).toBe(0); expect(counter).toHaveBeenLastCalledWith(0); expect(failed).not.toHaveBeenCalled()
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    const reduced = createAttention(); reduced.setStep('welcome')
    expect(vi.mocked(Element.prototype.animate).mock.calls.at(-1)?.[1]).toMatchObject({ duration: 2000, iterations: Infinity })
    reduced.destroy()
  })
  it('uses one inherited animation to outline a complete sticky desktop row', () => {
    const animations = installGuideDOM()
    document.body.innerHTML = '<table><tbody><tr data-row-id="71"><td>Name</td><td>Actions</td></tr></tbody></table>'
    const row = document.querySelector<HTMLElement>('tr')!
    const attention = createAttention()
    attention.setStep('usage-row', { selector: () => row })
    expect(row.classList.contains('s2o-row-attention')).toBe(true)
    expect(animations.size).toBe(1)
    expect(vi.mocked(Element.prototype.animate).mock.calls.at(-1)?.[0]).toEqual(expect.arrayContaining([expect.objectContaining({ '--s2o-row-attention-color': '#047857' })]))
    attention.destroy(); expect(row.classList.contains('s2o-row-attention')).toBe(false)
  })
})
