import { sortGroupsForDisplay } from '@/utils/groupDisplayOrder'
import { keysAPI } from '@/api/keys'
import { getPublicSettings } from '@/api/auth'
import { importAppForGroup } from './config'
import type { Group } from '@/types'

/**
 * 新密钥的默认名：没被用过就用「测试」，否则依次用「测试1」「测试2」……
 * base 跟着界面语言走（中文「测试」，英文「test」）。
 */
export function nextTestKeyName(existingNames: readonly (string | null | undefined)[], base: string): string {
  const used = new Set(existingNames.map((name) => (name ?? '').trim()))
  if (!used.has(base)) return base
  for (let n = 1; ; n++) {
    const candidate = `${base}${n}`
    if (!used.has(candidate)) return candidate
  }
}

/** 按密钥页的分组显示顺序，取第一个适合导入 Codex 的分组。 */
export function pickFirstGptGroup<T extends Pick<Group, 'id' | 'platform'>>(groups: readonly T[], imageGroupId?: number): T | null {
  return sortGroupsForDisplay(groups).find((group) => importAppForGroup(group, imageGroupId) === 'codex') ?? null
}

/** 两个入口发链接前重新确认当前分组和站点设置。 */
export async function getCurrentQuickImport(id: number) {
  const [key, settings] = await Promise.all([keysAPI.getById(id), getPublicSettings()])
  const app = key.status === 'active' && key.group?.status === 'active' ? importAppForGroup(key.group, settings.quick_import_image_group_id) : null
  return { key, app }
}
