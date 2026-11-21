import type { Group } from '@/types'
import {
  GROUP_DISPLAY_CATEGORIES,
  GROUP_DISPLAY_CATEGORY_ICONS,
  groupCategory,
} from './groupDisplay'

export const KEY_GROUP_PROVIDERS = GROUP_DISPLAY_CATEGORIES
export type KeyGroupProvider = (typeof KEY_GROUP_PROVIDERS)[number]

export function getKeyGroupProvider(group: Pick<Group, 'platform' | 'display_category'>): KeyGroupProvider {
  return groupCategory(group)
}

export const KEY_GROUP_PROVIDER_ICONS = GROUP_DISPLAY_CATEGORY_ICONS
