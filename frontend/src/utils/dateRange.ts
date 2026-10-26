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


export type CacheHitRateRange = 'today' | 'yesterday' | 'yesterdayAndToday'

export function normalizeCacheHitRateRange(value: unknown): CacheHitRateRange {
  return value === 'yesterday' || value === 'yesterdayAndToday' ? value : 'today'
}

export function getCacheHitRateRangeDates(key: string, now = new Date()): { start: string; end: string } {
  const range = normalizeCacheHitRateRange(key)
  const today = formatDateLocalInput(now)
  const previous = new Date(now)
  previous.setDate(previous.getDate() - 1)
  const yesterday = formatDateLocalInput(previous)
  if (range === 'yesterday') return { start: yesterday, end: yesterday }
  return { start: range === 'yesterdayAndToday' ? yesterday : today, end: today }
}
