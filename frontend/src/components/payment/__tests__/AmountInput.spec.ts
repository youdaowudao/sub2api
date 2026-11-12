import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import AmountInput from '../AmountInput.vue'
import { formatPaymentAmount } from '../currency'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params && 'amount' in params ? `${key} ${String(params.amount)}` : key,
  }),
}))
enableAutoUnmount(afterEach)

function mountInput(value: number | null = null) {
  return mount(AmountInput, { props: { modelValue: value } })
}

describe('recharge amount input', () => {
  it('shows the nine updated shortcuts and emits small amounts when clicked', async () => {
    const wrapper = mountInput()
    expect(wrapper.findAll('button').map(button => Number(button.text()))).toEqual([1, 5, 10, 20, 50, 100, 200, 500, 1000])
    for (const amount of [1, 5]) await wrapper.get(`[data-testid="quick-amount-${amount}"]`).trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[1], [5]])
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('5')
    expect(wrapper.find('[data-testid="quick-amount-2000"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-amount-5000"]').exists()).toBe(false)
  })

  it.each(['10abc', '10.555', '-10', '1e2'])('restores the accepted amount after rejecting %s', async (value) => {
    const wrapper = mountInput(10)
    const input = wrapper.get('input')
    await input.setValue(value)
    expect((input.element as HTMLInputElement).value).toBe('10')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('restores the last typed amount rather than a stale prop', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    await input.setValue('12.50')
    await input.setValue('12.500')
    expect((input.element as HTMLInputElement).value).toBe('12.50')
    expect(wrapper.emitted('update:modelValue')).toEqual([[12.5]])
  })

  it('preserves decimal editing and allows clearing the amount', async () => {
    const wrapper = mountInput()
    const input = wrapper.get('input')
    for (const value of ['0', '0.', '0.5', '0.50', '']) await input.setValue(value)
    expect(wrapper.emitted('update:modelValue')).toEqual([[null], [null], [0.5], [0.5], [null]])
    expect((input.element as HTMLInputElement).value).toBe('')
  })
})

describe('recharge bonus hints on quick amounts', () => {
  it('shows the agreed four tiers using actual quotes and does not change the submitted amount', async () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, bonusTiers: [
      { min_amount: 100, bonus_percent: 3 }, { min_amount: 200, bonus_percent: 3.5 },
      { min_amount: 500, bonus_percent: 4 }, { min_amount: 1000, bonus_percent: 5 },
    ] } })
    for (const [amount, bonus, total] of [[100, 3, '103.00'], [200, 3.5, '207.00'], [500, 4, '520.00'], [1000, 5, '1050.00']]) {
      const button = wrapper.get(`[data-testid=quick-amount-${amount}]`)
      expect(button.get('.recharge-glass-badge').text()).toBe(`+${bonus}%`)
      expect(button.text()).toContain(`¥${total}`)
      await button.trigger('click')
    }
    expect(wrapper.emitted('update:modelValue')).toEqual([[100], [200], [500], [1000]])
    for (const amount of [1, 5, 10, 20, 50]) expect(wrapper.get(`[data-testid=quick-amount-${amount}]`).find('.recharge-glass-badge').exists()).toBe(false)
    await wrapper.setProps({ bonusMode: 'discount' })
    expect(wrapper.find('.recharge-glass-badge').exists()).toBe(false)
    expect(wrapper.text()).toContain('3.5% OFF')
  })
  const tiers = [
    { min_amount: 100, bonus_percent: 20 },
    { min_amount: 500, bonus_percent: 30 },
  ]

  it('renders no badge or second line when no tiers are configured', () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [50, 100, 500] } })
    expect(wrapper.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-amount-credited"]').exists()).toBe(false)
  })

  it('bonus mode: price tag only on amounts that hit a tier, credited totals on every button', () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [50, 100, 500], bonusTiers: tiers } })
    const below = wrapper.get('[data-testid="quick-amount-50"]')
    const first = wrapper.get('[data-testid="quick-amount-100"]')
    const second = wrapper.get('[data-testid="quick-amount-500"]')

    expect(below.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect.soft(below.get('[data-testid="quick-amount-credited"]').text()).toContain('¥50.00')
    expect(first.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+20%')
    expect.soft(first.get('[data-testid="quick-amount-credited"]').text()).toContain('¥120.00')
    expect(second.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+30%')
    expect.soft(second.get('[data-testid="quick-amount-credited"]').text()).toContain('¥650.00')
    for (const button of [below, first, second]) expect(button.get('[data-testid="quick-amount-credited"]').text()).not.toContain('$')
  })

  it('bonus mode: matches tiers by the entered amount but credits by the multiplier', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [1000], bonusTiers: tiers, multiplier: 0.14 },
    })
    const button = wrapper.get('[data-testid="quick-amount-1000"]')
    expect(button.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('+30%')
    // 1000 × 0.14 = 140 base, +30% = 182
    expect.soft(button.get('[data-testid="quick-amount-credited"]').text()).toContain('¥182.00')
    expect(button.get('[data-testid="quick-amount-credited"]').text()).not.toContain('$')
  })

  it('discount mode: tag reads N% OFF and the second line shows the discounted payment', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [50, 500], bonusTiers: tiers, bonusMode: 'discount', currency: 'USD' },
    })
    const below = wrapper.get('[data-testid="quick-amount-50"]')
    const hit = wrapper.get('[data-testid="quick-amount-500"]')
    expect(below.find('[data-testid="quick-amount-bonus-badge"]').exists()).toBe(false)
    expect(below.get('[data-testid="quick-amount-credited"]').text()).toContain('50.00')
    expect(hit.get('[data-testid="quick-amount-bonus-badge"]').text()).toBe('30% OFF')
    // 500 × (1 − 30%) = 350
    expect(hit.get('[data-testid="quick-amount-credited"]').text()).toContain('350.00')
    for (const [button, amount] of [[below, 50], [hit, 350]] as const) {
      expect(button.get('[data-testid="quick-amount-credited"]').text()).toContain(formatPaymentAmount(amount, 'USD'))
      expect.soft(button.get('[data-testid="quick-amount-credited"]').text()).not.toContain('¥')
    }
  })
})
