import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createStreams } from '../streams'

function fakeContext() {
  const gradient = { addColorStop: vi.fn() }
  return new Proxy(
    {
      createLinearGradient: () => gradient,
      createRadialGradient: () => gradient,
      measureText: () => ({ width: 40 })
    } as Record<string, unknown>,
    {
      get(target, key) {
        if (!(key in target)) target[key as string] = vi.fn()
        return target[key as string]
      },
      set(target, key, value) {
        target[key as string] = value
        return true
      }
    }
  ) as unknown as CanvasRenderingContext2D
}

function makeCanvas(ctx: CanvasRenderingContext2D | null) {
  const canvas = document.createElement('canvas')
  vi.spyOn(canvas, 'getContext').mockReturnValue(ctx as never)
  vi.spyOn(canvas, 'getBoundingClientRect').mockReturnValue({
    left: 0, top: 0, width: 1200, height: 800, right: 1200, bottom: 800, x: 0, y: 0, toJSON: () => ({})
  } as DOMRect)
  return canvas
}

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  document.dispatchEvent(new Event('visibilitychange'))
}

function setReducedMotion(reduce: boolean) {
  vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: reduce } as MediaQueryList)
}

describe('createStreams', () => {
  let rafCallbacks: FrameRequestCallback[]
  let raf: ReturnType<typeof vi.fn>

  beforeEach(() => {
    rafCallbacks = []
    raf = vi.fn((cb: FrameRequestCallback) => {
      rafCallbacks.push(cb)
      return rafCallbacks.length
    })
    vi.stubGlobal('requestAnimationFrame', raf)
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const runFrame = (ts: number) => {
    const cb = rafCallbacks.shift()
    cb?.(ts)
  }

  it('returns a harmless handle when the 2D context is unavailable', () => {
    setReducedMotion(false)
    const handle = createStreams(makeCanvas(null), { focal: () => null })

    expect(() => {
      handle.refresh()
      handle.destroy()
    }).not.toThrow()
    expect(raf).not.toHaveBeenCalled()
  })

  it('draws a single still frame without an animation loop when reduced motion is requested', () => {
    setReducedMotion(true)
    const ctx = fakeContext()
    const handle = createStreams(makeCanvas(ctx), { focal: () => null, labels: true })

    expect(raf).not.toHaveBeenCalled()
    expect(ctx.clearRect).toHaveBeenCalled()
    handle.destroy()
  })

  it('stops scheduling frames while the page is hidden and resumes when visible', () => {
    setReducedMotion(false)
    const handle = createStreams(makeCanvas(fakeContext()), { focal: () => null })
    expect(raf).toHaveBeenCalledTimes(1)

    runFrame(16)
    expect(raf).toHaveBeenCalledTimes(2)

    setHidden(true)
    runFrame(32)
    expect(raf).toHaveBeenCalledTimes(2)

    setHidden(false)
    expect(raf).toHaveBeenCalledTimes(3)
    handle.destroy()
  })

  it('cancels the animation and removes listeners on destroy', () => {
    setReducedMotion(false)
    const removeWindow = vi.spyOn(window, 'removeEventListener')
    const removeDocument = vi.spyOn(document, 'removeEventListener')
    const handle = createStreams(makeCanvas(fakeContext()), { focal: () => null })

    handle.destroy()

    expect(cancelAnimationFrame).toHaveBeenCalled()
    expect(removeWindow).toHaveBeenCalledWith('resize', expect.any(Function))
    expect(removeDocument).toHaveBeenCalledWith('visibilitychange', expect.any(Function))

    setHidden(false)
    runFrame(16)
    expect(raf).toHaveBeenCalledTimes(1)
  })
})
