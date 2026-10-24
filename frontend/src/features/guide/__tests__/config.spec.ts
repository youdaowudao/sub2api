import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enabledModules, normalizeConfig, registrationModules } from '../config'
import bundled from '../config/onboarding.json'

describe('guide configuration', () => {
  beforeEach(() => { localStorage.clear(); history.replaceState({}, '', '/dashboard'); vi.resetModules() })
  afterEach(() => { history.replaceState({}, '', '/dashboard'); localStorage.clear(); vi.doUnmock('../config/onboarding.json') })
  it('validates the shipped configuration and limits automatic playback to keys then quick start', () => {
    expect(normalizeConfig(bundled)).toEqual(bundled)
    expect(registrationModules(bundled)).toEqual(['keys', 'quick-start'])
    expect(enabledModules(bundled)).toEqual(['keys', 'key-use', 'quick-start', 'recharge'])
    expect(registrationModules({ schemaVersion: 1, modules: [{ id: 'key-use', enabled: true }, { id: 'recharge', enabled: true }] })).toEqual([])
  })
  it.each([
    null, { ...bundled, extra: true }, { ...bundled, schemaVersion: 2 },
    { schemaVersion: 1, modules: [{ id: 'unknown', enabled: true }] },
    { schemaVersion: 1, modules: [{ id: 'keys', enabled: true }, { id: 'keys', enabled: false }] },
    { schemaVersion: 1, modules: [{ id: 'keys', enabled: 'false' }] },
    { schemaVersion: 1, modules: [{ id: 'quick-start', enabled: true }] },
    { schemaVersion: 1, modules: [{ id: 'quick-start', enabled: true, menuId: 'x'.repeat(33) }] },
    { schemaVersion: 1, modules: [{ id: 'quick-start', enabled: false, menuId: '../private' }] },
    { schemaVersion: 1, modules: [{ id: 'keys', enabled: true, menuId: 'other' }] },
  ])('rejects invalid or ambiguous configuration %j', value => { expect(() => normalizeConfig(value)).toThrow() })
  it('binds the article even with the quick-start chapter disabled or the tour opted out', async () => {
    const draft = { schemaVersion: 1, modules: [{ id: 'quick-start', enabled: false, menuId: 'local-doc' }] }
    localStorage.setItem('s2o:configuration-preview:v1', JSON.stringify(draft))
    history.replaceState({}, '', '/dashboard?onboarding-preview=1&onboarding=off')
    const config = await import('../config')
    expect(config.boundQuickStartMenuId()).toBe('local-doc')
    expect(config.initialOptOut).toBe(true)
    localStorage.clear(); history.replaceState({}, '', '/dashboard')
    expect(config.boundQuickStartMenuId()).toBe('local-doc') // Read once per document.
  })
  it('ignores a browser draft without the preview query and falls back from an invalid draft', async () => {
    localStorage.setItem('s2o:configuration-preview:v1', '{}')
    const normal = await import('../config')
    expect(normal.configuration).toEqual(bundled)
    expect(normal.configPreview).toBe(false)
    vi.resetModules(); history.replaceState({}, '', '/dashboard?onboarding-preview=1')
    const preview = await import('../config')
    expect(preview.configuration).toEqual(bundled)
    expect(preview.configPreview).toBe(false)
  })
  it('disables an invalid bundled configuration without throwing out of application startup', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.doMock('../config/onboarding.json', () => ({ default: { schemaVersion: 1, modules: [{ id: 'unknown', enabled: true }] } }))
    try {
      const invalid = await import('../config')
      expect(invalid.configuration).toBeNull()
      expect(invalid.configPreview).toBe(false)
      expect(invalid.boundQuickStartMenuId()).toBeUndefined()
      expect(warn).toHaveBeenCalledWith('新手引导配置无效，引导已关闭。')
    } finally { warn.mockRestore() }
  })
})
