import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import PricingEntryCard from '../PricingEntryCard.vue'
import IntervalRow from '../IntervalRow.vue'
import { createDefaultTimePricingForm, formIntervalsToAPI, type PricingFormEntry } from '../types'
import zh from '@/i18n/locales/zh/admin/channels'
import en from '@/i18n/locales/en/admin/channels'

enableAutoUnmount(afterEach)

function createEntry(): PricingFormEntry {
  return {
    models: [],
    billing_mode: 'video',
    input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
    image_input_price: null, image_output_price: null, per_request_price: 0.6,
    time_pricing: createDefaultTimePricingForm(),
    intervals: [{
      min_tokens: 3, max_tokens: 15, tier_label: '720p', sort_order: 0,
      input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
      input_multiplier: null, output_multiplier: null, cache_write_multiplier: null,
      cache_read_multiplier: null, per_request_price: 0.48,
    }],
  }
}

function mountCard(entry = createEntry(), customerVideoPricing = true, locale = 'zh') {
  return mount(PricingEntryCard, {
    props: { entry, customerVideoPricing },
    global: {
      plugins: [createI18n({ legacy: false, locale, messages: { zh: { admin: zh }, en: { admin: en } } })],
      stubs: { Select: true, ModelTagInput: true, TimePricingSection: true },
    },
  })
}

describe('customer video pricing', () => {
  it.each([
    ['zh', '分辨率', '每秒价格', '默认每秒价格', '视频按秒计价怎么填', '不能用 0 表示免费。', '分组'],
    ['en', 'Resolution', 'Price per second', 'Default price per second', 'How to fill in per-second video pricing', '0 does not mean free.', 'group'],
  ])('renders the %s customer guide and resolution prices without token bounds or dollars', (locale, resolution, price, defaultPrice, title, zero, group) => {
    const wrapper = mountCard(createEntry(), true, locale)
    const row = wrapper.getComponent(IntervalRow)
    expect(row.findAll('label').map(label => label.text())).toEqual([resolution, price])
    expect(row.get('input[type="text"]').attributes('placeholder')).toBe('480p / 720p / 1080p')
    expect(wrapper.findAll('label').map(label => label.text())).toContain(defaultPrice)
    expect(wrapper.text()).not.toContain('$')
    expect(wrapper.text()).toContain(title)
    const strings = (locale === 'zh' ? zh : en).channels.form
    expect(wrapper.findAll('li')).toHaveLength(5)
    expect(wrapper.text()).toContain(strings.videoPricingGuideResolution)
    expect(wrapper.text()).toContain(strings.videoPricingGuideCharge)
    expect(wrapper.text()).toContain(strings.videoPricingGuideSettings)
    expect(wrapper.text()).toContain(strings.videoPricingGuideFallback)
    expect(wrapper.text()).toContain(strings.videoPricingGuideValidation)
    expect(wrapper.text()).toContain(strings.videoPricingGuideGroup.replace('{group}', group))
    expect(wrapper.findAll('strong').map(item => item.text())).toEqual([zero, group])
    expect(wrapper.text()).toContain(strings.videoPricingGuideExample)
  })

  it('keeps old bounds in the submitted interval and initializes only new rows to 0 / null', async () => {
    const wrapper = mountCard()
    const row = wrapper.getComponent(IntervalRow)
    await row.get('input[type="number"]').setValue('0.5')
    const updated = wrapper.emitted('update')!.at(-1)![0] as PricingFormEntry
    expect(formIntervalsToAPI(updated.intervals)[0]).toMatchObject({ min_tokens: 3, max_tokens: 15, tier_label: '720p', per_request_price: 0.5 })
    await wrapper.setProps({ entry: updated })
    const add = wrapper.findAll('button').find(button => button.text().includes(zh.channels.form.addTier))!
    await add.trigger('click')
    const added = wrapper.emitted('update')!.at(-1)![0] as PricingFormEntry
    expect(added.intervals[1]).toMatchObject({ min_tokens: 0, max_tokens: null, per_request_price: null })
    expect(added.intervals[0]).toEqual(updated.intervals[0])
  })

  it.each([
    ['video', false, '层级', '默认视频每秒价格'],
    ['image', true, '分辨率', '默认图片价格'],
    ['per_request', true, '层级', '默认单次价格'],
  ] as const)('keeps old %s rendering (customer flag %s)', (mode, flag, tier, defaultLabel) => {
    const wrapper = mountCard({ ...createEntry(), billing_mode: mode }, flag)
    const row = wrapper.getComponent(IntervalRow)
    expect(row.findAll('label').map(label => label.text().replace(/\s+/g, ' '))).toEqual([tier, '最小', '最大 （含）', '单次价格 $'])
    expect(wrapper.text()).toContain(defaultLabel)
    expect(wrapper.text()).toContain('$')
    expect(wrapper.text()).not.toContain('视频按秒计价怎么填')
    expect(row.findAll('input')).toHaveLength(4)
  })

  it('opts in both group editors and channel customer prices, not account statistics prices', () => {
    const groups = readFileSync(resolve('src/views/admin/GroupsView.vue'), 'utf8')
    const channels = readFileSync(resolve('src/views/admin/ChannelsView.vue'), 'utf8')
    const groupCards = groups.match(/<PricingEntryCard\b[^>]*\/>/g) ?? []
    const channelCards = channels.match(/<PricingEntryCard\b[^>]*\/>/g) ?? []
    expect(groupCards).toHaveLength(2)
    for (const form of ['createForm', 'editForm']) {
      expect(groupCards.find(card => card.includes(form + '.model_pricing'))).toContain('customer-video-pricing')
    }
    expect(channelCards).toHaveLength(2)
    expect(channelCards.find(card => card.includes('section.model_pricing'))).toContain('customer-video-pricing')
    expect(channelCards.find(card => card.includes('rule.pricing'))).not.toContain('customer-video-pricing')
  })
})
