import type { GuideModule } from './config'

export const moreRoles = ['affiliate', 'quick-start', 'tutorial', 'support', 'pricing', 'radar'] as const
export type MoreRole = typeof moreRoles[number]
export const moreIntroductions: Record<MoreRole, { title: string; description: string }> = {
  affiliate: { title: '邀请返利', description: '从这里邀请好友，查看本站的返利规则和邀请记录。具体条件以页面说明为准。' },
  'quick-start': { title: '新用户快速开始', description: '需要查看接入步骤时，可以从这里打开快速开始。' },
  tutorial: { title: '使用教程', description: '从这里查看使用方法和常见操作说明。' },
  support: { title: '联系客服（新用户福利）', description: '需要帮助时，从这里查看客服联系方式和新用户福利，具体规则以页面说明为准。' },
  pricing: { title: '模型价格', description: '从这里查看模型和分组价格，使用前可按页面说明核对。' },
  radar: { title: '智商雷达', description: '从这里查看 GPT 表现相关的工具与说明。' },
}
export const moreRoleForStep = (step: string): MoreRole | undefined => moreRoles.find(role => step === `more-${role}` || step === `more-open-${role}`)

// null means settings have not arrived. An empty array means no bound entry is available.
// The sidebar and this resolver read the same saved settings; page bodies play no part.
export function resolveMoreTargets(modules: GuideModule[], settings: any): { role: MoreRole; selector: string }[] | null {
  if (settings == null) return null
  if (typeof settings !== 'object' || Array.isArray(settings)) throw new Error('invalid-menu-settings')
  const bindings = modules.find(item => item.id === 'more')?.menuIds || {}
  const quick = modules.find(item => item.id === 'quick-start')?.menuId
  const menus = settings.custom_menu_items || []
  if (!Array.isArray(menus)) throw new Error('invalid-menu-settings')
  const used = new Set<string>()
  return moreRoles.flatMap<{ role: MoreRole; selector: string }>(role => {
    if (role === 'affiliate') return settings.affiliate_enabled === true ? [{ role, selector: '[data-tour="sidebar-affiliate"]' }] : []
    const id = role === 'quick-start' ? quick : bindings[role]
    if (!id) return []
    if (!/^[A-Za-z0-9_-]{1,32}$/.test(id) || used.has(id)) throw new Error('ambiguous-menu-binding')
    used.add(id)
    const matches = menus.filter((item: any) => item?.id === id)
    if (matches.length > 1) throw new Error('ambiguous-menu-binding')
    return matches[0]?.visibility === 'user' ? [{ role, selector: `[data-tour="sidebar-custom-${id}"]` }] : []
  })
}
