import { formatDateLocalInput } from './format'

export function getTodayRangeDates(now = new Date()): { start: string; end: string } {
  const today = formatDateLocalInput(now)
  return { start: today, end: today }
}

/** Match the existing administrator dashboard rule when applying a date range. */
export function getGranularityForDateRange(start: string, end: string): 'hour' | 'day' {
  const days = Math.ceil((new Date(end).getTime() - new Date(start).getTime()) / 86400000)
  return days <= 1 ? 'hour' : 'day'
}
