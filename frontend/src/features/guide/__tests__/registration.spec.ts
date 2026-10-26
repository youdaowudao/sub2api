import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearRegistrationProgress, historyKey, onGuideRegistration, progressKey, readGuideRegistration, recordGuideRegistration, registrationKey, suppressedKey } from '../registration'
import { FLOW_VERSION } from '../steps'

describe('guide registration records', () => {
  beforeEach(() => { sessionStorage.clear(); vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-24T15:00:00Z')) })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })
  it('opens a new registration boundary before notifying the existing host', () => {
    sessionStorage.setItem(suppressedKey, '1'); sessionStorage.setItem(progressKey, 'old progress')
    sessionStorage.setItem(historyKey, 'old history')
    const observed: unknown[] = []
    const dispose = onGuideRegistration(() => { observed.push({ userId: readGuideRegistration(42)?.userId, suppressed: sessionStorage.getItem(suppressedKey), progress: sessionStorage.getItem(progressKey) }) })
    try {
      recordGuideRegistration({ id: 42, role: 'user' })
      expect(sessionStorage.getItem(suppressedKey)).toBeNull()
      expect(sessionStorage.getItem(progressKey)).toBeNull()
      expect(sessionStorage.getItem(historyKey)).toBe('old history')
      expect(observed).toEqual([{ userId: 42, suppressed: null, progress: null }])
    } finally { dispose() }
  })
  it('keeps the old boundary when the user is ineligible or the new record cannot be saved', () => {
    sessionStorage.setItem(suppressedKey, '1'); sessionStorage.setItem(progressKey, 'old progress')
    recordGuideRegistration({ id: 42, role: 'admin' })
    const notify = vi.fn(), dispose = onGuideRegistration(notify)
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Denied') })
    try { recordGuideRegistration({ id: 42, role: 'user' }) } finally { set.mockRestore(); dispose() }
    expect(notify).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(suppressedKey)).toBe('1')
    expect(sessionStorage.getItem(progressKey)).toBe('old progress')
  })
  it.each([7, 8])('cleans only the old registration while preserving a newer record for user %s', newUserId => {
    const old = { userId: 7, registrationId: 'a'.repeat(32) }
    const next = { version: FLOW_VERSION, userId: newUserId, registrationId: 'b'.repeat(32), expiresAt: Date.now() + 10_000 }
    sessionStorage.setItem(registrationKey, JSON.stringify(next))
    sessionStorage.setItem(progressKey, JSON.stringify(old))
    clearRegistrationProgress(old)
    expect(JSON.parse(sessionStorage.getItem(registrationKey)!)).toEqual(next)
    expect(sessionStorage.getItem(progressKey)).toBeNull()
    clearRegistrationProgress({ userId: newUserId })
    expect(sessionStorage.getItem(registrationKey)).toBeNull()
  })
  it('does not delete either record when storage cannot be read to verify its owner', () => {
    const remove = vi.spyOn(Storage.prototype, 'removeItem')
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('Denied') })
    clearRegistrationProgress({ userId: 7 })
    expect(remove).not.toHaveBeenCalled()
  })
  it('records only ordinary signup results, with a random identity and fixed 30 minute deadline', () => {
    const registered = vi.fn(); const dispose = onGuideRegistration(registered)
    recordGuideRegistration({ id: 1, role: 'admin' })
    expect(readGuideRegistration(1)).toBeNull()
    recordGuideRegistration({ id: 42, role: 'user' })
    const record = readGuideRegistration(42)!
    expect(record).toEqual({ version: FLOW_VERSION, userId: 42, registrationId: expect.stringMatching(/^[a-f0-9]{32}$/), expiresAt: Date.now() + 1_800_000 })
    expect(registered).toHaveBeenCalledOnce()
    vi.advanceTimersByTime(60_000)
    expect(readGuideRegistration(42)?.expiresAt).toBe(record.expiresAt)
    expect(readGuideRegistration(43)).toBeNull()
    expect(sessionStorage.getItem(registrationKey)).toBeNull()
    dispose()
  })
  it('expires at exactly 30 minutes without extending the deadline', () => {
    recordGuideRegistration({ id: 42, role: 'user' })
    vi.advanceTimersByTime(1_799_999); expect(readGuideRegistration(42)).not.toBeNull()
    vi.advanceTimersByTime(1); expect(readGuideRegistration(42)).toBeNull()
  })
  it('swallows storage errors and callback errors so registration can still navigate', () => {
    const dispose = onGuideRegistration(() => { throw new Error('guide listener failed') })
    expect(() => recordGuideRegistration({ id: 42, role: 'user' })).not.toThrow()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Denied') })
    expect(() => recordGuideRegistration({ id: 42, role: 'user' })).not.toThrow()
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('Denied') })
    expect(readGuideRegistration(42)).toBeNull()
    dispose()
  })
})
