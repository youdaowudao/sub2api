<template>
  <AppLayout>
    <div class="space-y-6" data-testid="admin-checkin">
      <div class="flex flex-wrap gap-2 border-b border-gray-200 dark:border-dark-700" role="tablist">
        <button v-for="tab in tabs" :key="tab" type="button" role="tab" :aria-selected="activeTab===tab" :data-testid="'checkin-tab-'+tab" class="px-4 py-3 text-sm font-medium" :class="activeTab===tab?'border-b-2 border-primary-500 text-primary-600 dark:text-primary-400':'text-gray-500 dark:text-gray-400'" @click="selectTab(tab)">{{ t('admin.checkin.'+tab) }}</button>
      </div>
      <div v-if="errors[activeTab]" class="card p-4 text-sm" role="alert">{{t('admin.checkin.loadFailed')}} <span v-if="errors[activeTab]!==t('admin.checkin.loadFailed')">{{errors[activeTab]}}</span> <button class="btn btn-secondary ml-2" @click="load(activeTab)">{{t('admin.checkin.retry')}}</button></div>
      <template v-if="activeTab==='overview'">
        <div v-if="pending.overview" class="card flex justify-center p-10"><LoadingSpinner /></div>
        <template v-else-if="overview && !errors.overview">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-5">
            <section v-for="period in periods" :key="period" class="card min-w-0 space-y-3 p-4">
              <h3 class="font-semibold">{{t('admin.checkin.'+period)}}</h3>
              <p class="text-xl font-bold text-primary-600 dark:text-primary-400">{{money(overview[period].amount)}}</p>
              <p class="text-sm text-gray-500">{{t('admin.checkin.usersCount')}} · {{overview[period].users}}</p>
              <div v-for="tier in tiers" :key="tier" class="text-xs">
                <div class="flex flex-wrap justify-between gap-1"><span>{{tierText(tier)}}</span><span>{{money(overview[period].tiers?.[tier]?.amount||0)}} · {{overview[period].tiers?.[tier]?.users||0}} · {{percent(overview[period],tier)}}%</span></div>
                <div class="mt-1 h-1.5 rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500" :style="{width:percent(overview[period],tier)+'%'}"/></div>
              </div>
            </section>
          </div>
          <div class="card flex items-center justify-between p-4"><span>{{t('admin.checkin.email_verifications')}}</span><strong>{{overview.email_verifications}}</strong></div>
          <div class="card space-y-4 p-4">
            <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="font-semibold">{{t('admin.checkin.trend')}}</h3><div class="flex gap-2"><button v-for="count in [7,30,90]" :key="count" :class="['btn',days===count?'btn-primary':'btn-secondary']" @click="setDays(count)">{{t('admin.checkin.days',{count})}}</button></div></div>
            <div v-if="overview.trend.length" class="h-64"><Line :data="trendData" :options="trendOptions" /></div>
            <p v-else class="p-6 text-center text-sm text-gray-500">{{t('admin.checkin.empty')}}</p>
          </div>
        </template>
      </template>
      <template v-else-if="activeTab==='users'">
        <div class="card space-y-4 p-4">
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="filter('users')">
            <label class="min-w-0 flex-1 text-sm">{{t('admin.checkin.search')}}<input v-model="usersFilter.search" class="input mt-1" :placeholder="t('admin.checkin.search')"/></label>
            <label class="text-sm">{{t('admin.checkin.tier')}}<select v-model="usersFilter.tier" class="input mt-1"><option value="">{{t('admin.checkin.all')}}</option><option v-for="tier in tiers" :key="tier" :value="tier">{{tierText(tier)}}</option></select></label>
            <button class="btn btn-secondary" type="submit">{{t('admin.checkin.filter')}}</button>
          </form>
          <DataTable v-if="!errors.users" :columns="usersColumns" :data="userRows" :loading="pending.users" row-key="user_id" server-side-sort :default-sort-key="usersFilter.sort" :default-sort-order="usersFilter.order" @sort="sortUsers">
            <template #cell-tier="{value}">{{tierText(value)}}</template>
            <template #cell-total_amount="{value}">{{money(value)}}</template>
            <template #cell-r="{value}">{{money(value)}}</template>
            <template #cell-shared_ip_users="{value}"><span :class="value>=2?'badge bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200':''">{{value}}</span></template>
            <template #cell-pending_email="{value}">{{t(value?'admin.checkin.yes':'admin.checkin.no')}}</template>
            <template #cell-actions="{row}"><div class="flex gap-2 whitespace-nowrap"><button class="text-primary-600" @click="viewRecords('#'+row.user_id,'')">{{t('admin.checkin.viewRecords')}}</button><router-link class="text-primary-600" :to="{path:'/admin/users',query:{search:row.email}}">{{t('admin.checkin.manageUser')}}</router-link></div></template>
          </DataTable>
          <Pagination v-if="!errors.users && totals.users>0" :page="usersFilter.page" :page-size="usersFilter.page_size" :total="totals.users" :persist-page-size="false" @update:page="page('users',$event)" @update:page-size="pageSize('users',$event)" />
        </div>
      </template>
      <template v-else-if="activeTab==='records'">
        <div class="card space-y-4 p-4">
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="filter('records')">
            <label class="min-w-0 flex-1 text-sm">{{t('admin.checkin.search')}}<input v-model="recordsFilter.search" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.ip')}}<input v-model="recordsFilter.ip" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.date_from')}}<input v-model="recordsFilter.date_from" type="date" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.date_to')}}<input v-model="recordsFilter.date_to" type="date" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.tier')}}<select v-model="recordsFilter.tier" class="input mt-1"><option value="">{{t('admin.checkin.all')}}</option><option v-for="tier in tiers" :key="tier" :value="tier">{{tierText(tier)}}</option></select></label>
            <label class="text-sm">{{t('admin.checkin.verification')}}<select v-model="recordsFilter.verification" class="input mt-1"><option value="">{{t('admin.checkin.all')}}</option><option v-for="v in ['none','captcha','email']" :key="v" :value="v">{{t('admin.checkin.verifications.'+v)}}</option></select></label>
            <button class="btn btn-secondary" type="submit">{{t('admin.checkin.filter')}}</button>
          </form>
          <DataTable v-if="!errors.records" :columns="recordsColumns" :data="recordRows" :loading="pending.records" row-key="id">
            <template #cell-tier="{value}">{{tierText(value)}}</template>
            <template #cell-amount="{value}">{{money(value)}}</template>
            <template #cell-created_at="{value}">{{formatDateTime(value)}}</template>
            <template #cell-verification="{value}">{{t('admin.checkin.verifications.'+value)}}</template>
          </DataTable>
          <Pagination v-if="!errors.records && totals.records>0" :page="recordsFilter.page" :page-size="recordsFilter.page_size" :total="totals.records" :persist-page-size="false" @update:page="page('records',$event)" @update:page-size="pageSize('records',$event)" />
        </div>
      </template>
      <template v-else-if="activeTab==='ips'">
        <div class="card space-y-4 p-4">
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="filter('ips')">
            <label class="text-sm">{{t('admin.checkin.date_from')}}<input v-model="ipsFilter.date_from" type="date" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.date_to')}}<input v-model="ipsFilter.date_to" type="date" class="input mt-1"/></label>
            <label class="text-sm">{{t('admin.checkin.min_users')}}<input v-model.number="ipsFilter.min_users" type="number" min="1" step="1" class="input mt-1"/></label>
            <button class="btn btn-secondary" type="submit">{{t('admin.checkin.filter')}}</button>
          </form>
          <DataTable v-if="!errors.ips" :columns="ipsColumns" :data="ipRows" :loading="pending.ips" row-key="ip">
            <template #cell-ip="{value}"><button class="text-primary-600" @click="viewRecords('',value)">{{value}}</button></template>
            <template #cell-total_amount="{value}">{{money(value)}}</template>
            <template #cell-emails="{value}">{{(value||[]).join(', ')}}</template>
          </DataTable>
          <Pagination v-if="!errors.ips && totals.ips>0" :page="ipsFilter.page" :page-size="ipsFilter.page_size" :total="totals.ips" :persist-page-size="false" @update:page="page('ips',$event)" @update:page-size="pageSize('ips',$event)" />
        </div>
      </template>
      <template v-else>
        <div v-if="pending.settings" class="card flex justify-center p-10"><LoadingSpinner /></div>
        <template v-else-if="config && !errors.settings">
          <p v-if="!configValid" data-testid="checkin-config-invalid" role="alert" class="rounded-xl bg-amber-50 p-4 text-amber-800 dark:bg-amber-900/30 dark:text-amber-200">{{t('admin.checkin.invalidConfig')}}</p>
          <p v-if="config.newcomer_captcha && !captchaAvailable" role="alert" class="rounded-xl bg-amber-50 p-4 text-amber-800 dark:bg-amber-900/30 dark:text-amber-200">{{t('admin.checkin.captchaUnavailable')}}</p>
          <div class="card p-4 text-sm">{{t('admin.checkin.captchaProvider')}}：{{captchaProvider||t('admin.checkin.none')}}</div>
          <div v-for="group in configGroups" :key="group.key" class="card space-y-4 p-5">
            <h3 class="font-semibold">{{t('admin.checkin.groups.'+group.key)}}</h3>
            <div v-for="field in group.fields" :key="field.key" class="grid grid-cols-1 items-center gap-2 border-t border-gray-100 pt-4 first:border-t-0 dark:border-dark-700 sm:grid-cols-[1fr_200px]" :class="fieldDisabled(field.key)?'opacity-50':''">
              <label :for="'checkin-field-'+field.key"><span class="text-sm font-medium">{{t('admin.checkin.fields.'+field.key+'.label')}}</span><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{t('admin.checkin.fields.'+field.key+'.help')}}</p></label>
              <input v-if="field.kind==='bool'" :id="'checkin-field-'+field.key" :data-testid="'checkin-field-'+field.key" type="checkbox" class="h-5 w-5 accent-primary-600" :checked="Boolean(config[field.key])" :disabled="saving||fieldDisabled(field.key)" @change="setField(field.key,($event.target as HTMLInputElement).checked)"/>
              <input v-else :id="'checkin-field-'+field.key" :data-testid="'checkin-field-'+field.key" type="number" class="input" :step="field.kind==='money'?'0.01':'1'" :min="field.min||0" :max="field.max" :value="config[field.key]" :disabled="saving||fieldDisabled(field.key)" @input="setField(field.key,($event.target as HTMLInputElement).valueAsNumber)"/>
            </div>
          </div>
          <p v-if="saveError" data-testid="checkin-save-error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{saveError}}</p>
          <p v-if="saved" role="status" class="text-sm text-primary-600">{{t('admin.checkin.saved')}}</p>
          <button data-testid="checkin-save" type="button" class="btn btn-primary" :disabled="saving" @click="save">{{t('admin.checkin.save')}}</button>
        </template>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Line } from 'vue-chartjs'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { Column } from '@/components/common/types'
import { formatDateTime } from '@/utils/format'
import { adminCheckinAPI, type CheckinTier, type CheckinConfig, type CheckinOverview, type CheckinPeriod, type CheckinAdminUser, type CheckinAdminRecord, type CheckinAdminIP, type CheckinUserParams, type CheckinRecordParams, type CheckinIPParams } from '@/api/admin/checkin'

ChartJS.register(CategoryScale,LinearScale,PointElement,LineElement,Tooltip,Legend)
const { t } = useI18n()
const tabs = ['overview','users','records','ips','settings'] as const
type Tab = typeof tabs[number]
type ListTab = 'users'|'records'|'ips'
const activeTab = ref<Tab>('overview')
const pending = reactive<Record<Tab, boolean>>({overview:false,users:false,records:false,ips:false,settings:false})
const errors = reactive<Record<Tab,string>>({overview:'',users:'',records:'',ips:'',settings:''}), loaded = reactive({...pending})
const seq: Record<Tab, number> = {overview:0,users:0,records:0,ips:0,settings:0}
let active = true
const overview = ref<CheckinOverview|null>(null), days=ref(30)
const userRows = ref<CheckinAdminUser[]>([]), recordRows=ref<CheckinAdminRecord[]>([]), ipRows=ref<CheckinAdminIP[]>([])
const usersFilter=reactive<CheckinUserParams>({page:1,page_size:20,search:'',tier:'',sort:'total_amount',order:'desc'})
const recordsFilter=reactive<CheckinRecordParams>({page:1,page_size:20,date_from:'',date_to:'',search:'',ip:'',tier:'',verification:''})
const ipsFilter=reactive<CheckinIPParams>({page:1,page_size:20,date_from:'',date_to:'',min_users:2})
const filters={users:usersFilter,records:recordsFilter,ips:ipsFilter}
const totals=reactive({users:0,records:0,ips:0})
const config=ref<CheckinConfig|null>(null), configValid=ref(true), captchaAvailable=ref(false), captchaProvider=ref('')
const saving=ref(false), saveError=ref(''), saved=ref(false)
const periods=['today','yesterday','last_7_days','last_30_days','all_time'] as const
const tiers:CheckinTier[]=['veteran','new_funded','new_unfunded']
const tierText=(tier:CheckinTier)=>t('admin.checkin.tiers.'+tier)
const money=(value:number)=>'¥'+Number(value||0).toFixed(2)
function percent(period:CheckinPeriod,tier:CheckinTier){return period.users?Math.round((period.tiers?.[tier]?.users||0)*100/period.users):0}
const trendData=computed(()=>({labels:overview.value?.trend.map(p=>p.date)||[],datasets:[
  {label:t('admin.checkin.usersCount'),data:overview.value?.trend.map(p=>p.users)||[],borderColor:'#6366f1',yAxisID:'users',tension:.2},
  {label:t('admin.checkin.amount'),data:overview.value?.trend.map(p=>p.amount)||[],borderColor:'#f59e0b',yAxisID:'amount',tension:.2},
]}))
const trendOptions:ChartOptions<'line'>={responsive:true,maintainAspectRatio:false,scales:{users:{type:'linear',position:'left',beginAtZero:true,ticks:{precision:0}},amount:{type:'linear',position:'right',beginAtZero:true,grid:{drawOnChartArea:false}}}}
const col=(key:string,sortable=false):Column=>({key,label:t('admin.checkin.'+key),sortable})
const usersColumns=computed(()=>['user_id','email','username','status','r','tier','total_days','total_amount','first_checkin','last_checkin','last_ip','shared_ip_users','email_verifications','pending_email','actions'].map(k=>col(k,['total_days','total_amount','last_checkin','shared_ip_users'].includes(k))))
const recordsColumns=computed(()=>['created_at','date','user_id','email','amount','tier','ip','verification'].map(k=>col(k)))
const ipsColumns=computed(()=>['ip','usersCount','total_days','total_amount','first_date','last_date','emails'].map(k=>k==='usersCount'?{key:'users',label:t('admin.checkin.usersCount')}:col(k)))

async function load(tab:Tab){
  const id=++seq[tab]
  const current=()=>active&&seq[tab]===id
  pending[tab]=true; errors[tab]=''
  try{
    if(tab==='overview'){const data=await adminCheckinAPI.overview(days.value);if(current())overview.value=data}
    else if(tab==='users'){const data=await adminCheckinAPI.users({...usersFilter});if(current()){userRows.value=data.items||[];totals.users=data.total}}
    else if(tab==='records'){const data=await adminCheckinAPI.records({...recordsFilter});if(current()){recordRows.value=data.items||[];totals.records=data.total}}
    else if(tab==='ips'){const data=await adminCheckinAPI.ips({...ipsFilter});if(current()){ipRows.value=data.items||[];totals.ips=data.total}}
    else {const data=await adminCheckinAPI.config();if(current()){config.value={...data.config};configValid.value=data.config_valid;captchaProvider.value=data.captcha_provider;captchaAvailable.value=data.captcha_available}}
    if(current())loaded[tab]=true
  } catch(error) {if(current())errors[tab]=(error as {message?:string})?.message || t('admin.checkin.loadFailed')}
  finally {if(current())pending[tab]=false}
}
function selectTab(tab:Tab){activeTab.value=tab;if(!loaded[tab]&&!pending[tab])void load(tab)}
function setDays(value:number){if(days.value!==value){days.value=value;void load('overview')}}
function filter(tab:ListTab){filters[tab].page=1;void load(tab)}
function page(tab:ListTab,value:number){filters[tab].page=value;void load(tab)}
function pageSize(tab:ListTab,value:number){filters[tab].page_size=value;filter(tab)}
function sortUsers(key:string,order:'asc'|'desc'){usersFilter.sort=key;usersFilter.order=order;filter('users')}
function viewRecords(search:string,ip:string){
  recordsFilter.search=search;recordsFilter.ip=ip;recordsFilter.page=1
  // A row action must not silently combine unrelated filters from a previous search.
  recordsFilter.date_from='';recordsFilter.date_to='';recordsFilter.tier='';recordsFilter.verification=''
  activeTab.value='records';void load('records')
}

type ConfigKey=keyof CheckinConfig
interface Field{key:ConfigKey;kind:'bool'|'money'|'int';min?:number;max?:number}
const bool=(key:ConfigKey):Field=>({key,kind:'bool'})
const amount=(key:ConfigKey,max=100):Field=>({key,kind:'money',max})
const int=(key:ConfigKey,max:number,min=0):Field=>({key,kind:'int',min,max})
const configGroups=[
  {key:'basic',fields:[bool('enabled')]},
  {key:'veteran',fields:[amount('veteran_threshold',1000000),amount('veteran_min'),amount('veteran_max')]},
  {key:'reward',fields:[amount('newcomer_min'),amount('newcomer_max'),amount('newcomer_cap'),amount('newcomer_after_cap')]},
  {key:'protection',fields:[bool('newcomer_captcha'),int('min_account_age_days',3650),int('ip_daily_limit',1000)]},
  {key:'email',fields:[bool('email_verify_enabled'),int('email_verify_first',3650,1),int('email_verify_every',3650),bool('email_verify_skip_reserved')]},
]
const emailChildren:ConfigKey[]=['email_verify_first','email_verify_every','email_verify_skip_reserved']
function fieldDisabled(key:ConfigKey){return key!=='enabled'&&(!config.value?.enabled||emailChildren.includes(key)&&!config.value.email_verify_enabled)}
function setField(key:ConfigKey,value:boolean|number){if(config.value&&!fieldDisabled(key)){config.value={...config.value,[key]:value};saved.value=false}}
async function save(){
  if(!config.value||saving.value)return
  saving.value=true;saveError.value='';saved.value=false
  try{
    const data=await adminCheckinAPI.save({...config.value})
    if(active){
      config.value={...data};configValid.value=true;saved.value=true
      // The users endpoint calculates current tier and next verification from the config.
      // Invalidate any older request/cache; keep its filter and pagination.
      ++seq.users;loaded.users=false;pending.users=false;errors.users=''
      userRows.value=[];totals.users=0
      if(activeTab.value==='users')void load('users')
    }
  }catch(error){
    if(active){const e=error as {metadata?:{field?:string};message?:string};saveError.value=t('admin.checkin.saveFailed')+(e.metadata?.field?' · '+e.metadata.field:'')+(e.message?' · '+e.message:'')}
  }finally{saving.value=false}
}
onMounted(()=>{void load('overview')})
onBeforeUnmount(()=>{active=false})
</script>
