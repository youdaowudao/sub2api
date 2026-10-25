import { listPlatforms, platformDisplayName } from '@/constants/platformCatalog'

export interface DisplayGroup {
  id: number
  platform?: string | null
  sort_order?: number
}

export interface GroupDisplayHeader {
  [key: string]: unknown
  value: string
  label: string
  kind: 'group'
  disabled: true
}

/** Platform headings use catalog order; unregistered identifiers remain visible. */
export function comparePlatformsForDisplay(a?: string | null, b?: string | null): number {
  const ids = listPlatforms().map(p => p.id)
  const rank = (p: string) => {
    const known = ids.indexOf(p)
    return known >= 0 ? known : p === 'composite' ? ids.length : p ? ids.length + 1 : ids.length + 2
  }
  const left = a ?? ''
  const right = b ?? ''
  return rank(left) - rank(right) || (left < right ? -1 : left > right ? 1 : 0)
}

/** Sort only a rendering copy. Never use this result to choose business defaults. */
export function sortGroupsForDisplay<T extends DisplayGroup>(groups: readonly T[]): T[] {
  return groups.map((group, index) => ({ group, index })).sort((a, b) => {
    const platform = comparePlatformsForDisplay(a.group.platform, b.group.platform)
    if (platform) return platform
    const av = Number.isInteger(a.group.sort_order)
    const bv = Number.isInteger(b.group.sort_order)
    if (av && bv) return a.group.sort_order! - b.group.sort_order! || a.group.id - b.group.id
    if (av !== bv) return av ? -1 : 1
    return a.index - b.index || a.group.id - b.group.id
  }).map(item => item.group)
}

export function groupByPlatformForDisplay<T extends DisplayGroup>(groups: readonly T[], unknownLabel = 'Unknown platform') {
  const sections: { platform: string; label: string; groups: T[] }[] = []
  for (const group of sortGroupsForDisplay(groups)) {
    const platform = group.platform ?? ''
    let section = sections[sections.length - 1]
    if (!section || section.platform !== platform) {
      section = { platform, label: platform === 'composite' ? 'Composite' : platformDisplayName(platform) || unknownLabel, groups: [] }
      sections.push(section)
    }
    section.groups.push(group)
  }
  return sections
}

export function buildGroupSelectOptions<T extends DisplayGroup, O>(groups: readonly T[], toOption: (group: T) => O, unknownLabel?: string): (O | GroupDisplayHeader)[] {
  return groupByPlatformForDisplay(groups, unknownLabel).flatMap(section => [
    { value: `__group_platform:${section.platform}`, label: section.label, kind: 'group' as const, disabled: true as const },
    ...section.groups.map(toOption),
  ])
}

export function sortGroupIdsForDisplay(ids: readonly number[], groups: readonly DisplayGroup[]): number[] {
  const selected = new Set(ids)
  const known = sortGroupsForDisplay(groups.filter(g => selected.has(g.id))).map(g => g.id)
  const knownIDs = new Set(known)
  return [...known, ...ids.filter(id => !knownIDs.has(id))]
}
