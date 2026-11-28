export const recentMinutes = [30, 60, 180] as const
export type RecentMinutes = typeof recentMinutes[number]
export type PreciseAdminRange = { start_time: string; end_time: string }
export function makeRecentRange(minutes: RecentMinutes, now = Date.now()): PreciseAdminRange {
  const end = Math.floor(now / 60000) * 60000
  return { start_time: new Date(end - minutes * 60000).toISOString(), end_time: new Date(end).toISOString() }
}
export function restoreRecentRange(start: unknown, end: unknown): { minutes: RecentMinutes; range: PreciseAdminRange } | null {
  if (typeof start !== 'string' || typeof end !== 'string') return null
  const a = new Date(start).getTime(), b = new Date(end).getTime()
  const minutes = (b - a) / 60000
  if (!Number.isFinite(a) || !Number.isFinite(b) || a % 60000 !== 0 || b % 60000 !== 0 || !recentMinutes.includes(minutes as RecentMinutes)) return null
  // Reject civil dates normalized by Date.parse (e.g. February 30).
  const validRFC3339 = (raw: string) => {
    const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.exec(raw)
    if (!m) return false
    const [, y, month, day, hour, minute, second] = m
    const date = new Date(Date.UTC(Number(y), Number(month)-1, Number(day)))
    return date.getUTCFullYear() === Number(y) && date.getUTCMonth()+1 === Number(month) && date.getUTCDate() === Number(day) &&
      Number(hour)<24 && Number(minute)<60 && Number(second)<60
  }
  if (!validRFC3339(start) || !validRFC3339(end)) return null
  return { minutes: minutes as RecentMinutes, range: { start_time: new Date(a).toISOString(), end_time: new Date(b).toISOString() } }
}
export function makeTodayOpsRange(now = new Date()): PreciseAdminRange {
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  return { start_time: start.toISOString(), end_time: now.toISOString() }
}
export function displayAdminWindow(start?: string, end?: string): string {
  if (!start || !end) return ''
  const a = new Date(start), b = new Date(end)
  if (!Number.isFinite(a.getTime()) || !Number.isFinite(b.getTime())) return ''
  return [a, b].map(x => new Intl.DateTimeFormat(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(x)).join(' – ')
}
export function localMinuteLabel(date: string): string {
  const local = new Date(date)
  return String(local.getHours()).padStart(2, '0') + ':' + String(local.getMinutes()).padStart(2, '0')
}
