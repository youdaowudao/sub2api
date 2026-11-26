import { afterEach,beforeEach,describe,expect,it,vi } from 'vitest'
import { enableAutoUnmount,flushPromises,mount } from '@vue/test-utils'
import CheckinView from '../CheckinView.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { CheckinConfig } from '@/api/admin/checkin'

const { overview, config, save, users, records, ips }=vi.hoisted(()=>({overview:vi.fn(),config:vi.fn(),save:vi.fn(),users:vi.fn(),records:vi.fn(),ips:vi.fn()}))
vi.mock('@/api/admin/checkin',()=>({adminCheckinAPI:{overview,config,save,users,records,ips}}))
vi.mock('vue-i18n',async()=>({...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),useI18n:()=>({t:(k:string)=>k})}))
const settings:CheckinConfig={enabled:false,veteran_threshold:20,veteran_min:.05,veteran_max:.30,newcomer_min:.01,newcomer_max:.05,newcomer_cap:.05,newcomer_after_cap:.01,newcomer_captcha:true,min_account_age_days:1,ip_daily_limit:3,email_verify_enabled:false,email_verify_first:3,email_verify_every:7,email_verify_skip_reserved:true}
const period={amount:0,users:0,tiers:{}}
const empty={items:[],total:0,page:1,page_size:20,pages:0}
beforeEach(()=>{
  vi.clearAllMocks()
  overview.mockResolvedValue({today:period,yesterday:period,last_7_days:period,last_30_days:period,all_time:period,email_verifications:0,trend:[]})
  config.mockResolvedValue({config:{...settings},config_valid:true,captcha_provider:'turnstile',captcha_available:true})
  save.mockImplementation(async(c)=>({...c}))
  users.mockResolvedValue(empty);records.mockResolvedValue(empty);ips.mockResolvedValue(empty)
})
enableAutoUnmount(afterEach)
async function view(){
  const w=mount(CheckinView,{global:{stubs:{AppLayout:{template:'<div><slot/></div>'},Line:true,DataTable:{props:['columns','data'],emits:['sort'],template:'<div data-testid="table"><div v-for="row in data" :key="row.id||row.user_id||row.ip"><slot name="cell-actions" :row="row"/><slot name="cell-ip" :value="row.ip"/></div></div>'},Pagination:true,RouterLink:true,LoadingSpinner:true}}})
  await flushPromises();return w
}
async function settingsView(){
  const w=await view()
  await w.get('[data-testid=checkin-tab-settings]').trigger('click');await flushPromises()
  return w
}
describe('check-in management',()=>{
  it('greys all descendants, preserves values and toggles only appropriate descendants',async()=>{
    const w=await settingsView()
    const field=(key:string)=>w.get('[data-testid=checkin-field-'+key+']')
    for(const key of Object.keys(settings).filter(k=>k!=='enabled'))expect(field(key).attributes('disabled')).toBeDefined()
    await field('enabled').setValue(true)
    expect(field('veteran_threshold').attributes('disabled')).toBeUndefined()
    expect(field('email_verify_enabled').attributes('disabled')).toBeUndefined()
    expect(field('email_verify_first').attributes('disabled')).toBeDefined()
    expect(field('email_verify_every').attributes('disabled')).toBeDefined()
    expect(field('email_verify_skip_reserved').attributes('disabled')).toBeDefined()
    await field('email_verify_enabled').setValue(true)
    await field('email_verify_first').setValue('11')
    await field('email_verify_enabled').setValue(false)
    expect(field('email_verify_first').attributes('disabled')).toBeDefined()
    expect((field('email_verify_first').element as HTMLInputElement).value).toBe('11')
    await field('enabled').setValue(false)
    await w.get('[data-testid=checkin-save]').trigger('click');await flushPromises()
    expect(save).toHaveBeenCalledWith({...settings,email_verify_first:11})
  })
  it('retains invalid input and displays the server field and reason; successful save clears error',async()=>{
    config.mockResolvedValueOnce({config:{...settings,enabled:true},config_valid:false,captcha_provider:'',captcha_available:false})
    const w=await settingsView()
    expect(w.get('[data-testid=checkin-config-invalid]').exists()).toBe(true)
    expect(w.text()).toContain('admin.checkin.captchaUnavailable')
    const field=w.get('[data-testid=checkin-field-veteran_min]')
    await field.setValue('100.01')
    save.mockRejectedValueOnce({reason:'CHECKIN_CONFIG_INVALID',metadata:{field:'veteran_min'},message:'must be between 0 and 100'})
    await w.get('[data-testid=checkin-save]').trigger('click');await flushPromises()
    expect(w.get('[data-testid=checkin-save-error]').text()).toContain('veteran_min')
    expect(w.get('[data-testid=checkin-save-error]').text()).toContain('must be between 0 and 100')
    expect((field.element as HTMLInputElement).value).toBe('100.01')
    await field.setValue('0.05')
    await w.get('[data-testid=checkin-save]').trigger('click');await flushPromises()
    expect(w.find('[data-testid=checkin-config-invalid]').exists()).toBe(false)
    expect(w.find('[data-testid=checkin-save-error]').exists()).toBe(false)
    expect(w.text()).toContain('admin.checkin.saved')
  })
  it('loads tabs on demand and retains filters when switching tabs',async()=>{
    const w=await view();expect(overview).toHaveBeenCalledWith(30);expect(config).not.toHaveBeenCalled()
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    expect(users).toHaveBeenCalledWith(expect.objectContaining({page:1,sort:'total_amount',order:'desc'}))
    await w.get('form input').setValue('someone@example.com');await w.get('form').trigger('submit');await flushPromises()
    expect(users).toHaveBeenLastCalledWith(expect.objectContaining({search:'someone@example.com'}))
    await w.get('[data-testid=checkin-tab-ips]').trigger('click');await flushPromises()
    expect(ips).toHaveBeenCalledWith(expect.objectContaining({min_users:2}))
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    expect((w.get('form input').element as HTMLInputElement).value).toBe('someone@example.com')
    expect(users).toHaveBeenCalledTimes(2)
  })
  it('sends sort and pagination parameters and opens filtered record tabs',async()=>{
    const row={user_id:101,email:'a@example.test',total_amount:.23}
    users.mockResolvedValue({...empty,items:[row],total:45,pages:3})
    const w=await view()
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    w.getComponent(DataTable).vm.$emit('sort','shared_ip_users','asc');await flushPromises()
    expect(users).toHaveBeenLastCalledWith(expect.objectContaining({sort:'shared_ip_users',order:'asc',page:1}))
    w.getComponent(Pagination).vm.$emit('update:page',2);await flushPromises()
    expect(users).toHaveBeenLastCalledWith(expect.objectContaining({page:2,sort:'shared_ip_users'}))
    w.getComponent(Pagination).vm.$emit('update:pageSize',10);await flushPromises()
    expect(users).toHaveBeenLastCalledWith(expect.objectContaining({page:1,page_size:10}))
    const detail=w.findAll('button').find(b=>b.text()==='admin.checkin.viewRecords')
    expect(detail).toBeDefined();await detail!.trigger('click');await flushPromises()
    expect(records).toHaveBeenLastCalledWith(expect.objectContaining({search:'#101',ip:''}))
  })
  it('retains invalid date inputs so the administrator can correct them after an error',async()=>{
    const w=await view()
    await w.get('[data-testid=checkin-tab-records]').trigger('click');await flushPromises()
    const dates=w.findAll('input[type=date]')
    await dates[0].setValue('2026-11-26');await dates[1].setValue('2026-11-25')
    records.mockRejectedValueOnce({message:'起始日期不能晚于结束日期'})
    await w.get('form').trigger('submit');await flushPromises()
    expect(w.text()).toContain('起始日期不能晚于结束日期')
    expect((w.findAll('input[type=date]')[0].element as HTMLInputElement).value).toBe('2026-11-26')
    await w.findAll('input[type=date]')[1].setValue('2026-11-27')
    await w.get('form').trigger('submit');await flushPromises()
    expect(records).toHaveBeenLastCalledWith(expect.objectContaining({date_from:'2026-11-26',date_to:'2026-11-27'}))
    expect(w.text()).not.toContain('admin.checkin.loadFailed')
  })
  it('invalidates cached user statistics after settings are saved but preserves filters',async()=>{
    const w=await view()
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    await w.get('form input').setValue('user@example.test');await w.get('form').trigger('submit');await flushPromises()
    const before=users.mock.calls.length
    await w.get('[data-testid=checkin-tab-settings]').trigger('click');await flushPromises()
    await w.get('[data-testid=checkin-field-enabled]').setValue(true)
    await w.get('[data-testid=checkin-save]').trigger('click');await flushPromises()
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    expect(users).toHaveBeenCalledTimes(before+1)
    expect(users).toHaveBeenLastCalledWith(expect.objectContaining({search:'user@example.test'}))
  })
  it('does not let an older user-list response overwrite a post-save reload',async()=>{
    let resolveOld!: (value:object)=>void
    users.mockReturnValueOnce(new Promise(resolve=>{resolveOld=resolve}))
    const w=await view()
    await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    await w.get('[data-testid=checkin-tab-settings]').trigger('click');await flushPromises()
    let resolveSave!: (value:CheckinConfig)=>void
    save.mockReturnValueOnce(new Promise(resolve=>{resolveSave=resolve}))
    await w.get('[data-testid=checkin-field-enabled]').setValue(true)
    await w.get('[data-testid=checkin-save]').trigger('click');await flushPromises()
    await w.get('[data-testid=checkin-tab-users]').trigger('click')
    const fresh={user_id:102,email:'fresh@example.test',tier:'veteran'}
    users.mockResolvedValueOnce({...empty,items:[fresh],total:1})
    resolveSave({...settings,enabled:true});await flushPromises()
    expect(users).toHaveBeenCalledTimes(2)
    resolveOld({...empty,items:[{user_id:101,email:'old@example.test'}],total:1});await flushPromises()
    expect(w.getComponent(DataTable).props('data')).toEqual([fresh])
  })
  it('retries a failed loading state',async()=>{
    users.mockRejectedValueOnce(new Error('offline'))
    const w=await view();await w.get('[data-testid=checkin-tab-users]').trigger('click');await flushPromises()
    expect(w.text()).toContain('admin.checkin.loadFailed')
    await w.get('[role=alert] button').trigger('click');await flushPromises()
    expect(users).toHaveBeenCalledTimes(2);expect(w.text()).not.toContain('admin.checkin.loadFailed')
  })
})
