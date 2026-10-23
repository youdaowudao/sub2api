import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { usePluginEntriesStore } from '../pluginEntries'
import type { PluginEntry } from '@/api/plugins'

const mocks = vi.hoisted(() => ({ list: vi.fn() }))
const auth = reactive({ token: 'private', isAuthenticated: true, user: { id: 42, role: 'user', status: 'active' } })
vi.mock('../auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/api/plugins', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/plugins')>(),
  default: { listEntries: mocks.list },
}))
const entry: PluginEntry = { plugin_id: 7, plugin_key: 'example.ui', revision: 'one', entry_id: 'home', label: 'Plugin page', mount: 'user', roles: ['user', 'admin'], methods: [] }
let release: (() => void) | undefined

describe('plugin entry lifecycle', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    auth.token = 'private'; auth.isAuthenticated = true; auth.user.role = 'user'; auth.user.status = 'active'
    mocks.list.mockResolvedValue([entry])
  })
  afterEach(() => { release?.(); release = undefined; vi.useRealTimers() })

  it('shares one refresh timer, refreshes after five seconds and stops when the last layout leaves', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const store = usePluginEntriesStore()
    release = store.acquire()
    const second = store.acquire()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(1)
    second()
    await vi.advanceTimersByTimeAsync(5_000)
    expect(mocks.list).toHaveBeenCalledTimes(2)
    release(); release = undefined
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps role and mount filtering consistent and immediately clears on logout', async () => {
    mocks.list.mockResolvedValue([entry, { ...entry, entry_id: 'admin', mount: 'admin', roles: ['admin'] }, { ...entry, entry_id: 'shell', mount: 'shell', roles: ['admin'] }])
    const store = usePluginEntriesStore()
    release = store.acquire()
    await flushPromises()
    expect(store.entries.map(entry => entry.entry_id)).toEqual(['home'])
    expect(store.forMount('admin')).toEqual([])
    auth.user.role = 'admin'
    expect(store.entries).toEqual([])
    await flushPromises()
    expect(store.forMount('admin').map(entry => entry.entry_id)).toEqual(['admin'])
    expect(store.forMount('shell').map(entry => entry.entry_id)).toEqual(['shell'])
    expect(store.selectedShellKey).toBe('7:shell')
    auth.isAuthenticated = false
    auth.token = ''
    expect(store.entries).toEqual([])
    expect(store.selectedShellKey).toBeNull()
  })

  it('ignores a pre-logout response and aborts the pending refresh when unused', async () => {
    let resolve!: (entries: PluginEntry[]) => void
    mocks.list.mockImplementation(() => new Promise(done => { resolve = done }))
    const store = usePluginEntriesStore()
    release = store.acquire()
    const signal = mocks.list.mock.calls[0][0] as AbortSignal
    auth.isAuthenticated = false; auth.token = ''
    expect(signal.aborted).toBe(true)
    resolve([entry])
    await flushPromises()
    expect(store.entries).toEqual([])
    auth.isAuthenticated = true; auth.token = 'new'
    await flushPromises()
    const latestSignal = mocks.list.mock.calls.at(-1)![0] as AbortSignal
    release(); release = undefined
    expect(latestSignal.aborted).toBe(true)
  })

  it('removes entries after an error and preserves a manually closed shell across refreshes', async () => {
    mocks.list.mockResolvedValue([{ ...entry, mount: 'shell' }])
    const store = usePluginEntriesStore()
    release = store.acquire()
    await flushPromises()
    expect(store.selectedShellKey).toBe('7:home')
    store.closeShell()
    await store.refresh()
    expect(store.selectedShellKey).toBeNull()
    mocks.list.mockRejectedValue(new Error('offline'))
    await store.refresh()
    expect(store.entries).toEqual([])
    expect(store.ready).toBe(false)
    expect(store.error).toBeInstanceOf(Error)
  })
})
