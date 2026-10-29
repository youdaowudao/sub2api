import { expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { CacheHitRateItem } from '@/types'
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

function makeAccounts(count: number): CacheHitRateItem[] {
  return Array.from({ length: count }, (_, index) => ({
    id: index + 1,
    name: `Account ${index + 1}`,
    platform: 'anthropic',
    requests: 100 - index,
    input_tokens: 10,
    cache_creation_tokens: 0,
    cache_read_tokens: 90,
    cache_hit_rate: 90
  }))
}

it.each<[number, number[]]>([
  [1, [1]],
  [5, [5]],
  [6, [5, 1]],
  [10, [5, 5]],
  [11, [6, 5]],
  [12, [6, 6]],
  [25, [13, 12]]
])('lays out %i accounts down the left column before the right', (count, expectedRows) => {
  const items = makeAccounts(count)
  const original = structuredClone(items)
  const wrapper = mount(AccountCacheHitRateTable, { props: { items, loading: false, error: null } })
  const tables = wrapper.findAll('table')

  expect(tables.map(table => table.findAll('tbody tr').length)).toEqual(expectedRows)
  expect(tables.flatMap(table => table.findAll('tbody tr').map(row => row.find('td').attributes('title'))))
    .toEqual(items.map(item => `#${item.id}`))
  expect(items).toEqual(original)
  // Empty slots have no extra table header or fabricated account rows.
  expect(wrapper.findAll('thead')).toHaveLength(expectedRows.length)
})

it('updates both columns after filtering without retaining old accounts', async () => {
  const wrapper = mount(AccountCacheHitRateTable, { props: { items: makeAccounts(6), loading: false, error: null } })
  expect(wrapper.findAll('table')).toHaveLength(2)

  await wrapper.setProps({ items: makeAccounts(4) })
  expect(wrapper.findAll('table')).toHaveLength(1)
  expect(wrapper.findAll('tbody tr')).toHaveLength(4)
  expect(wrapper.find('[title="#5"]').exists()).toBe(false)
  expect(wrapper.find('[title="#6"]').exists()).toBe(false)

  await wrapper.setProps({ items: makeAccounts(12) })
  expect(wrapper.findAll('table').map(table => table.findAll('tbody tr').length)).toEqual([6, 6])
  expect(wrapper.findAll('tbody tr')).toHaveLength(12)
})

it('replaces populated columns with loading, failure and empty states', async () => {
  const wrapper = mount(AccountCacheHitRateTable, { props: { items: makeAccounts(6), loading: false, error: null } })
  await wrapper.setProps({ loading: true })
  expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
  expect(wrapper.find('table').exists()).toBe(false)

  await wrapper.setProps({ loading: false, error: 'fixture offline' })
  expect(wrapper.text()).toContain('fixture offline')
  expect(wrapper.find('table').exists()).toBe(false)
  expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(false)

  await wrapper.setProps({ error: null, items: [] })
  expect(wrapper.text()).toContain('usage.cacheRateNoAccounts')
  expect(wrapper.text()).not.toContain('fixture offline')
  expect(wrapper.find('table').exists()).toBe(false)
})

it('uses blue percentages and a separate request count only for available records', () => {
  const wrapper = mount(CacheHitRateCell, { props: { loading: false, error: null, item: { id: 1, name: '', platform: '', requests: 3, input_tokens: 1, cache_creation_tokens: 0, cache_read_tokens: 9, cache_hit_rate: 90 } } })
  expect(wrapper.find('.text-blue-600').text()).toBe('90.0%'); expect(wrapper.text()).toContain('3')
})
