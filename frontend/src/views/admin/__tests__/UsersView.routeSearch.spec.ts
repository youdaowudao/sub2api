import { afterEach,beforeEach,describe,expect,it,vi } from 'vitest'
import { enableAutoUnmount,flushPromises,mount } from '@vue/test-utils'
import UsersView from '../UsersView.vue'
const { list, route }=vi.hoisted(()=>({list:vi.fn(),route:{query:{} as Record<string,unknown>}}))
vi.mock('vue-router',async()=>({...await vi.importActual<typeof import('vue-router')>('vue-router'),useRoute:()=>route}))
vi.mock('@/api/admin',()=>({adminAPI:{users:{list},groups:{getAll:vi.fn().mockResolvedValue([])},userAttributes:{listEnabledDefinitions:vi.fn().mockResolvedValue([])},dashboard:{getBatchUsersUsage:vi.fn().mockResolvedValue({stats:{}})}}}))
vi.mock('@/stores/app',()=>({useAppStore:()=>({showError:vi.fn(),showSuccess:vi.fn()})}))
vi.mock('vue-i18n',async()=>({...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),useI18n:()=>({t:(k:string)=>k})}))
enableAutoUnmount(afterEach)
beforeEach(()=>{localStorage.clear();list.mockReset();list.mockResolvedValue({items:[],total:0,page:1,page_size:20,pages:0});route.query={}})
async function view(){
  const w=mount(UsersView,{global:{stubs:{AppLayout:{template:'<div><slot/></div>'},TablePageLayout:{template:'<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>'},DataTable:true,Pagination:true,ConfirmDialog:true,EmptyState:true,GroupBadge:true,Select:true,UserAttributesConfigModal:true,UserConcurrencyCell:true,UserCreateModal:true,UserEditModal:true,BulkEditUserModal:true,UserPlatformQuotaModal:true,UserApiKeysModal:true,UserAllowedGroupsModal:true,UserBalanceModal:true,UserBalanceHistoryModal:true,GroupReplaceModal:true,Icon:true}}})
  await flushPromises();return w
}
describe('admin users route search',()=>{
  it('uses the search from the check-in link before the very first request',async()=>{
    route.query={search:'someone@example.com'}
    const w=await view()
    expect((w.get('input').element as HTMLInputElement).value).toBe('someone@example.com')
    expect(list.mock.calls[0]).toEqual(expect.arrayContaining([expect.objectContaining({search:'someone@example.com'})]))
    expect(list).toHaveBeenCalledTimes(1)
  })
  it.each([{}, {search:''}, {search:['x','y']}])('preserves normal empty search for %j',async(query)=>{
    route.query=query
    const w=await view()
    expect((w.get('input').element as HTMLInputElement).value).toBe('')
    expect(list.mock.calls[0]).toEqual(expect.arrayContaining([expect.objectContaining({search:undefined})]))
  })
})
