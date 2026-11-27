import { sortGroupsForDisplay, type DisplayGroup, type GroupDisplayHeader } from './groupDisplayOrder'
import type { GroupPlatform } from '@/types'

export const GROUP_DISPLAY_ICON_PLATFORMS = [
  'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
  'minimax', 'deepseek', 'opencode_go', 'command_code', 'cline',
] as const

export const GROUP_DISPLAY_CATEGORIES = ['openai', 'anthropic', 'gemini', 'domestic', 'image', 'other'] as const

type Category = (typeof GROUP_DISPLAY_CATEGORIES)[number]
type GroupWithCategory = { platform: string; display_category?: string }

export const GROUP_DISPLAY_CATEGORY_ICONS = {
  openai: ['openai'],
  anthropic: ['anthropic'],
  gemini: ['gemini'],
  domestic: ['deepseek', 'kimi'],
  image: [],
  other: ['grok'],
} as const satisfies Record<Category, readonly GroupPlatform[]>

export function groupIconPlatform<Platform extends string>(group: { platform: Platform; display_icon?: string }) {
  const icon = group.display_icon?.trim()
  return GROUP_DISPLAY_ICON_PLATFORMS.find(platform => platform === icon) ?? group.platform
}

/** 模型广场按前台分类挪动分组：分类设成 OpenAI、Anthropic、Gemini 时按该平台显示，其余按真实平台。 */
export function groupPlazaPlatform(group: GroupWithCategory): string {
  const category = group.display_category?.trim()
  return category === 'openai' || category === 'anthropic' || category === 'gemini' ? category : group.platform
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

export function groupGroupsByCategory<T extends DisplayGroup & GroupWithCategory>(groups: readonly T[], includeEmptyImage = false) {
  const byCategory = new Map<Category, T[]>()
  for (const group of sortGroupsForDisplay(groups)) {
    const category = groupCategory(group)
    const members = byCategory.get(category)
    if (members) members.push(group)
    else byCategory.set(category, [group])
  }

  return GROUP_DISPLAY_CATEGORIES.flatMap(category => {
    const members = byCategory.get(category)
    return members || (includeEmptyImage && category === 'image' && groups.length > 0)
      ? [{ category, groups: members ?? [] }]
      : []
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
