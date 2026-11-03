import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CreateKeyGuideDialog from '../CreateKeyGuideDialog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const mountDialog = () =>
  mount(CreateKeyGuideDialog, {
    props: { show: true },
    global: {
      stubs: {
        BaseDialog: { props: ['show', 'title'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        RouterLink: { props: ['to'], template: '<a :data-to="to" @click="$attrs.onClick"><slot /></a>' }
      }
    }
  })

describe('CreateKeyGuideDialog', () => {
  it('shows the two create-key screenshots with captions, each linking to the full image', () => {
    const w = mountDialog()
    const images = w.findAll('[data-testid="create-key-guide-image"]')
    expect(images).toHaveLength(2)
    expect(images[0].attributes('src')).toContain('create-api-key-1')
    expect(images[1].attributes('src')).toContain('create-api-key-2')
    expect(images[0].element.closest('a')?.getAttribute('href')).toBe(images[0].attributes('src'))
    expect(w.text()).toContain('dashboard.quickStart.step1GuideShot1')
    expect(w.text()).toContain('dashboard.quickStart.step1GuideShot2')
  })

  it('goes to the keys page and closes', async () => {
    const w = mountDialog()
    const go = w.get('[data-testid="create-key-guide-go"]')
    expect(go.attributes('data-to')).toBe('/keys')
    await go.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })
})
