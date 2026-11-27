import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { opsAPI, type OpsErrorRecord, type OpsErrorLog, type OpsErrorRecordsParams, type OpsErrorRecordsResponse } from '@/api/admin/ops'
import { makeTodayOpsRange } from '@/utils/adminRecentRange'
import { extractApiErrorMessage } from '@/utils/apiError'

export interface OpsErrorRecordGlobals {
  timeRange: string
  customStartTime?: string | null
  customEndTime?: string | null
  platform?: string
  groupId?: number | null
  refreshToken?: number
}
export type RecordFilters = Required<Pick<OpsErrorRecordsParams, 'scope' | 'finance' | 'view' | 'display'>> & {
  user_id?: number; api_key_id?: number; account_id?: number
  model: string; phase: string; category: string; status_codes: string; status_codes_other: string
  error_owner: string; error_source: string; resolved: string; q: string
}
const routeFields = { user_id: 'user_id', api_key_id: 'api_key_id', account_id: 'account_id', model: 'model', phase: 'phase', category: 'category', status_codes: 'status_codes', status_codes_other: 'status_codes_other', error_owner: 'owner', error_source: 'source', resolved: 'resolved', q: 'q' } as const
const ids = ['user_id', 'api_key_id', 'account_id'] as const
const defaultFilters = (): RecordFilters => ({ scope: 'all', finance: 'hide', view: 'all', display: 'grouped', model: '', phase: '', category: '', status_codes: '', status_codes_other: '', error_owner: '', error_source: '', resolved: '', q: '' })
export function readRecordPreference<T>(prefix: string | undefined, key: string, fallback: T, valid: (value: unknown) => value is T): T {
  try { const value = JSON.parse(localStorage.getItem(`${prefix}${key}`) || 'null'); return prefix && valid(value) ? value : fallback } catch { return fallback }
}
export function saveRecordPreference(prefix: string | undefined, key: string, value: unknown) { try { if (prefix) localStorage.setItem(`${prefix}${key}`, JSON.stringify(value)) } catch { /* This session still works without storage. */ } }
export function errorRecordRows(items: OpsErrorRecord[]): OpsErrorLog[] {
  return items.map(item => {
    const row = { ...(item.representative || item) } as OpsErrorLog
    row.id = item.representative_id || row.id
    if (item.representative) {
      row.user_id = item.single_user?.id ?? null; row.user_email = item.single_user?.email || ''
      row.api_key_id = item.single_api_key?.id ?? null; row.api_key_name = item.single_api_key?.name || ''; row.api_key_deleted = !!item.single_api_key?.deleted
      row.client_ip = item.single_client_ip; row.user_agent = item.single_user_agent || ''
      row.inbound_endpoint ||= row.request_path
    }
    return row
  })
}
export function errorRecordMeta(items: OpsErrorRecord[]): Record<number, OpsErrorRecord> { return Object.fromEntries(items.map(item => [item.representative_id || item.id, item])) }

export function useOpsErrorRecords(props: OpsErrorRecordGlobals, prefix: () => string | undefined, translate: (key: string) => string) {
  const route = useRoute(); const router = useRouter()
  const invalidGlobalGroup = computed(() => {
    const value = route.query.group_id
    return value !== undefined && (typeof value !== 'string' || !/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(Number(value)))
  })
  const filters = reactive<RecordFilters>(defaultFilters())
  const page = ref(1)
  const pageSize = ref(readRecordPreference(prefix(), 'page-size', 10, (v): v is number => [10, 20, 50].includes(v as number)))
  const sortBy = ref('created_at'); const sortOrder = ref<'asc' | 'desc'>('desc')
  const items = ref<OpsErrorRecord[]>([]); const total = ref(0); const totalRecords = ref(0)
  const loading = ref(false); const error = ref(''); const updatedAt = ref<string | null>(null)
  const invalidRoute = ref(false); const refreshRevision = ref(0)
  const windowRange = ref<{ start_time: string; end_time: string } | null>(null)
  let mainController: AbortController | undefined; let mainSequence = 0
  const activeParams = ref<OpsErrorRecordsParams | null>(null)
  const group = ref<OpsErrorRecord | null>(null); const showGroup = ref(false)
  const memberItems = ref<OpsErrorRecord[]>([]); const memberTotal = ref(0); const memberPage = ref(1)
  const memberSortBy = ref('created_at'); const memberSortOrder = ref<'asc' | 'desc'>('desc')
  const memberPageSize = ref(10); const memberLoading = ref(false); const memberError = ref('')
  let memberController: AbortController | undefined; let memberSequence = 0; let loadedMemberKey = ''
  let disposed = false

  function applyRoute() {
    const next = defaultFilters(); let invalid = false; let cleanup = false; const cleanQuery = { ...route.query }
    next.finance = readRecordPreference(prefix(), 'finance', 'hide' as RecordFilters['finance'], (v): v is RecordFilters['finance'] => ['hide', 'include', 'only'].includes(v as string))
    next.display = readRecordPreference(prefix(), 'display', 'grouped' as RecordFilters['display'], (v): v is RecordFilters['display'] => ['grouped', 'raw'].includes(v as string))
    const enums = { scope: ['all', 'request', 'upstream'], finance: ['hide', 'include', 'only'], view: ['all', 'errors', 'excluded'], display: ['grouped', 'raw'] }
    for (const key of Object.keys(enums) as Array<keyof typeof enums>) {
      const value = route.query[`errors_${key}`]
      if (typeof value === 'string' && enums[key].includes(value)) Object.assign(next, { [key]: value })
      else if (value != null) { delete cleanQuery[`errors_${key}`]; cleanup = true }
    }
    for (const [field, suffix] of Object.entries(routeFields)) {
      const value = route.query[`errors_${suffix}`]
      if (value == null) continue
      if (typeof value !== 'string') { invalid = true; continue }
      if (ids.includes(field as typeof ids[number])) {
        if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(Number(value))) invalid = true
        else Object.assign(next, { [field]: Number(value) })
      } else Object.assign(next, { [field]: value })
    }
    Object.assign(filters, { ...defaultFilters(), ...next })
    // Object.assign does not remove optional IDs when a URL condition disappears.
    for (const id of ids) filters[id] = next[id]
    invalidRoute.value = invalid
    if (cleanup) void router.replace({ query: cleanQuery }).catch(() => {})
  }
  watch(() => route.query, applyRoute, { immediate: true })

  function syncRoute() {
    if (disposed) return
    const query = { ...route.query }
    for (const name of ['scope', 'finance', 'view', 'display'] as const) query[`errors_${name}`] = filters[name]
    for (const [field, suffix] of Object.entries(routeFields)) {
      const value = filters[field as keyof RecordFilters]
      if (value !== '' && value != null) query[`errors_${suffix}`] = String(value)
      else delete query[`errors_${suffix}`]
    }
    saveRecordPreference(prefix(), 'finance', filters.finance); saveRecordPreference(prefix(), 'display', filters.display)
    void router.replace({ query }).catch(() => { /* Navigating away cancels state persistence. */ })
  }
  function changeFilters() { invalidRoute.value = false; syncRoute() }
  function reset() { Object.assign(filters, defaultFilters(), { user_id: undefined, api_key_id: undefined, account_id: undefined }); sortBy.value = 'created_at'; sortOrder.value = 'desc'; page.value = 1; invalidRoute.value = false; syncRoute() }
  function refresh() { refreshRevision.value++ }
  function sort(key: string, order: 'asc' | 'desc') { sortBy.value = key; sortOrder.value = order }
  function closeGroup() { showGroup.value = false; group.value = null; memberItems.value = []; loadedMemberKey = ''; memberSequence++; memberController?.abort() }
  function valid(): boolean {
    if (invalidRoute.value || invalidGlobalGroup.value) return false
    if (filters.phase && !['request', 'auth', 'account_auth', 'routing', 'upstream', 'network', 'internal'].includes(filters.phase)) return false
    if (filters.category && !['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber'].includes(filters.category)) return false
    if (filters.error_owner && !['provider', 'client', 'platform'].includes(filters.error_owner)) return false
    if (filters.resolved && !['true', 'false'].includes(filters.resolved)) return false
    if (filters.status_codes && (!filters.status_codes.split(',').every(v => /^\d{1,3}$/.test(v) && Number(v) < 600) || filters.status_codes.split(',').length > 20 || filters.status_codes_other)) return false
    if (filters.status_codes_other && !['true', 'false'].includes(filters.status_codes_other)) return false
    if ([...filters.q].length > 512 || filters.error_source.length > 64) return false
    if (props.timeRange === 'custom') return !!props.customStartTime && !!props.customEndTime && Date.parse(props.customStartTime) < Date.parse(props.customEndTime)
    return ['5m', '30m', '1h', '3h', '6h', '24h', 'today'].includes(props.timeRange)
  }
  function params(): OpsErrorRecordsParams {
    const extra: Record<string, string | number> = {}
    for (const field of Object.keys(routeFields) as Array<keyof typeof routeFields>) if (filters[field] !== '' && filters[field] != null) extra[field] = filters[field]!
    const time = windowRange.value || (props.timeRange === 'custom' ? { start_time: props.customStartTime!, end_time: props.customEndTime! } : props.timeRange === 'today' ? makeTodayOpsRange() : { time_range: props.timeRange as OpsErrorRecordsParams['time_range'] })
    return { ...time, ...extra, platform: props.platform || undefined, group_id: props.groupId ?? undefined, scope: filters.scope, finance: filters.finance, view: filters.view, display: filters.display, page: page.value, page_size: pageSize.value, sort_by: sortBy.value, sort_order: sortOrder.value }
  }
  async function load() {
    const sequence = ++mainSequence; mainController?.abort(); mainController = new AbortController()
    error.value = ''
    if (!valid()) { loading.value = false; error.value = translate(invalidGlobalGroup.value ? 'admin.ops.records.invalidGroup' : 'admin.ops.records.invalid'); return }
    const query = params(); if (query.start_time && query.end_time && Date.parse(query.start_time) >= Date.parse(query.end_time)) { items.value = []; total.value = 0; totalRecords.value = 0; loading.value = false; return }
    loading.value = true
    try {
      const data = await opsAPI.listErrorRecords(query, { signal: mainController.signal })
      if (sequence !== mainSequence || disposed) return
      items.value = data.items; total.value = data.total; totalRecords.value = data.total_records
      windowRange.value = { start_time: data.window_start, end_time: data.window_end }
      activeParams.value = { ...query, ...windowRange.value, time_range: undefined }
      updatedAt.value = new Date().toISOString()
    } catch (err) {
      if (sequence === mainSequence && !mainController.signal.aborted && !disposed) error.value = extractApiErrorMessage(err, translate('admin.ops.records.loadFailed'))
    } finally { if (sequence === mainSequence && !disposed) loading.value = false }
  }
  const criteria = computed(() => JSON.stringify([props.timeRange, props.customStartTime, props.customEndTime, props.platform, props.groupId, filters.scope, filters.finance, filters.view, filters.user_id, filters.api_key_id, filters.account_id, filters.model, filters.phase, filters.category, filters.status_codes, filters.status_codes_other, filters.error_owner, filters.error_source, filters.resolved, filters.q, invalidRoute.value, invalidGlobalGroup.value]))
  watch(() => JSON.stringify([criteria.value, page.value, pageSize.value, sortBy.value, sortOrder.value, filters.display, props.refreshToken, refreshRevision.value]), (value, old) => {
    const next = JSON.parse(value); const previous = old ? JSON.parse(old) : null
    const changed = !previous || next[0] !== previous[0]
    const refreshChanged = !!previous && (next[6] !== previous[6] || next[7] !== previous[7])
    const pageChanged = !!previous && next[1] !== previous[1]
    const presentationChanged = !!previous && [2, 3, 4, 5].some(i => next[i] !== previous[i])
    if (changed || refreshChanged) { windowRange.value = null; closeGroup() }
    if (changed || presentationChanged || pageChanged) { items.value = []; total.value = 0; totalRecords.value = 0; updatedAt.value = null }
    if ((changed || presentationChanged) && page.value !== 1) { page.value = 1; return }
    saveRecordPreference(prefix(), 'page-size', pageSize.value)
    void load()
  }, { immediate: true })

  function openGroup(item: OpsErrorRecord) { group.value = item; memberPage.value = 1; memberSortBy.value = 'created_at'; memberSortOrder.value = 'desc'; memberItems.value = []; loadedMemberKey = ''; showGroup.value = true }
  async function loadMembers(force = false) {
    if (!showGroup.value || !group.value || !activeParams.value) return
    const id = group.value.representative_id
    const { display: _display, ...query } = activeParams.value
    const memberParams = { ...query, page: memberPage.value, page_size: memberPageSize.value, sort_by: memberSortBy.value, sort_order: memberSortOrder.value }
    const key = JSON.stringify([id, memberParams])
    if (!force && key === loadedMemberKey) return
    memberController?.abort(); memberController = new AbortController(); const sequence = ++memberSequence
    memberLoading.value = true; memberError.value = ''; memberItems.value = []
    try {
      const data: OpsErrorRecordsResponse = await opsAPI.listErrorRecordMembers(id, memberParams, { signal: memberController.signal })
      if (sequence !== memberSequence || disposed) return
      memberItems.value = data.items; memberTotal.value = data.total; loadedMemberKey = key
    } catch (err) {
      if (sequence === memberSequence && !memberController.signal.aborted && !disposed) memberError.value = extractApiErrorMessage(err, translate('admin.ops.records.loadFailed'))
    } finally { if (sequence === memberSequence && !disposed) memberLoading.value = false }
  }
  function sortMembers(key: string, order: 'asc' | 'desc') { memberSortBy.value = key; memberSortOrder.value = order }
  watch(() => JSON.stringify([showGroup.value, group.value?.representative_id, memberPage.value, memberPageSize.value, memberSortBy.value, memberSortOrder.value]), (value, old) => {
    const next = JSON.parse(value); const previous = old ? JSON.parse(old) : null
    if (previous && [3, 4, 5].some(i => next[i] !== previous[i]) && memberPage.value !== 1) { memberPage.value = 1; return }
    if (showGroup.value) void loadMembers()
    else { memberSequence++; memberController?.abort(); memberLoading.value = false }
  })
  onUnmounted(() => { disposed = true; mainSequence++; memberSequence++; mainController?.abort(); memberController?.abort() })
  return { filters, page, pageSize, sortBy, sortOrder, items, total, totalRecords, loading, error, updatedAt, windowRange, changeFilters, reset, refresh, sort, group, showGroup, memberItems, memberTotal, memberPage, memberPageSize, memberSortBy, memberSortOrder, memberLoading, memberError, closeGroup, openGroup, loadMembers, sortMembers }
}
