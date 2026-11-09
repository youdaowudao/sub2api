import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({ Doughnut: { name: 'Doughnut', props: ['options'], template: '<div />' } }))
const endpointStats = [{ endpoint: '/v1/messages', requests: 2, total_tokens: 1000, cost: 4.56, actual_cost: 1.23 }]

describe('endpoint currencies', () => {
  it('RMB actual table and tooltip exclude dollars', () => {
    const wrapper = mount(EndpointDistributionChart, { props: { endpointStats, metric: 'actual_cost' } })
    const actual = wrapper.get('tbody tr').findAll('td')[3].text()
    expect.soft(actual).toBe('¥1.23')
    expect.soft(actual).not.toContain('$')
    const options = wrapper.getComponent({ name: 'Doughnut' }).props('options')
    const label = options.plugins.tooltip.callbacks.label({ label: 'Messages', raw: 1.23, dataset: { data: [1.23] } })
    expect.soft(label).toBe('Messages: ¥1.23 (100.0%)')
    expect.soft(label).not.toContain('$')
    wrapper.unmount()
  })

  it('preserves standard dollars and token tooltips without currency', () => {
    const wrapper = mount(EndpointDistributionChart, { props: { endpointStats } })
    const standard = wrapper.get('tbody tr').findAll('td')[4].text()
    expect(standard).toBe('$4.56')
    expect.soft(standard).not.toContain('¥')
    const options = wrapper.getComponent({ name: 'Doughnut' }).props('options')
    const label = options.plugins.tooltip.callbacks.label({ label: 'Messages', raw: 1000, dataset: { data: [1000] } })
    expect(label).toBe('Messages: 1.00K (100.0%)')
    expect(label).not.toMatch(/[$¥]/)
    wrapper.unmount()
  })
})
