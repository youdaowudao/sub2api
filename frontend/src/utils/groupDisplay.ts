import { sortGroupsForDisplay, type DisplayGroup, type GroupDisplayHeader } from './groupDisplayOrder'
import type { GroupPlatform } from '@/types'

export const GROUP_DISPLAY_ICON_PLATFORMS = [
  'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
  'minimax', 'deepseek', 'opencode_go', 'command_code', 'cline',
] as const

export const GROUP_DISPLAY_CATEGORIES = ['openai', 'anthropic', 'gemini', 'domestic', 'other'] as const

type Category = (typeof GROUP_DISPLAY_CATEGORIES)[number]
type GroupWithCategory = { platform: string; display_category?: string }

export const GROUP_DISPLAY_CATEGORY_ICONS = {
  openai: ['openai'],
  anthropic: ['anthropic'],
  gemini: ['gemini'],
  domestic: ['deepseek', 'kimi'],
  other: ['grok'],
} as const satisfies Record<Category, readonly GroupPlatform[]>

export function groupIconPlatform<Platform extends string>(group: { platform: Platform; display_icon?: string }) {
  const icon = group.display_icon?.trim()
  return GROUP_DISPLAY_ICON_PLATFORMS.find(platform => platform === icon) ?? group.platform
}

export function groupCategory(group: GroupWithCategory): Category {
  const displayCategory = group.display_category?.trim()
  const manual = GROUP_DISPLAY_CATEGORIES.find(category => category !== 'other' && category === displayCategory)
  if (manual) return manual

  switch (group.platform) {
    case 'openai':
    case 'anthropic':
    case 'gemini':
      return group.platform
    case 'kimi':
    case 'zhipu':
    case 'deepseek':
    case 'minimax':
      return 'domestic'
    default:
      return 'other'
  }
}

export function groupGroupsByCategory<T extends DisplayGroup & GroupWithCategory>(groups: readonly T[]) {
  const byCategory = new Map<Category, T[]>()
  for (const group of sortGroupsForDisplay(groups)) {
    const category = groupCategory(group)
    const members = byCategory.get(category)
    if (members) members.push(group)
    else byCategory.set(category, [group])
  }

  return GROUP_DISPLAY_CATEGORIES.flatMap(category => {
    const members = byCategory.get(category)
    return members ? [{ category, groups: members }] : []
  })
}

export function buildGroupSelectOptionsByCategory<T extends DisplayGroup & GroupWithCategory, Option>(
  groups: readonly T[],
  toOption: (group: T) => Option,
  labelOf: (category: Category) => string,
): (Option | GroupDisplayHeader)[] {
  return groupGroupsByCategory(groups).flatMap(({ category, groups }) => [
    { value: `__group_category:${category}`, label: labelOf(category), kind: 'group' as const, disabled: true as const },
    ...groups.map(toOption),
  ])
}
