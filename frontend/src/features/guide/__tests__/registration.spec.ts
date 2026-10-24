import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { onGuideRegistration, readGuideRegistration, recordGuideRegistration, registrationKey } from '../registration'
import { FLOW_VERSION } from '../steps'

describe('guide registration records', () => {
  beforeEach(() => { sessionStorage.clear(); vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-24T15:00:00Z')) })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })
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
