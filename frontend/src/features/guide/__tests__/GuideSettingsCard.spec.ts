import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive, nextTick, h } from 'vue'
import GuideSettingsCard from '../components/GuideSettingsCard.vue'
import { normalizeConfig, previewConfigKey } from '../config'

const { fetchPublicSettings, store } = vi.hoisted(() => ({ fetchPublicSettings: vi.fn(), store: { value: null as any } }))
vi.mock('@/stores', () => ({ useAppStore: () => store.value }))
const menu = (id = 'onboarding-quick-start', label = '已保存的快速开始') => ({ id, label, url: 'md:old', visibility: 'user' })
let wrapper: VueWrapper | undefined
const button = (text: string) => wrapper!.findAll('button').find(button => button.text() === text)!
function create() { wrapper = mount(GuideSettingsCard); return wrapper }

describe('admin guide configuration card', () => {
  beforeEach(() => {
    localStorage.clear(); fetchPublicSettings.mockReset()
    store.value = reactive({ cachedPublicSettings: null, fetchPublicSettings })
    fetchPublicSettings.mockResolvedValue({ custom_menu_items: [] })
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.doUnmock('../config/onboarding.json'); vi.resetModules() })
  it('shows loading without claiming a missing binding, then uses only saved user-visible menus', async () => {
    let resolve!: (value: any) => void
    fetchPublicSettings.mockReturnValue(new Promise(done => { resolve = done }))
    create()
    expect(wrapper!.text()).toContain('正在读取已保存的自定义菜单…')
    expect(wrapper!.text()).not.toContain('在已保存的自定义菜单中找不到')
    expect(button('重新读取').attributes('disabled')).toBeDefined()
    expect(wrapper!.get('select').element.value).toBe('onboarding-quick-start')
    store.value.cachedPublicSettings = { custom_menu_items: [menu(), menu('saved', '已保存'), { ...menu('admin'), visibility: 'admin' }, menu('', '未保存')] }
    resolve(store.value.cachedPublicSettings); await flushPromises()
    expect(wrapper!.text()).toContain('这个菜单里填写的 URL 不生效')
    expect(wrapper!.findAll('option').map(option => option.attributes('value'))).toEqual(['', 'onboarding-quick-start', 'saved'])
    expect(wrapper!.get('[data-guide-current]').findAll('input')).toHaveLength(0)
    const unsaved = [menu('draft', '未保存草稿')]
    unsaved[0].label = '修改但没保存'; await nextTick()
    expect(wrapper!.findAll('option').map(option => option.attributes('value'))).not.toContain('draft')
    store.value.cachedPublicSettings = { custom_menu_items: [menu('saved-after', '保存后')] }; await nextTick()
    expect(wrapper!.findAll('option').map(option => option.attributes('value'))).toContain('saved-after')
  })
  it.each([false, true])('distinguishes a failed read with cached=%s and retains the selected binding for export/preview', async cached => {
    if (cached) store.value.cachedPublicSettings = { custom_menu_items: [menu()] }
    fetchPublicSettings.mockResolvedValue(null)
    create(); await flushPromises()
    expect(wrapper!.text()).toContain(cached ? '下面显示的是上次读到的结果' : '现在无法判断绑定的菜单是否存在')
    if (!cached) expect(wrapper!.text()).not.toContain('在已保存的自定义菜单中找不到')
    expect(wrapper!.get('select').element.value).toBe('onboarding-quick-start')
    await button('仅当前浏览器试用').trigger('click')
    expect(normalizeConfig(JSON.parse(localStorage.getItem(previewConfigKey)!)).modules).toHaveLength(4)
    expect(button('导出配置文件').attributes('disabled')).toBeUndefined()
  })
  it('waits for an in-flight read and only then forces a new read on retry', async () => {
    let done!: (value: any) => void
    fetchPublicSettings.mockResolvedValueOnce(null)
    create(); await flushPromises()
    fetchPublicSettings.mockReturnValueOnce(new Promise(resolve => { done = resolve })).mockResolvedValueOnce({})
    await button('重新读取').trigger('click')
    expect(fetchPublicSettings.mock.calls).toEqual([[], []])
    expect(button('重新读取').attributes('disabled')).toBeDefined()
    done({}); await flushPromises()
    expect(fetchPublicSettings.mock.calls).toEqual([[], [], [true]])
    expect(button('重新读取').attributes('disabled')).toBeUndefined()
  })
  it('validates a draft, previews it locally, clears only the preview and exports without changing the build', async () => {
    store.value.cachedPublicSettings = { custom_menu_items: [menu()] }
    create(); await flushPromises()
    await wrapper!.get('[data-guide-module="recharge"]').setValue(false)
    await button('仅当前浏览器试用').trigger('click')
    expect(wrapper!.text()).toContain('只对当前浏览器生效')
    const preview = normalizeConfig(JSON.parse(localStorage.getItem(previewConfigKey)!))
    expect(preview.modules.find(module => module.id === 'recharge')?.enabled).toBe(false)
    expect(wrapper!.get('[data-guide-current]').text()).toContain('余额充值：已启用')
    const createURL = vi.fn(() => 'blob:test'), revokeURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeURL })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await button('导出配置文件').trigger('click')
    expect(createURL).toHaveBeenCalledOnce(); expect(revokeURL).toHaveBeenCalledWith('blob:test')
    expect(localStorage.getItem(previewConfigKey)).not.toBeNull()
    await button('清除试用').trigger('click'); expect(localStorage.getItem(previewConfigKey)).toBeNull()
    expect(wrapper!.text()).toContain('已经打开的试用页面需要刷新一次')
    await wrapper!.get('select').setValue(''); await button('仅当前浏览器试用').trigger('click')
    expect(wrapper!.get('[role="alert"]').text()).toBe('启用快速开始时，需要选择一个自定义菜单。')
  })
  it('reports storage failures and leaves the previous draft intact', async () => {
    create(); await flushPromises(); localStorage.setItem(previewConfigKey, 'existing')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Denied') })
    await button('仅当前浏览器试用').trigger('click')
    expect(wrapper!.get('[role="alert"]').text()).toBe('当前浏览器无法保存试用配置，可以直接导出文件。')
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new DOMException('Denied') })
    await button('清除试用').trigger('click')
    expect(wrapper!.get('[role="alert"]').text()).toBe('当前浏览器无法清除试用配置，试用配置仍然保留。')
    expect(localStorage.getItem(previewConfigKey)).toBe('existing')
  })
  it.each([false, true])('isolates invalid build configuration while other settings work and preview clearing fails=%s', async clearFails => {
    vi.resetModules()
    vi.doMock('../config/onboarding.json', () => ({ default: { schemaVersion: 1, modules: [{ id: 'unknown', enabled: true }] } }))
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const InvalidCard = (await import('../components/GuideSettingsCard.vue')).default
    expect((await import('../config')).configuration).toBeNull()
    const otherSettings = vi.fn()
    localStorage.setItem(previewConfigKey, 'previous-browser-draft')
    if (clearFails) vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new DOMException('Denied') })
    expect(() => {
      wrapper = mount({ render: () => h('section', [h('button', { 'data-other-setting': '', onClick: otherSettings }, '其他设置操作'), h(InvalidCard)]) })
    }).not.toThrow()
    await flushPromises()
    const card = wrapper!.get('[data-guide-settings]')
    expect(card.text()).toContain('当前全站引导配置无效，普通用户引导已关闭。')
    expect(card.text()).toContain('章节配置不正确。')
    expect(card.text()).toContain('请检查配置文件并重新构建，修正后刷新页面重试。其他设置可以继续使用。')
    expect(card.find('[data-guide-current], fieldset, select, [data-guide-module]').exists()).toBe(false)
    expect(card.findAll('button').map(button => button.text())).toEqual(['清除试用'])
    expect(localStorage.getItem(previewConfigKey)).toBe('previous-browser-draft')
    await wrapper!.get('[data-other-setting]').trigger('click'); expect(otherSettings).toHaveBeenCalledOnce()
    await button('清除试用').trigger('click')
    expect(localStorage.getItem(previewConfigKey)).toBe(clearFails ? 'previous-browser-draft' : null)
    expect(card.text()).toContain(clearFails ? '当前浏览器无法清除试用配置，试用配置仍然保留。' : '已清除试用配置。')
    expect(card.text()).toContain('章节配置不正确。') // Clearing a browser draft does not repair the build.
  })
})
