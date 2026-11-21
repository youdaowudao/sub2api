import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountScheduleTimer from '../AccountScheduleTimer.vue'
import zh from '@/i18n/locales/zh/admin/accounts'
import type { Account } from '@/types'

vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params: Record<string, unknown> = {}) => {
    let value: unknown = { admin: zh }
    for (const part of key.split('.')) value = (value as Record<string, unknown> | undefined)?.[part]
    return typeof value === 'string' ? value.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
  }
}) }))

const resumeAt = '2026-11-20T06:00:00Z'
const account = (changes: Partial<Account> = {}) => ({
  id: 17, name: 'Timer account', schedulable: true, schedule_resume_at: null, ...changes
}) as Account
const wrappers: ReturnType<typeof mount>[] = []
function mountTimer(value = account()) {
  const wrapper = mount(AccountScheduleTimer, {
    props: { account: value }, attachTo: document.body
  })
  wrappers.push(wrapper)
  return wrapper
}
async function choose(wrapper: ReturnType<typeof mountTimer>, hours: number) {
  await wrapper.get('button').trigger('click')
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
    .find(el => el.textContent?.trim() === `暂停 ${hours} 小时`)
  expect(option).toBeDefined()
  option!.click()
  await flushPromises()
}

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})

describe('AccountScheduleTimer', () => {
  it.each([6, 12, 24])('asks the page to pause for %s hours without changing the switch itself', async hours => {
    const wrapper = mountTimer()
    await choose(wrapper, hours)
    expect(wrapper.emitted('start')).toEqual([[hours]])
    expect(wrapper.props('account').schedulable).toBe(true)
    await wrapper.setProps({ account: account({ schedulable: false, schedule_resume_at: resumeAt }) })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('自动开启调度')
  })

  it.each([
    account({ schedulable: false }),
    account({ schedule_resume_at: resumeAt })
  ])('does not submit for a closed account or an existing timer', async value => {
    const wrapper = mountTimer(value)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[role="option"]')).toBeNull()
    expect(wrapper.emitted('start')).toBeUndefined()
  })

  it('offers exactly the three approved durations', async () => {
    const wrapper = mountTimer()
    await wrapper.get('button').trigger('click')
    expect([...document.querySelectorAll('[role="option"]')].map(el => el.textContent?.trim()))
      .toEqual(['暂停 6 小时', '暂停 12 小时', '暂停 24 小时'])
  })

  it.each([{ busy: true }, { disabled: true }])('uses the page-owned pending and disabled state: %j', async state => {
    const wrapper = mountTimer()
    await wrapper.setProps(state)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')
    expect(document.querySelector('[role="option"]')).toBeNull()
    expect(wrapper.emitted('start')).toBeUndefined()
  })
})
