import { sortGroupsForDisplay } from '@/utils/groupDisplayOrder'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'
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

/** 「GPT 第一个分组」：按密钥页的分组显示顺序，取第一个 OpenAI 分组 */
export function pickFirstGptGroup<T extends Pick<Group, 'id' | 'platform'>>(groups: readonly T[]): T | null {
  return sortGroupsForDisplay(groups as readonly (T & Group)[]).find((group) => getKeyGroupProvider(group.platform) === 'openai') ?? null
}
