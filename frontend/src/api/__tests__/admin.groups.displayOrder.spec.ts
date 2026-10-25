import { beforeEach, describe, expect, it, vi } from 'vitest'
import { listAllForDisplay, GroupListChangedError } from '../admin/groups'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get } }))
const rows = Array.from({ length: 1002 }, (_, i) => ({ id: i + 1, status: i % 2 ? 'inactive' : 'active', is_exclusive: !!(i % 3) }))
const first = () => ({ data: { items: rows.slice(0, 1000), total: 1002, page: 1, pages: 2 } })
const last = () => ({ data: { items: rows.slice(1000), total: 1002, page: 2, pages: 2 } })
beforeEach(() => get.mockReset())
describe('complete group display snapshot', () => {
  it('reads >1000 public, exclusive and inactive groups without inheriting table filters', async () => {
    get.mockResolvedValueOnce(first()).mockResolvedValueOnce(last())
    const signal = new AbortController().signal
    expect(await listAllForDisplay({ signal })).toEqual(rows)
    expect(get.mock.calls.map(c => c[1])).toEqual([1, 2].map(page => ({ params: { page, page_size: 1000, sort_by: 'id', sort_order: 'asc' }, signal })))
  })
  it.each(['total', 'pages', 'duplicate', 'missing'] as const)('rejects %s changes without returning partial groups', async mode => {
    const second = last()
    if (mode === 'total') second.data.total++
    if (mode === 'pages') second.data.pages++
    if (mode === 'duplicate') second.data.items = [rows[0]!, rows[1001]!]
    if (mode === 'missing') second.data.items = []
    get.mockResolvedValueOnce(first()).mockResolvedValueOnce(second)
    await expect(listAllForDisplay()).rejects.toBeInstanceOf(GroupListChangedError)
  })
  it('rejects a failed page and honours cancellation', async () => {
    get.mockResolvedValueOnce(first()).mockRejectedValueOnce(new Error('page failed'))
    await expect(listAllForDisplay()).rejects.toThrow('page failed')
    const controller = new AbortController(); controller.abort()
    await expect(listAllForDisplay({ signal: controller.signal })).rejects.toThrow()
    expect(get).toHaveBeenCalledTimes(2)
  })
  it('accepts a legal empty list', async () => {
    get.mockResolvedValueOnce({ data: { items: [], total: 0, page: 1, pages: 0 } })
    expect(await listAllForDisplay()).toEqual([])
  })
})
