import { describe, expect, it, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import UserDashboardCharts from '../UserDashboardCharts.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const props = { loading: false, startDate: '2026-11-09', endDate: '2026-11-09', granularity: 'day', trend: [], models: [{ model: 'fixture', requests: 1, total_tokens: 100, actual_cost: 1.23, cost: 4.56 }] }
describe('dashboard model currencies', () => {
  it('RMB actual excludes dollars', () => {
    const wrapper = shallowMount(UserDashboardCharts, { props })
    const actual = wrapper.get('tbody tr').findAll('td')[3].text()
    expect.soft(actual).toBe('¥1.2300')
    expect.soft(actual).not.toContain('$')
    wrapper.unmount()
  })
  it('preserves standard dollars', () => {
    const wrapper = shallowMount(UserDashboardCharts, { props })
    const standard = wrapper.get('tbody tr').findAll('td')[4].text()
    expect(standard).toBe('$4.5600')
    expect.soft(standard).not.toContain('¥')
    wrapper.unmount()
  })
})
