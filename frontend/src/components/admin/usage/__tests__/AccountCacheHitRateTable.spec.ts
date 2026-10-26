import { expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountCacheHitRateTable from '../AccountCacheHitRateTable.vue'
import CacheHitRateCell from '../CacheHitRateCell.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (k: string, args?: Record<string, string>) => `${k}${args?.reason ?? ''}` }) }))
it('shows loading, empty and failed states with retry text', () => {
  expect(mount(AccountCacheHitRateTable, { props: { items: [], loading: true, error: null } }).findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
  expect(mount(AccountCacheHitRateTable, { props: { items: [], loading: false, error: null } }).text()).toContain('usage.cacheRateNoAccounts')
  expect(mount(AccountCacheHitRateTable, { props: { items: [], loading: false, error: 'fixture offline' } }).text()).toContain('fixture offline')
})
it('shows server request order, missing names and account ID titles', () => {
  const items = [{ id: 2, name: '', platform: '', requests: 4, input_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, cache_hit_rate: null }, { id: 1, name: 'Fixture', platform: 'anthropic', requests: 2, input_tokens: 10, cache_creation_tokens: 0, cache_read_tokens: 90, cache_hit_rate: 90 }]
  const wrapper = mount(AccountCacheHitRateTable, { props: { items, loading: false, error: null } })
  const rows = wrapper.findAll('tbody tr'); expect(rows[0].text()).toContain('#2'); expect(rows[1].text()).toContain('90.0%'); expect(rows[1].find('[title="#1"]').exists()).toBe(true)
})
it('uses blue percentages and a separate request count only for available records', () => {
  const wrapper = mount(CacheHitRateCell, { props: { loading: false, error: null, item: { id: 1, name: '', platform: '', requests: 3, input_tokens: 1, cache_creation_tokens: 0, cache_read_tokens: 9, cache_hit_rate: 90 } } })
  expect(wrapper.find('.text-blue-600').text()).toBe('90.0%'); expect(wrapper.text()).toContain('3')
})
