import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { UserSpendingRankingItem } from '@/types'
import AdminDashboardUserRanking, { getSpendingComparison } from '../AdminDashboardUserRanking.vue'

const messages: Record<string, string> = {
  'admin.dashboard.userUsageRanking': '用户用量排行',
  'admin.dashboard.totalUsersInRange': '共 {count} 位用户',
  'admin.dashboard.spendingRankingUser': '用户',
  'admin.dashboard.spendingRankingRequests': '请求',
  'admin.dashboard.spendingRankingTokens': 'Token',
  'admin.dashboard.spendingRankingSpend': '消费',
  'admin.dashboard.otherUsers': '其他用户',
  'admin.dashboard.share': '占比',
  'admin.dashboard.vsYesterday': '较昨日同时段',
  'admin.dashboard.newUser': '新增',
  'admin.dashboard.failedToLoad': '加载失败',
  'admin.dashboard.noDataAvailable': '暂无数据',
  'admin.redeem.userPrefix': '用户 #{id}',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      (messages[key] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name])),
  }),
}))

const createItem = (id: number, cost: number, overrides: Partial<UserSpendingRankingItem> = {}): UserSpendingRankingItem => ({
  user_id: id,
  username: `user-${id}`,
  email: `user-${id}@example.com`,
  requests: 2,
  tokens: 3,
  actual_cost: cost,
  ...overrides,
})

const global = { stubs: { LoadingSpinner: true } }

describe('getSpendingComparison', () => {
  const item = createItem(1, 3)

  it('calculates increases, decreases and equal costs for a user included in yesterday’s results', () => {
    const compare = (cost: number) => ({ items: [createItem(1, cost)], totalUsers: 3 })
    expect(getSpendingComparison(item, compare(2))).toBe(50)
    expect(getSpendingComparison(item, compare(6))).toBe(-50)
    expect(getSpendingComparison(item, compare(3))).toBe(0)
    expect(getSpendingComparison(item, compare(0))).toBe('new')
  })

  it('distinguishes an absent user from an incomplete or unavailable comparison', () => {
    const items = [createItem(2, 2)]
    expect(getSpendingComparison(item, { items, totalUsers: 1 })).toBe('new')
    expect(getSpendingComparison(item, { items: [], totalUsers: 0 })).toBe('new')
    expect(getSpendingComparison(item, { items, totalUsers: 2 })).toBeNull()
    expect(getSpendingComparison(item, null)).toBeNull()
  })
})

describe('AdminDashboardUserRanking', () => {
  it('shows name fallbacks, spend share, comparisons and their colors; hides comparison in date mode', async () => {
    const items = [
      createItem(1, 4, { username: ' alpha ' }),
      createItem(2, 3, { username: ' ', email: ' beta@example.com ' }),
      createItem(3, 2, { username: '', email: ' ' }),
      createItem(4, 1),
      createItem(5, 1),
    ]
    const wrapper = mount(AdminDashboardUserRanking, {
      props: {
        items,
        totalActualCost: 16,
        totalRequests: 10,
        totalTokens: 15,
        totalUsers: items.length,
        compare: {
          items: [createItem(1, 2), createItem(2, 6), createItem(3, 2), createItem(4, 0)],
          totalUsers: 5,
        },
        showCompare: true,
        loading: false,
        error: false,
      },
      global,
    })

    expect(wrapper.text()).toContain('用户用量排行')
    expect(wrapper.text()).toContain('共 5 位用户')
    expect(wrapper.findAll('thead th').map(th => th.text())).toEqual(['#', '用户', '请求', 'Token', '消费', '占比', '较昨日同时段'])
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(5)
    expect(rows[0].findAll('td').map(td => td.text())).toEqual(['1', 'alpha', '2', '3', '¥4.00', '25.0%', '↑ 100.0%'])
    expect(rows[1].findAll('td')[1].text()).toBe('beta@example.com')
    expect(rows[2].findAll('td')[1].text()).toBe('用户 #3')
    expect(rows[1].findAll('td')[6].text()).toBe('↓ 50.0%')
    expect(rows[0].findAll('td')[6].classes()).toContain('text-green-600')
    expect(rows[1].findAll('td')[6].classes()).toContain('text-red-600')
    expect(rows[2].findAll('td')[6].text()).toBe('0%')
    expect(rows[3].findAll('td')[6].text()).toBe('新增')
    expect(rows[4].findAll('td')[6].text()).toBe('—')
    expect(wrapper.text()).not.toContain('其他用户')

    await wrapper.setProps({
      compare: { items: [createItem(1, 2), createItem(2, 6), createItem(3, 2), createItem(4, 0)], totalUsers: 4 },
    })
    expect(wrapper.findAll('tbody tr')[4].findAll('td')[6].text()).toBe('新增')

    await wrapper.setProps({ compare: null, totalActualCost: 0 })
    for (const row of wrapper.findAll('tbody tr')) {
      expect(row.findAll('td')[5].text()).toBe('—')
      expect(row.findAll('td')[6].text()).toBe('—')
    }

    await wrapper.setProps({ showCompare: false })
    expect(wrapper.text()).not.toContain('较昨日同时段')
    expect(wrapper.findAll('thead th')).toHaveLength(6)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(6)
  })

  it('aggregates the remainder after the first 12 users and only emits user clicks', async () => {
    const items = Array.from({ length: 12 }, (_, index) => createItem(index + 1, 1))
    const wrapper = mount(AdminDashboardUserRanking, {
      props: {
        items,
        totalActualCost: 18,
        totalRequests: 28,
        totalTokens: 48,
        totalUsers: 15,
        compare: { items: [], totalUsers: 0 },
        showCompare: true,
        loading: false,
        error: false,
      },
      global,
    })
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(13)
    const other = rows[12]
    expect(other.findAll('td').map(td => td.text())).toEqual(['Σ', '其他用户', '4', '12', '¥6.00', '33.3%', '—'])
    expect(other.classes()).not.toContain('cursor-pointer')
    await rows[0].trigger('click')
    await other.trigger('click')
    expect(wrapper.emitted('row-click')).toEqual([[items[0]]])

    await wrapper.setProps({ totalUsers: 12 })
    expect(wrapper.findAll('tbody tr')).toHaveLength(12)
    expect(wrapper.text()).not.toContain('其他用户')

    await wrapper.setProps({ totalUsers: 15, totalActualCost: 12.0000001, totalRequests: 24, totalTokens: 36 })
    expect(wrapper.text()).not.toContain('其他用户')

    await wrapper.setProps({ totalActualCost: 10, totalRequests: 20, totalTokens: 38 })
    expect(wrapper.findAll('tbody tr')[12].findAll('td').map(td => td.text())).toEqual(['Σ', '其他用户', '0', '2', '¥0.0000', '0.0%', '—'])
  })

  it('does not display stale rows while loading or after failure', async () => {
    const wrapper = mount(AdminDashboardUserRanking, {
      props: {
        items: [createItem(1, 3)],
        totalActualCost: 3,
        totalRequests: 2,
        totalTokens: 3,
        totalUsers: 1,
        compare: null,
        showCompare: true,
        loading: true,
        error: true,
      },
      global,
    })
    expect(wrapper.find('loading-spinner-stub').exists()).toBe(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('加载失败')
    expect(wrapper.text()).not.toContain('user-1')

    await wrapper.setProps({ loading: false })
    expect(wrapper.find('loading-spinner-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('user-1')

    await wrapper.setProps({ error: false, items: [], totalActualCost: 0, totalRequests: 0, totalTokens: 0, totalUsers: 0 })
    expect(wrapper.text()).toContain('暂无数据')
    expect(wrapper.text()).toContain('共 0 位用户')
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
  })
})
