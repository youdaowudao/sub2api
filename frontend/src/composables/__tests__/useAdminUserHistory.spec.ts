import { defineComponent, h } from 'vue'
import { createPinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { useAuthStore } from '@/stores/auth'
import { useAdminUserHistory } from '../useAdminUserHistory'
import type { User } from '@/types'

describe('admin user query history', () => {
  it('keeps pinned users, deduplicates recent selections and isolates administrator histories', async () => {
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    const setAdmin = (id: number) => { auth.user = { id, role: 'admin' } as User }
    const admin = 934501, other = 934502
    localStorage.removeItem('admin-user-query-history:' + admin)
    localStorage.removeItem('admin-user-query-history:' + other)
    setAdmin(admin)
    let history!: ReturnType<typeof useAdminUserHistory>
    const wrapper = mount(defineComponent({ setup() { history = useAdminUserHistory(); return () => h('div') } }), { global: { plugins: [pinia] } })
    try {
      history.record(1, 'first@example.test')
      history.togglePin(1)
      for(let id=2;id<=24;id++) history.record(id, id+'@example.test')
      expect(history.entries.value.filter(x=>!x.pinned)).toHaveLength(20)
      expect(history.entries.value[0]).toMatchObject({ id:1, pinned:true })
      history.record(1,'changed@example.test')
      expect(history.entries.value.filter(x=>x.id===1)).toHaveLength(1)
      expect(history.entries.value[0].email).toBe('changed@example.test')
      setAdmin(other); await flushPromises()
      expect(history.entries.value).toEqual([])
      history.record(2,'other@example.test')
      setAdmin(admin); await flushPromises()
      expect(history.entries.value[0].id).toBe(1)
      history.remove(1); expect(history.entries.value.some(x=>x.id===1)).toBe(false)
      history.clear(); expect(history.entries.value).toEqual([])
    } finally {
      wrapper.unmount()
      localStorage.removeItem('admin-user-query-history:' + admin)
      localStorage.removeItem('admin-user-query-history:' + other)
    }
  })
})
