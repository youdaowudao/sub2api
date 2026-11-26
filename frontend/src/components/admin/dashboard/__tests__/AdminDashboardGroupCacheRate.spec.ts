import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { GroupStat } from '@/types'
import AdminDashboardGroupCacheRate from '../AdminDashboardGroupCacheRate.vue'

const messages: Record<string, string> = {
  'admin.dashboard.groupCacheHitRate': '分组缓存命中率',
  'admin.dashboard.groupCacheHitRateHint': '按所选时间范围内请求量最多的 5 个分组',
  'admin.dashboard.requests': '请求',
  'admin.dashboard.noGroup': '无分组',
  'admin.dashboard.noDataAvailable': '暂无数据',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
}))

const group = (id: number, name: string, requests: number, rate?: number | null): GroupStat => ({
  group_id: id,
  group_name: name,
  requests,
  total_tokens: 1000,
  actual_cost: 1,
  cost: 2,
  cache_hit_rate: rate,
})

const global = { stubs: { LoadingSpinner: true } }

describe('AdminDashboardGroupCacheRate', () => {
  it('selects the five groups with the most requests and displays null, zero and missing cache rates distinctly', () => {
    const groups = [
      group(1, 'few-requests', 2, 100),
      group(2, 'many-requests', 100, 43.25),
      group(3, 'middle', 40, 0),
      group(4, '', 80, null),
      group(5, 'fourth', 60, 75),
      group(6, 'fifth', 20),
      group(7, 'other-low', 10, 98),
    ]
    const wrapper = mount(AdminDashboardGroupCacheRate, {
      props: { groups, loading: false },
      global,
    })

    expect(wrapper.text()).toContain('分组缓存命中率')
    expect(wrapper.text()).toContain('按所选时间范围内请求量最多的 5 个分组')
    const rows = wrapper.findAll('li')
    expect(rows).toHaveLength(5)
    expect(rows.map(row => row.get('span[title]').text())).toEqual(['many-requests', '无分组', 'fourth', 'middle', 'fifth'])
    expect(rows.map(row => row.findAll('span')[1].text())).toEqual(['请求 100', '请求 80', '请求 60', '请求 40', '请求 20'])
    expect(rows.map(row => row.findAll('span')[2].text())).toEqual(['43.3%', '—', '75.0%', '0.0%', '—'])
    expect(rows[0].get('[style]').attributes('style')).toContain('width: 43.25%')
    expect(rows[1].find('[style]').exists()).toBe(false)
    expect(rows[2].get('[style]').attributes('style')).toContain('width: 75%')
    expect(rows[3].get('[style]').attributes('style')).toContain('width: 0%')
    expect(rows[4].find('[style]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('few-requests')
    expect(wrapper.text()).not.toContain('other-low')
    expect(groups.map(item => item.group_id)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('shows loading before data and an empty state when no groups are returned', async () => {
    const wrapper = mount(AdminDashboardGroupCacheRate, {
      props: { groups: [group(1, 'old-group', 10, 50)], loading: true },
      global,
    })
    expect(wrapper.find('loading-spinner-stub').exists()).toBe(true)
    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('old-group')
    await wrapper.setProps({ loading: false, groups: [] })
    expect(wrapper.find('loading-spinner-stub').exists()).toBe(false)
    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.text()).toContain('暂无数据')
  })
})
