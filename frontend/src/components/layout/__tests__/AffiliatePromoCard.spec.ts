import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount, RouterLinkStub } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import AffiliatePromoCard from '../AffiliatePromoCard.vue'

enableAutoUnmount(afterEach)
function create(props = {}) {
  return mount(AffiliatePromoCard, { props: { collapsed: false, compact: false, rebateRate: 10, ...props }, global: {
    plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })], stubs: { RouterLink: RouterLinkStub },
  } })
}

describe('referral promotion card', () => {
  it('toggles independently without navigation, and keeps links pointed to the invitation page', async () => {
    const wrapper = create()
    expect(wrapper.text()).toContain('10%')
    expect(wrapper.text()).not.toMatch(/首充|符合规则/)
    expect(wrapper.get('.promo-body').text()).toContain('好友通过邀请链接加入，')
    expect(wrapper.get('.promo-body').text()).toContain('TA 每笔充值，你都享')
    expect(wrapper.get('.promo-body').text()).toContain('邀请返利')
    expect(wrapper.get('.promo-lasting').text()).toContain('永久有效！')
    expect(wrapper.get('.promo-lasting').find('svg').exists()).toBe(true)
    expect(wrapper.get('.promo-rate').find('svg').exists()).toBe(false)
    expect(wrapper.text().match(/10%/g)).toHaveLength(1)
    expect(wrapper.findAllComponents(RouterLinkStub).every(link => link.props('to') === '/affiliate')).toBe(true)
    const toggle = wrapper.get('button')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(toggle.element.closest('a')).toBeNull()
    await toggle.trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
    expect(wrapper.emitted('navigate')).toBeUndefined()
    await wrapper.setProps({ collapsed: true })
    expect(wrapper.text()).not.toContain('10%')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('a').trigger('click')
    expect(wrapper.emitted('navigate')).toHaveLength(1)
  })

  it.each([null, 0, -1, 101, NaN])('does not invent a rebate when the rate is %s', rate => {
    const wrapper = create({ rebateRate: rate })
    expect(wrapper.text()).toContain('查看返利规则')
    expect(wrapper.text()).not.toContain('10%')
    expect(wrapper.text()).not.toContain('永久有效')
  })

  it('preserves fractional rates and compact sidebar mode has only an invitation link', async () => {
    const wrapper = create({ rebateRate: 12.5 })
    expect(wrapper.text()).toContain('12.5%')
    await wrapper.setProps({ compact: true })
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.findAll('a')).toHaveLength(1)
    expect(wrapper.get('a').attributes('aria-label')).toBeTruthy()
  })
})
