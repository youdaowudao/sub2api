import { describe, expect, it } from 'vitest'
import { checkinCalendar, shiftCheckinMonth } from '../checkinCalendar'

describe('checkinCalendar', () => {
  it('handles leap years, 42 cells and Monday-first offsets', () => {
    const cells = checkinCalendar('2024-02', '2024-02-29', '2024-01')
    expect(cells).toHaveLength(42)
    expect(cells.filter(c => c.inMonth)).toHaveLength(29)
    expect(cells.find(c => c.isToday)?.date).toBe('2024-02-29')
    expect(cells[0].date).toBe('2024-01-29')
    expect(checkinCalendar('2026-11', '2026-11-26', '2026-10')[6].date).toBe('2026-11-01')
  })
  it('uses server today and bounds navigation, including year rollover', () => {
    const cells = checkinCalendar('2026-01', '2026-01-01', '2025-12')
    expect(cells.find(c => c.isToday)?.day).toBe(1)
    expect(cells.find(c => c.date === '2026-01-02')?.isFuture).toBe(true)
    expect(shiftCheckinMonth('2025-12', 1)).toBe('2026-01')
    expect(shiftCheckinMonth('2026-01', -1)).toBe('2025-12')
    expect(checkinCalendar('2025-11', '2026-01-01', '2025-12')).toEqual([])
    expect(checkinCalendar('2026-02', '2026-01-01', '2025-12')).toEqual([])
  })
})
