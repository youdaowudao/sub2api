import { afterEach, describe, expect, it, vi } from 'vitest'

async function freshLocale() {
  vi.resetModules()
  const mod = await import('../index')
  return mod.getLocale()
}

describe('default locale', () => {
  afterEach(() => {
    localStorage.clear()
    vi.unstubAllGlobals()
  })

  it('is Chinese for first-time visitors even with an English browser', async () => {
    vi.stubGlobal('navigator', { ...navigator, language: 'en-US' })
    expect(await freshLocale()).toBe('zh')
  })

  it('keeps a language the visitor chose', async () => {
    localStorage.setItem('sub2api_locale', 'en')
    expect(await freshLocale()).toBe('en')
  })
})
