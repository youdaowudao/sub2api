import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QuickStartArticle from '../components/QuickStartArticle.vue'

describe('built-in quick start article', () => {
  afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })
  it('renders seven unique markers, one read-only command and packaged images with secure links', () => {
    const wrapper = mount(QuickStartArticle)
    for (const id of ['windows-step', 'windows-run', 'windows-command', 'codex-test', 'models', 'problems', 'support']) expect(wrapper.findAll(`[data-onboarding="${id}"]`)).toHaveLength(1)
    const commands = wrapper.findAll('textarea')
    expect(commands).toHaveLength(1)
    expect(commands[0].attributes('id')).toBe('windows-command')
    expect(commands[0].attributes()).toMatchObject({ readonly: '', rows: '2' })
    expect(commands[0].element.closest('[data-onboarding="windows-command"]')).not.toBeNull()
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.get('.s2o-quick-start').element.tagName).toBe('DIV')
    expect(wrapper.findAll('img')).toHaveLength(3)
    for (const image of wrapper.findAll('img')) expect(image.attributes('src')).not.toMatch(/^\/images\//)
    const link = wrapper.findAll('a').find(link => link.attributes('href') === 'https://docs.usegoodai.com/clients/codex')!
    expect(link.attributes()).toMatchObject({ target: '_blank', rel: 'noopener noreferrer' })
    expect(wrapper.text()).not.toContain('title: 快速开始')
    wrapper.unmount()
  })
  it('selects the entire command on double click and reports only a complete manual copy gesture', async () => {
    const wrapper = mount(QuickStartArticle, { attachTo: document.body })
    const input = wrapper.get<HTMLTextAreaElement>('#windows-command')
    const copied = vi.fn(); wrapper.element.addEventListener('s2o:command-copied', copied)
    input.element.focus(); input.element.setSelectionRange(2, 5)
    await input.trigger('copy'); expect(copied).not.toHaveBeenCalled()
    await input.trigger('dblclick')
    expect(input.element.selectionStart).toBe(0); expect(input.element.selectionEnd).toBe(input.element.value.length)
    await input.trigger('copy'); expect(copied).toHaveBeenCalledOnce()
    expect(copied.mock.calls[0][0].detail).toBeNull()
    expect(wrapper.text()).toContain('双击命令行选中整条命令 → Ctrl+C 复制。')
    wrapper.unmount()
  })
})
