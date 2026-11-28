import { describe, expect, it } from 'vitest'
import { makeTodayRankingWindows, makeRecentRange } from '../adminRecentRange'

const minutes = (start: string, end: string) => (Date.parse(end) - Date.parse(start)) / 60000
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone

describe('dashboard ranking windows', () => {
  it('uses independent civil days and one complete-minute cutoff', () => {
    const windows = makeTodayRankingWindows(new Date(2026, 10, 27, 15, 30, 25))
    expect(windows.current).toEqual({ start_time: new Date(2026, 10, 27).toISOString(), end_time: new Date(2026, 10, 27, 15, 30).toISOString() })
    expect(windows.previous).toEqual({ start_time: new Date(2026, 10, 26).toISOString(), end_time: new Date(2026, 10, 26, 15, 30).toISOString() })
    const midnight = makeTodayRankingWindows(new Date(2026, 10, 27, 0, 0, 25))
    expect(midnight.current.start_time).toBe(midnight.current.end_time)
    expect(midnight.previous).toBeNull()
  })

  it('retains exact elapsed recent windows', () => {
    const now = Date.parse('2026-11-27T00:37:25Z')
    for (const duration of [30, 60, 180] as const) {
      const window = makeRecentRange(duration, now)
      expect(window.end_time).toBe('2026-11-27T00:37:00.000Z')
      expect(minutes(window.start_time, window.end_time)).toBe(duration)
    }
  })

  it.skipIf(timezone !== 'America/New_York')('uses actual clock time on spring-forward and fall-back days', () => {
    const spring = makeTodayRankingWindows(new Date(2026, 2, 8, 4))
    expect(minutes(spring.current.start_time, spring.current.end_time)).toBe(180)
    expect(minutes(spring.previous!.start_time, spring.previous!.end_time)).toBe(240)
    const skipped = makeTodayRankingWindows(new Date(2026, 2, 9, 2, 30))
    expect(skipped.previous!.end_time).toBe('2026-03-08T07:30:00.000Z')
    const fall = makeTodayRankingWindows(new Date(2026, 10, 1, 4))
    expect(minutes(fall.current.start_time, fall.current.end_time)).toBe(300)
    expect(minutes(fall.previous!.start_time, fall.previous!.end_time)).toBe(240)
    const repeated = makeTodayRankingWindows(new Date(2026, 10, 2, 1, 30))
    expect(repeated.previous!.end_time).toBe('2026-11-01T05:30:00.000Z')
  })

  it.skipIf(timezone !== 'America/Santiago')('does not derive yesterday from today’s skipped midnight', () => {
    const windows = makeTodayRankingWindows(new Date(2026, 8, 6, 5))
    expect(windows.current.start_time).toBe('2026-09-06T04:00:00.000Z')
    expect(windows.previous!.start_time).toBe('2026-09-05T04:00:00.000Z')
    expect(minutes(windows.current.start_time, windows.current.end_time)).toBe(240)
    expect(minutes(windows.previous!.start_time, windows.previous!.end_time)).toBe(300)
  })

  it.skipIf(timezone !== 'America/Havana')('uses the first real midnight on a repeated-midnight day', () => {
    const windows = makeTodayRankingWindows(new Date(2026, 10, 2, 0, 30))
    expect(windows.previous!.start_time).toBe('2026-11-01T04:00:00.000Z')
    expect(windows.previous!.end_time).toBe('2026-11-01T04:30:00.000Z')
  })
})
