import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel, UpdateChannelRequest } from '@/api/admin/channels'
import ChannelsView from '@/views/admin/ChannelsView.vue'

const { list, create, update, getGroups, showError } = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), getGroups: vi.fn(), showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list, create, update },
    groups: { getAll: getGroups },
    settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }
  }
}))
vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn().mockResolvedValue({ found: false }) }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
        }),
        BaseDialog: defineComponent({
          props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>'
        }),
        DataTable: defineComponent({
          props: ['data'],
          template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
        }),
        Pagination: true, ConfirmDialog: true, EmptyState: true,
        Select: true, Icon: true, PlatformIcon: true
      }
    }
  })
}

type View = ReturnType<typeof mountView>

async function clickButton(wrapper: View, text: string) {
  const button = wrapper.findAll('button').find(button => button.text() === text)
  expect(button).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function makeChannel(): Channel {
  return {
    id: 1, name: 'Ultra Fast channel', description: '', status: 'active',
    billing_model_source: 'channel_mapped', restrict_models: false,
    features_config: {}, group_ids: [22], model_pricing: [], model_mapping: {},
    apply_pricing_to_account_stats: false, account_stats_pricing_rules: [],
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z'
  }
}


describe('ChannelsView mapping order', () => {
  const initial = () => ({
    ...makeChannel(),
    model_mapping: { openai: { a: 'target-a', z: 'target-z', m: 'target-m' } },
    model_mapping_order: { openai: ['z', 'a', 'm'] },
  })
  let stored: Channel
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    stored = initial()
    list.mockImplementation(async () => ({ items: [stored], total: 1 }))
    getGroups.mockResolvedValue([{ id: 22, name: 'OpenAI group', platform: 'openai', rate_multiplier: 1 }])
    update.mockImplementation(async (_id: number, request: UpdateChannelRequest) => {
      // Simulate JSONB key order independently of the separately stored order.
      const mapping = request.model_mapping || {}
      stored = {
        ...stored, ...request,
        model_mapping: Object.fromEntries(Object.entries(mapping).map(([platform, entries]) =>
          [platform, Object.fromEntries(Object.entries(entries).sort(([a], [b]) => a.localeCompare(b)))])),
      }
      return stored
    })
  })
  const sources = (wrapper: View) => wrapper.findAll<HTMLInputElement>('input[placeholder="admin.channels.form.mappingSource"]')
  const targets = (wrapper: View) => wrapper.findAll<HTMLInputElement>('input[placeholder="admin.channels.form.mappingTarget"]')
  async function open(wrapper: View) {
    await clickButton(wrapper, 'common.edit')
    await clickButton(wrapper, 'admin.groups.platforms.openai')
  }
  async function save(wrapper: View) {
    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    return update.mock.lastCall![1]
  }

  it('retains paired rows across rename and two saves/reopens, and clears both maps', async () => {
    const wrapper = mountView()
    await flushPromises()
    await open(wrapper)
    expect(sources(wrapper).map(input => input.element.value)).toEqual(['z', 'a', 'm'])
    await sources(wrapper)[1].setValue('new-a')
    expect(sources(wrapper).map(input => input.element.value)).toEqual(['z', 'new-a', 'm'])
    const wanted = { openai: ['z', 'new-a', 'm'] }
    for (let i = 0; i < 2; i++) {
      expect(targets(wrapper).map(input => input.element.value)).toEqual(['target-z', 'target-a', 'target-m'])
      expect((await save(wrapper)).model_mapping_order).toEqual(wanted)
      await open(wrapper)
      expect(sources(wrapper).map(input => input.element.value)).toEqual(wanted.openai)
    }
    for (const input of sources(wrapper)) {
      await input.element.parentElement!.querySelector('button')!.click()
      await flushPromises()
    }
    expect(sources(wrapper)).toHaveLength(0)
    const cleared = await save(wrapper)
    expect(cleared.model_mapping).toEqual({})
    expect(cleared.model_mapping_order).toEqual({})
    wrapper.unmount()
  })

  it('sorts older data and ignores stale and duplicate order keys', async () => {
    stored.model_mapping_order = { openai: ['z', 'missing', 'z'] }
    const wrapper = mountView()
    await flushPromises()
    await open(wrapper)
    expect(sources(wrapper).map(input => input.element.value)).toEqual(['z', 'a', 'm'])
    expect(targets(wrapper).map(input => input.element.value)).toEqual(['target-z', 'target-a', 'target-m'])
    wrapper.unmount()
    stored.model_mapping_order = null
    const old = mountView()
    await flushPromises()
    await open(old)
    expect(sources(old).map(input => input.element.value)).toEqual(['a', 'm', 'z'])
    old.unmount()
  })
})
