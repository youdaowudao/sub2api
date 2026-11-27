export const errorRecordColumnOrder = ['message', 'account', 'model', 'user', 'created_at', 'actions', 'api_key', 'platform', 'endpoint', 'group', 'type', 'category', 'status', 'user_agent', 'client_ip']
export const errorRecordDefaultVisible = ['message', 'account', 'model', 'user', 'created_at', 'actions']
export const errorRecordAlwaysVisible = ['message', 'created_at', 'actions']

import type { OpsErrorRecord } from '@/api/admin/ops'
export function uniqueErrorUser(record?: OpsErrorRecord): boolean {
  return (!record || (!record.representative && record.count <= 1)) || (record.user_count === 1 && record.has_unknown_user === false && (record.single_user?.id ?? 0) > 0)
}
export function uniqueErrorKey(record?: OpsErrorRecord): boolean {
  return (!record || (!record.representative && record.count <= 1)) || (uniqueErrorUser(record) && record.api_key_count === 1 && record.has_unknown_api_key === false && (record.single_api_key?.id ?? 0) > 0)
}
