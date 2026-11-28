import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PaginatedResponse } from '@/types'
import type { VideoTask } from '@/api/admin/videoTasks'
import VideoTasksView from '../VideoTasksView.vue'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { videoTasks: { list } } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

enableAutoUnmount(afterEach)

const refund: VideoTask = {
  task_id: 'video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  user_id: 12, user_email: '', user_name: '', group_id: 15, group_name: '', account_id: 17, account_name: '', account_deleted: false,
  model: 'site-video', upstream_model: 'provider-video', seconds: 5, resolution: '480p',
  hold_amount: 2.5, status: 'failed', billing_state: 'refunded',
  failure_reason: 'tracking_expired', upstream_task_id: '',
  created_at: '2026-11-24T03:00:00Z', settled_at: '2026-11-24T08:00:00Z', risk: true
}

function page(items: VideoTask[], total = items.length): PaginatedResponse<VideoTask> {
  return { items, total, page: 1, page_size: 20, pages: Math.max(1, Math.ceil(total / 20)) }
}

function render() {
  return mount(VideoTasksView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="item in options" :key="item.value" :value="item.value">{{ item.label }}</option></select>'
        },
        Pagination: {
          emits: ['update:page'],
          template: '<button data-test="next-page" @click="$emit(\'update:page\', 2)">next</button>'
        }
      }
    }
  })
}

describe('VideoTasksView', () => {
  beforeEach(() => {
    list.mockReset()
    localStorage.clear()
    vi.stubGlobal('matchMedia', () => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    }))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders the original freeze and warns about provider cost only where returned by the API', async () => {
    const completed: VideoTask = {
      ...refund, task_id: 'video_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', status: 'completed',
      billing_state: 'charged', upstream_task_id: 'safe-provider-id', failure_reason: '', risk: false
    }
    list.mockResolvedValue(page([refund, completed]))
    const view = render()
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    const [failedRow, completedRow] = view.findAll('tbody tr')
    expect(failedRow.text()).toContain('2.5000')
    expect(failedRow.text()).toContain('admin.videoTasks.reasons.tracking_expired')
    expect(failedRow.text()).toContain('admin.videoTasks.providerCostBadge')
    expect(failedRow.text()).not.toContain('safe-provider-id')
    expect(completedRow.text()).toContain('safe-provider-id')
    expect(completedRow.text()).not.toContain('admin.videoTasks.providerCostBadge')
  })

  it('shows names rather than IDs and preserves column choices across remounts', async () => {
    const normal = { ...refund, user_email: 'customer@example.com', user_name: 'Customer', group_name: 'Video group', account_name: 'Video account' }
    const deleted = { ...normal, task_id: 'video_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', account_name: 'Old account', account_deleted: true }
    list.mockResolvedValue(page([normal, deleted, refund]))
    let view = render()
    await flushPromises()
    const rows = view.findAll('tbody tr')
    expect(rows[0].text()).toContain('customer@example.com')
    expect(rows[0].text()).toContain('Customer')
    expect(rows[0].text()).toContain('Video account')
    expect(rows[0].text()).not.toContain('Video group')
    expect(rows[1].text()).toContain('Old account')
    expect(rows[1].text()).toContain('admin.videoTasks.deleted')
    expect(rows[2].text()).toContain('admin.videoTasks.deletedAccount')
    for (const row of rows) for (const id of ['#12', '#15', '#17']) expect(row.text()).not.toContain(id)
    async function openColumns() {
      await view.get('button[title="admin.videoTasks.columnSettings"]').trigger('click')
    }
    function option(key: string) {
      return view.findAll('button[aria-pressed]').find(button => button.text() === 'admin.videoTasks.columns.' + key)!
    }
    await openColumns()
    expect(option('task_id')).toBeUndefined()
    await option('group_id').trigger('click')
    await option('account_id').trigger('click')
    expect(view.text()).toContain('Video group')
    expect(view.text()).not.toContain('Video account')
    expect(list).toHaveBeenCalledTimes(1)
    view.unmount()
    view = render()
    await flushPromises()
    expect(view.text()).toContain('Video group')
    expect(view.text()).not.toContain('Video account')
    await openColumns()
    await option('account_id').trigger('click')
    view.unmount()
    view = render()
    await flushPromises()
    expect(view.text()).toContain('Video account')
    expect(view.text()).toContain(normal.task_id)
  })

  it('returns to the first page for filters and ignores an older failed response', async () => {
    list.mockResolvedValueOnce(page([refund], 31))
    const view = render()
    await flushPromises()

    let rejectOld!: (reason: Error) => void
    list.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
    await view.get('[data-test="next-page"]').trigger('click')
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    list.mockResolvedValueOnce(page([]))
    await view.get('select').setValue('confirmation')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, status: 'confirmation' }))
    expect(view.text()).toContain('admin.videoTasks.empty')
    rejectOld(new Error('superseded'))
    await flushPromises()
    expect(view.text()).not.toContain('admin.videoTasks.loadFailed')
    expect(view.text()).not.toContain(refund.task_id)
  })

  it('allows retry after a failure with the current filters', async () => {
    list.mockRejectedValueOnce(new Error('local fixture outage'))
    const view = render()
    await flushPromises()
    expect(view.text()).toContain('admin.videoTasks.loadFailed')
    expect(view.text()).not.toContain('admin.videoTasks.empty')
    list.mockResolvedValueOnce(page([refund]))
    await view.get('[data-test="retry-video-tasks"]').trigger('click')
    await flushPromises()
    expect(view.text()).toContain(refund.task_id)
    expect(view.text()).not.toContain('admin.videoTasks.loadFailed')
  })

  it('identifies invalid filters without requesting a list and accepts corrected values', async () => {
    list.mockResolvedValue(page([refund]))
    const view = render()
    await flushPromises()
    const [userID, taskID] = view.findAll('form input[type=text]')
    const form = view.get('form')

    for (const invalid of ['word', '0', '9223372036854775808']) {
      await userID.setValue(invalid)
      await form.trigger('submit')
      expect(view.text()).toContain('admin.videoTasks.invalidUserId')
      expect(view.text()).toContain(refund.task_id)
      expect(list).toHaveBeenCalledTimes(1)
    }
    await userID.setValue('9223372036854775807')
    await taskID.setValue('video_not_a_site_task_id')
    await form.trigger('submit')
    expect(view.text()).toContain('admin.videoTasks.invalidTaskId')
    expect(view.text()).toContain(refund.task_id)
    expect(view.text()).not.toContain('admin.videoTasks.loadFailed')
    expect(view.find('[data-test="retry-video-tasks"]').exists()).toBe(false)
    expect(list).toHaveBeenCalledTimes(1)

    await taskID.setValue(refund.task_id)
    await form.trigger('submit')
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: '9223372036854775807', task_id: refund.task_id
    }))
    expect(view.text()).not.toContain('admin.videoTasks.invalidTaskId')
    expect(view.text()).toContain(refund.task_id)
  })

  it('ignores old rows that arrive after the latest filter results', async () => {
    let resolveOld!: (value: PaginatedResponse<VideoTask>) => void
    list.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const view = render()
    list.mockResolvedValueOnce(page([]))
    await view.get('select').setValue('completed')
    await flushPromises()
    resolveOld(page([refund]))
    await flushPromises()
    expect(view.text()).toContain('admin.videoTasks.empty')
    expect(view.text()).not.toContain(refund.task_id)
  })
})
