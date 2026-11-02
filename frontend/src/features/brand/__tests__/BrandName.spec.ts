import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BrandName from '../components/BrandName.vue'

describe('BrandName', () => {
  it('splits the first "Good" out for the gradient', () => {
    const wrapper = mount(BrandName, { props: { name: 'UseGoodAI' } })
    const highlight = wrapper.get('[data-testid="brand-highlight"]')

    expect(wrapper.text()).toBe('UseGoodAI')
    expect(highlight.text()).toBe('Good')
    expect(highlight.classes()).toContain('brand-good')
  })

  it('only highlights the first occurrence', () => {
    const wrapper = mount(BrandName, { props: { name: 'GoodGood' } })

    expect(wrapper.findAll('[data-testid="brand-highlight"]')).toHaveLength(1)
    expect(wrapper.text()).toBe('GoodGood')
  })

  it.each(['Sub2API', 'usegoodai'])('renders "%s" unchanged without a highlight', (name) => {
    const wrapper = mount(BrandName, { props: { name } })

    expect(wrapper.text()).toBe(name)
    expect(wrapper.find('[data-testid="brand-highlight"]').exists()).toBe(false)
  })
})
