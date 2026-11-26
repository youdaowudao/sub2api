// The server's civil date is authoritative, independent of browser timezone.
export interface CheckinDay {
  date: string
  day: number
  inMonth: boolean
  isToday: boolean
  isFuture: boolean
}
const pad = (n: number) => String(n).padStart(2, '0')
export function shiftCheckinMonth(month: string, offset: number): string {
  const [year, m] = month.split('-').map(Number)
  const date = new Date(Date.UTC(year, m - 1 + offset, 1))
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}`
}
export function checkinCalendar(month: string, today: string, registeredMonth: string): CheckinDay[] {
  if (month < registeredMonth || month > today.slice(0, 7)) return []
  const [year, m] = month.split('-').map(Number)
  const first = (new Date(Date.UTC(year, m - 1, 1)).getUTCDay() + 6) % 7
  return Array.from({ length: 42 }, (_, i) => {
    const d = new Date(Date.UTC(year, m - 1, i - first + 1))
    const date = `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
    return { date, day: d.getUTCDate(), inMonth: date.startsWith(month), isToday: date === today, isFuture: date > today }
  })
}
