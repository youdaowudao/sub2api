import { flushPromises, mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import AccountPerformancePanel from '../AccountPerformancePanel.vue'
import AccountPerformanceTable from '../AccountPerformanceTable.vue'
const { getCacheHitRate, push } = vi.hoisted(() => ({ getCacheHitRate: vi.fn(), push: vi.fn() }))
vi.mock('@/api/admin',()=>({adminAPI:{usage:{getCacheHitRate}}}))
vi.mock('vue-router',()=>({useRouter:()=>({push})}))
vi.mock('vue-i18n',async()=>({...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),useI18n:()=>({t:(key:string)=>key})}))
const range = { start_time:'2026-11-27T08:00:00Z',end_time:'2026-11-27T09:00:00Z' }
const row = { id:1,name:'A',platform:'openai',requests:4,input_tokens:1,cache_creation_tokens:0,cache_read_tokens:1,cache_hit_rate:50,upstream_error_count:1,rate_limit_count:0,account_exists:true,schedulable:true,average_duration_ms:100 }
const result = { start_date:'2026-11-27',end_date:'2026-11-27',window_start:range.start_time,window_end:range.end_time,error_state:'available' as const,items:[row,{...row,id:2,name:'B',requests:0,cache_hit_rate:null,upstream_error_count:3,rate_limit_count:2,average_duration_ms:null}] }
describe('account performance', () => {
 it('loads only when expanded and refreshes the selected window once', async () => {
  getCacheHitRate.mockReset().mockResolvedValue(result)
  const w=mount(AccountPerformancePanel,{props:{range},global:{stubs:{AccountPerformanceTable:true}}})
  expect(getCacheHitRate).not.toHaveBeenCalled()
  await w.get('button').trigger('click');await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(1)
  expect(getCacheHitRate).toHaveBeenLastCalledWith({...range,dimension:'account',include_performance:1})
  await w.setProps({currentRange:{...range,end_time:'2026-11-27T09:01:00Z'}});await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(1)
  await w.findAll('button')[1].trigger('click');await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(2)
  expect(getCacheHitRate).toHaveBeenLastCalledWith(expect.objectContaining({nocache:1,end_time:'2026-11-27T09:01:00Z'}))
  await w.setProps({refreshToken:1});await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(3)
  expect(getCacheHitRate).toHaveBeenLastCalledWith(expect.objectContaining({nocache:1}))
  await w.get('button').trigger('click')
  await w.setProps({range:{...range,end_time:'2026-11-27T09:02:00Z'}});await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(3)
  await w.setProps({currentRange:{...range,end_time:'2026-11-27T09:03:00Z'}})
  await w.get('button').trigger('click');await flushPromises()
  expect(getCacheHitRate).toHaveBeenCalledTimes(4)
  expect(getCacheHitRate).toHaveBeenLastCalledWith(expect.objectContaining({end_time:'2026-11-27T09:03:00Z'}))
  w.unmount()
 })
 it('keeps error-only accounts, sorts by errors and opens the exact upstream detail window', async () => {
  push.mockReset()
  const w=mount(AccountPerformanceTable,{props:{result,loading:false,error:null,filters:{user_id:42,group_id:7,model:'fixture'}}})
  expect(w.findAll('tbody tr')).toHaveLength(2)
  await w.findAll('th button').find(b=>b.text().includes('performance.errors'))!.trigger('click')
  expect(w.findAll('tbody tr')[0].text()).toContain('B')
  await w.findAll('tbody tr')[0].findAll('button')[1].trigger('click')
  expect(push).toHaveBeenLastCalledWith({path:'/admin/ops',query:expect.objectContaining({tr:'custom',start_time:range.start_time,end_time:range.end_time,errors_scope:'upstream',errors_account_id:'2',errors_user_id:'42',group_id:'7',errors_model:'fixture',errors_display:'raw'})})
  await w.setProps({result:{...result,error_state:'monitoring_disabled',items:[{...row,upstream_error_count:null,rate_limit_count:null}]}})
  expect(w.text()).toContain('performance.monitoring_disabled')
  expect(w.find('tbody').text()).toContain('—');w.unmount()
 })
})
