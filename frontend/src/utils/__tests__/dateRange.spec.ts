import { describe, expect, it } from 'vitest'
import { getTodayRangeDates, getGranularityForDateRange } from '../dateRange'

describe('calendar date defaults', () => {
  it('uses the local calendar date and recomputes on each call', () => {
    expect(getTodayRangeDates(new Date(2026, 9, 25, 23, 59))).toEqual({ start: '2026-10-25', end: '2026-10-25' })
    expect(getTodayRangeDates(new Date(2026, 9, 26, 0, 1))).toEqual({ start: '2026-10-26', end: '2026-10-26' })
  })
  it('converts a UTC instant to the browser local calendar day', () => {
    const expected = new Date('2026-10-25T16:00:00Z').getTimezoneOffset() <= -480 ? '2026-10-26' : '2026-10-25'
    expect(getTodayRangeDates(new Date('2026-10-25T16:00:00Z'))).toEqual({ start: expected, end: expected })
  })
  it.each([['2026-10-25', '2026-10-25', 'hour'], ['2026-10-24', '2026-10-25', 'hour'], ['2026-10-19', '2026-10-25', 'day']])('applies the existing granularity rule to %s through %s', (start, end, expected) => {
    expect(getGranularityForDateRange(start, end)).toBe(expected)
  })
})
