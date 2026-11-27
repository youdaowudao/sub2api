import { buildAdminGroupSelectOptions } from '@/utils/groupDisplay'
import type { AdminGroup } from '@/types'

export interface ApiKeyGroupFilterOption {
  value: number | string | null
  label: string
  kind?: 'group'
  disabled?: boolean
}

export interface ApiKeyGroupFilterLabels {
  all: string
  exclusive: string
  public: string
  subscription: string
  disabled: string
  categoryLabel?: (key: string) => string
}

export function buildApiKeyGroupFilterOptions(
  groups: AdminGroup[],
  labels: ApiKeyGroupFilterLabels
): ApiKeyGroupFilterOption[] {
  return [
    { value: null, label: labels.all },
    ...buildAdminGroupSelectOptions(groups, group => ({
      value: group.id,
      label: `${group.name} · ${group.status !== 'active' ? labels.disabled : group.subscription_type === 'subscription' ? labels.subscription : group.is_exclusive ? labels.exclusive : labels.public}`
    }), labels.categoryLabel ?? (key => key.split('.').pop() || key))
  ]
}
