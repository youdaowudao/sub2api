import { beforeEach, describe, expect, it, vi } from 'vitest'
import { startScheduleTimer } from '../admin/accounts'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post } }))

describe('account schedule timer API', () => {
  beforeEach(() => post.mockReset())

  it.each([1, 2, 3, 6, 12, 24])('sends %s hours to the backend and preserves its saved state', async hours => {
    const saved = { id: 17, schedulable: false, schedule_resume_at: '2026-11-20T06:00:00Z' }
    post.mockResolvedValue({ data: saved })
    expect(await startScheduleTimer(17, hours)).toEqual(saved)
    expect(post).toHaveBeenCalledWith('/admin/accounts/17/scheduling-timer', { duration_hours: hours })
    expect(post).toHaveBeenCalledTimes(1)
  })
})
