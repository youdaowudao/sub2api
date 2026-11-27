export interface UsageQuickFilter {
  field: 'account_id' | 'model' | 'group_id' | 'api_key_id'
  value: number | string
  label?: string
  userId?: number
  userLabel?: string
}
