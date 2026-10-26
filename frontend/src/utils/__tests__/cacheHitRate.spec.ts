import { describe, expect, it } from 'vitest'
import { getCacheHitRateRangeDates } from '../dateRange'
import { formatCacheHitRate } from '../format'

describe('cache rate display and local calendar dates', () => {
  it.each([
    ['today', '2026-11-01', '2026-11-01'],
    ['yesterday', '2026-10-31', '2026-10-31'],
    ['yesterdayAndToday', '2026-10-31', '2026-11-01'],
    ['invalid', '2026-11-01', '2026-11-01'],
  ])('resolves %s', (key, start, end) => {
    expect(getCacheHitRateRangeDates(key, new Date(2026, 10, 1, 12))).toEqual({ start, end })
  })
  it('formats without calculating the rate in the browser', () => {
    expect(formatCacheHitRate(null)).toBe('-')
    expect(formatCacheHitRate(0)).toBe('0.0%')
    expect(formatCacheHitRate(90.345)).toBe('90.3%')
  })
})
