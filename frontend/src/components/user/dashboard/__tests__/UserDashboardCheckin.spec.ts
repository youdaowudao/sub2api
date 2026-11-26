import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import DashboardView from '@/views/user/DashboardView.vue'
import type { CheckinDetails } from '@/api/checkin'
import zh from '@/i18n/locales/zh/checkin'

const { get, records, submit, sendCode, refreshUser, applyBalance, action, reset, stats } = vi.hoisted(()=>({
  get:vi.fn(),records:vi.fn(),submit:vi.fn(),sendCode:vi.fn(),refreshUser:vi.fn(),applyBalance:vi.fn(),
  action:vi.fn(),reset:vi.fn(),stats:vi.fn(),
}))
vi.mock('@/api/checkin',()=>({checkinAPI:{get,records,submit,sendCode}}))
vi.mock('@/api/usage',()=>({usageAPI:{getDashboardStats:stats,getDashboardTrend:vi.fn().mockResolvedValue({trend:[]}),getDashboardModels:vi.fn().mockResolvedValue({models:[]}),getByDateRange:vi.fn().mockResolvedValue({items:[]})}}))
vi.mock('@/api/user',()=>({getMyPlatformQuotas:vi.fn().mockResolvedValue({platform_quotas:[]})}))
vi.mock('@/stores/auth',()=>({useAuthStore:()=>({user:{id:1,balance:12.34},isSimpleMode:false,refreshUser,applyBalance})}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({contactInfo:'',showWarning:vi.fn()})}))
vi.mock('vue-i18n',async()=>({...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),useI18n:()=>({locale:ref('zh'),t:(key:string,params:Record<string,string|number>={})=> {
  const value = key.startsWith('checkin.') ? key.slice(8).split('.').reduce<unknown>((node,k)=>node && typeof node==='object' ? (node as Record<string,unknown>)[k] : undefined,zh) : key
  return String(value||key).replace(/\{(\w+)\}/g,(_,k)=>String(params[k]??''))
}})}))
let mounted = 0
const CaptchaStub = defineComponent({
  props:['aliyunSceneId','tencentEnabled','turnstileEnabled','aliyunEnabled','turnstileSiteKey'],
  emits:['verify','error','expire'],
  setup(props,{expose}){const instance=++mounted;expose({reset,verifyAction:()=>action(props.aliyunSceneId,instance)});return{instance}},
  template:'<button data-testid="mock-captcha" :data-instance="instance" :data-scene="aliyunSceneId" @click="$emit(\'verify\',\'fake-proof\',\'\')">mock</button>',
})
const base = (overrides:Partial<CheckinDetails>={}):CheckinDetails=>({
  enabled:true,status:'available',today:'2026-11-26',seconds_until_reset:3600,registered_month:'2026-10',
  needs_captcha:false,needs_email:false,today_amount:null,stats:{total_days:1,total_amount:.1,streak:1},
  month:'2026-11',month_amount:.1,records:[{date:'2026-11-25',amount:.1,time:'12:00'}],...overrides,
})
const result={amount:.23,balance:12.57,checkin_date:'2026-11-26',time:'09:12',month:'2026-11',month_amount:.33,total_days:2,total_amount:.33,streak:2}
const stubs={AppLayout:{template:'<div><header><slot name="header-actions"/></header><slot/></div>'},UserDashboardQuickStart:true,UserDashboardQuickActions:true,Doughnut:true,TokenUsageTrend:true,Icon:true,CaptchaChallenge:CaptchaStub,RouterLink:{template:'<a><slot/></a>'}}
enableAutoUnmount(afterEach)
beforeEach(()=>{
  vi.useFakeTimers();vi.clearAllMocks();mounted=0
  get.mockResolvedValue(base());submit.mockResolvedValue(result)
  records.mockResolvedValue({month:'2026-10',month_amount:0,records:[]})
  sendCode.mockResolvedValue({masked_email:'e***@example.com',cooldown_seconds:60})
  action.mockResolvedValue({token:'proof',randstr:''})
  stats.mockResolvedValue({by_platform:[{platform:'openai',total_requests:1,total_tokens:5,total_actual_cost:.1,today_actual_cost:.1}]})
  vi.stubGlobal('matchMedia',vi.fn().mockImplementation(()=>({matches:true,addEventListener:vi.fn(),removeEventListener:vi.fn()})))
})
afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals()})
async function view(){const w=mount(DashboardView,{attachTo:document.body,global:{stubs}});await flushPromises();return w}

describe('daily check-in dashboard',()=>{
  it.each(['checkin-header','checkin-submit'])('uses one GET and synchronizes both entries via %s without refreshing auth',async(id)=>{
    const w=await view()
    expect(get).toHaveBeenCalledTimes(1);const refreshCount=refreshUser.mock.calls.length
    expect(w.findAll('.daily-cell')).toHaveLength(42)
    await w.get('[data-testid="'+id+'"]').trigger('click');await flushPromises()
    expect(submit).toHaveBeenCalledTimes(1); expect(submit).toHaveBeenCalledWith({})
    expect(applyBalance).toHaveBeenCalledTimes(1); expect(applyBalance).toHaveBeenCalledWith(12.57)
    expect(refreshUser).toHaveBeenCalledTimes(refreshCount)
    expect(w.get('[data-testid="checkin-header"]').text()).toBe('已签到')
    expect(w.get('[data-testid="checkin-header"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="checkin-amount"]').text()).toBe('+¥0.23')
    expect(w.get('[data-date="2026-11-26"]').attributes('data-signed')).toBe('true')
    expect(get).toHaveBeenCalledTimes(1)
  })
  it('restores exact original layout after disabled response and never changes it on initial disabled GET',async()=>{
    get.mockResolvedValueOnce({enabled:false})
    const w=await view()
    expect(w.find('[data-testid=checkin-card]').exists()).toBe(false)
    expect(w.find('[data-testid=checkin-header]').exists()).toBe(false)
    expect(w.get('[data-testid=dashboard-chart-pair]').classes()).toContain('lg:grid-cols-2')
    expect(w.get('#dashboard-checkin-platform').attributes('hidden')).toBeDefined()
    expect(w.get('#dashboard-checkin-platform').text()).toBe('')
    w.unmount()
    const on=await view()
    expect(on.get('#dashboard-checkin-platform').text()).toContain('dashboard.platformBreakdown')
    expect(on.get('[data-testid=dashboard-chart-pair]').classes()).not.toContain('lg:grid-cols-2')
    submit.mockRejectedValueOnce({reason:'CHECKIN_DISABLED'});get.mockResolvedValueOnce({enabled:false})
    await on.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(on.find('[data-testid=checkin-card]').exists()).toBe(false)
    expect(on.find('[data-testid=checkin-header]').exists()).toBe(false)
    expect(on.get('[data-testid=dashboard-chart-pair]').classes()).toContain('lg:grid-cols-2')
    expect(on.get('#dashboard-checkin-platform').text()).toBe('')
    expect(on.text()).toContain('dashboard.platformBreakdown')
  })
  it('keeps configuration error card without calendar or timers',async()=>{
    get.mockResolvedValueOnce({enabled:true,status:'unavailable'})
    const w=await view()
    expect(w.get('[data-testid=checkin-card]').text()).toContain('签到暂不可用')
    expect(w.findAll('.daily-cell')).toHaveLength(0)
    expect(w.find('.daily-metrics').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(86400000)
    expect(get).toHaveBeenCalledTimes(1)
  })
  it('signs while viewing prior month, updates cached current month, then refreshes at midnight',async()=>{
    get.mockResolvedValueOnce(base({seconds_until_reset:1}))
    const w=await view()
    await w.get('[data-testid=checkin-prev]').trigger('click');await flushPromises()
    expect(records).toHaveBeenCalledTimes(1); expect(records).toHaveBeenCalledWith('2026-10')
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(w.get('.daily-month strong').text()).toContain('10 月')
    expect(w.get('.daily-metrics').text()).toContain('¥0.00')
    await w.get('[data-testid=checkin-next]').trigger('click');await flushPromises()
    expect(w.get('[data-date="2026-11-26"]').attributes('data-signed')).toBe('true')
    expect(w.get('.daily-metrics').text()).toContain('¥0.33')
    get.mockResolvedValueOnce(base({today:'2026-11-27',seconds_until_reset:3600}))
    await vi.advanceTimersByTimeAsync(1000);await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect(w.get('[data-testid=checkin-header]').text()).toBe('签到')
  })
  it('reloads immediately when cross-midnight done has no amount or reliable streak',async()=>{
    get.mockResolvedValueOnce(base({status:'done',seconds_until_reset:0,today_amount:null,stats:{total_days:9,total_amount:1,streak:9}}))
    get.mockResolvedValueOnce(base({status:'done',today:'2026-11-27',today_amount:.11}))
    const w=await view()
    expect(w.find('[data-testid=checkin-amount]').exists()).toBe(false)
    expect(w.find('.daily-streak').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(0);await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect(w.get('[data-testid=checkin-amount]').text()).toContain('0.11')
  })
  it('requires email, preserves input on invalid code and enforces cooldown from response',async()=>{
    get.mockResolvedValueOnce(base({needs_email:true,masked_email:'e***@example.com'}))
    const w=await view()
    await w.get('[data-testid=checkin-header]').trigger('click');await flushPromises()
    expect(submit).not.toHaveBeenCalled()
    sendCode.mockRejectedValueOnce({reason:'VERIFY_CODE_TOO_FREQUENT'})
    await w.get('[data-testid=checkin-send]').trigger('click');await flushPromises()
    expect(w.get('[data-testid=checkin-send]').text()).toContain('60')
    expect(w.get('[data-testid=checkin-send]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid=checkin-email-code]').setValue('123456')
    submit.mockRejectedValueOnce({reason:'INVALID_VERIFY_CODE'})
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(submit).toHaveBeenCalledWith({email_code:'123456'})
    expect((w.get('[data-testid=checkin-email-code]').element as HTMLInputElement).value).toBe('123456')
    expect(w.text()).toContain('验证码错误或已过期')
    await vi.advanceTimersByTimeAsync(60000)
    expect(w.get('[data-testid=checkin-send]').attributes('disabled')).toBeUndefined()
  })
  it('does not claim unchanged balance when the POST result cannot be confirmed',async()=>{
    const w=await view()
    submit.mockRejectedValueOnce(new Error('connection lost'))
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(w.get('[data-testid=checkin-card]').text()).toContain('无法确认签到结果')
    expect(w.get('[data-testid=checkin-card]').text()).not.toContain('余额未变动')
  })
  it.each([
    [{reason:'CHECKIN_FAILED',metadata:{result_unknown:'true'}},'无法确认签到结果',false],
    [{reason:'CHECKIN_FAILED'},'余额未变动',true],
  ] as const)('distinguishes an uncertain database commit from confirmed failure',async(error,text,confirmed)=>{
    const w=await view()
    submit.mockRejectedValueOnce(error)
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    const message=w.get('[data-testid=checkin-card]').text()
    expect(message).toContain(text)
    expect(message.includes('余额未变动')).toBe(confirmed)
    expect(submit).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledTimes(1)
  })
  it('switches service provider from Turnstile to Tencent after failed proof',async()=>{
    get.mockResolvedValueOnce(base({needs_captcha:true,captcha_provider:'turnstile',captcha_public:{site_key:'old-site'}}))
    const w=await view()
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    submit.mockRejectedValueOnce({reason:'CHECKIN_CAPTCHA_FAILED'})
    get.mockResolvedValueOnce(base({needs_captcha:true,captcha_provider:'tencent',captcha_public:{app_id:'new-app',region:'cn'}}))
    await w.get('[data-testid=mock-captcha]').trigger('click');await flushPromises()
    expect(reset).toHaveBeenCalled()
    expect(w.text()).toContain('人机验证未通过，请重试')
    action.mockResolvedValueOnce({token:'ticket',randstr:'random'})
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(submit).toHaveBeenLastCalledWith({captcha_token:'ticket',captcha_randstr:'random'})
    expect(w.get('[data-testid=checkin-header]').text()).toBe('已签到')
  })
  it('remounts an existing Aliyun widget when public SceneID changes on reload',async()=>{
    get.mockResolvedValueOnce(base({needs_captcha:true,captcha_provider:'aliyun',captcha_public:{scene_id:'first',prefix:'prefix',region:'cn'}}))
    const w=await view()
    submit.mockRejectedValueOnce({reason:'CHECKIN_CAPTCHA_FAILED'})
    get.mockResolvedValueOnce(base({needs_captcha:true,captcha_provider:'aliyun',captcha_public:{scene_id:'second',prefix:'prefix',region:'cn'}}))
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    const first=action.mock.calls[0][1]
    const next=w.get('[data-testid=mock-captcha]')
    expect(next.attributes('data-scene')).toBe('second')
    expect(Number(next.attributes('data-instance'))).not.toBe(first)
    await w.get('[data-testid=checkin-submit]').trigger('click');await flushPromises()
    expect(action.mock.calls[1][0]).toBe('second')
    expect(action.mock.calls[1][1]).not.toBe(first)
    expect(w.get('[data-testid=checkin-header]').text()).toBe('已签到')
  })
  it.each([
    ['bind_email','为保障账户安全，请先绑定邮箱后继续签到'],
    ['too_new','11-27 08:00 起可以签到'],
    ['unavailable','签到暂不可用，请稍后再试'],
  ] as const)('renders neutral nonavailable status %s',async(status,text)=>{
    get.mockResolvedValueOnce(base({status,available_at:'2026-11-27T08:00:00+08:00'}))
    const w=await view();expect(w.get('[data-testid=checkin-card]').text()).toContain(text)
    expect(w.get('[data-testid=checkin-card]').text()).not.toMatch(/充值|门槛|新用户/)
    expect(submit).not.toHaveBeenCalled()
  })
})
