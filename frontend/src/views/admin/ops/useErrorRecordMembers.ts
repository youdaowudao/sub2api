import { onUnmounted, ref, watch, type Ref } from 'vue'
import { opsAPI, type OpsErrorRecord, type OpsErrorRecordsParams, type OpsErrorRecordsResponse } from '@/api/admin/ops'
import { extractApiErrorMessage } from '@/utils/apiError'

export function useErrorRecordMembers(activeParams: Ref<OpsErrorRecordsParams | null>, translate: (key: string) => string) {
  const group = ref<OpsErrorRecord | null>(null); const showGroup = ref(false)
  const memberItems = ref<OpsErrorRecord[]>([]); const memberTotal = ref(0); const memberPage = ref(1)
  const memberSortBy = ref('created_at'); const memberSortOrder = ref<'asc' | 'desc'>('desc')
  const memberPageSize = ref(10); const memberLoading = ref(false); const memberError = ref('')
  let memberController: AbortController | undefined; let memberSequence = 0; let loadedMemberKey = ''
  let disposed = false
  function closeGroup() { showGroup.value = false; group.value = null; memberItems.value = []; loadedMemberKey = ''; memberSequence++; memberController?.abort() }
  function openGroup(item: OpsErrorRecord) { group.value = item; memberPage.value = 1; memberSortBy.value = 'created_at'; memberSortOrder.value = 'desc'; memberItems.value = []; loadedMemberKey = ''; showGroup.value = true }
  async function loadMembers(force = false) {
    if (!showGroup.value || !group.value || !activeParams.value) return
    const id = group.value.representative_id
    const { display: _display, ...query } = activeParams.value
    const memberParams = { ...query, page: memberPage.value, page_size: memberPageSize.value, sort_by: memberSortBy.value, sort_order: memberSortOrder.value }
    const key = JSON.stringify([id, memberParams])
    if (!force && key === loadedMemberKey) return
    memberController?.abort(); memberController = new AbortController(); const sequence = ++memberSequence
    memberLoading.value = true; memberError.value = ''; memberItems.value = []; loadedMemberKey = ''
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
  onUnmounted(() => { disposed = true; memberSequence++; memberController?.abort() })
  return { group, showGroup, memberItems, memberTotal, memberPage, memberPageSize, memberSortBy, memberSortOrder, memberLoading, memberError, closeGroup, openGroup, loadMembers, sortMembers }
}
