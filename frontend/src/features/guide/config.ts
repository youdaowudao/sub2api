import bundledConfiguration from './config/onboarding.json'

export type ModuleId = 'keys' | 'key-use' | 'quick-start' | 'recharge' | 'more'
export const moreMenuCatalog = [
  { id: 'tutorial', title: '使用教程' }, { id: 'support', title: '联系客服（新用户福利）' },
  { id: 'pricing', title: '模型价格' }, { id: 'radar', title: '智商雷达' },
] as const
export type MoreMenuRole = typeof moreMenuCatalog[number]['id']
export interface GuideModule { id: ModuleId; enabled: boolean; menuId?: string; menuIds?: Partial<Record<MoreMenuRole, string>> }
export interface GuideConfig { schemaVersion: 1; modules: GuideModule[] }
export const moduleCatalog: { id: ModuleId; title: string; description: string; needsMenu?: boolean }[] = [
  { id: 'keys', title: '创建密钥', description: '填写名称、选择分组，创建密钥。' },
  { id: 'key-use', title: '使用密钥', description: '复制、切换分组，了解客户端配置。' },
  { id: 'quick-start', title: '快速开始', description: '打开自定义文档，手动复制命令，在本机配置 Codex。', needsMenu: true },
  { id: 'recharge', title: '余额充值', description: '查看余额、充值入口和付款记录。' },
  { id: 'more', title: '更多功能与帮助', description: '了解常用功能与帮助入口。' },
]
export const previewConfigKey = 's2o:configuration-preview:v1'

export function normalizeConfig(value: unknown): GuideConfig {
  const input = value as Record<string, unknown> | null
  if (!input || input.schemaVersion !== 1 || !Array.isArray(input.modules) || input.modules.length > moduleCatalog.length || Object.keys(input).some(key => !['schemaVersion', 'modules'].includes(key))) throw new Error('配置格式不正确。')
  const seen = new Set<string>()
  const modules = input.modules.map((item: GuideModule) => {
    const fields = item?.id === 'quick-start' ? ['id', 'enabled', 'menuId'] : item?.id === 'more' ? ['id', 'enabled', 'menuIds'] : ['id', 'enabled']
    if (!item || !moduleCatalog.some(module => module.id === item.id) || typeof item.enabled !== 'boolean' || seen.has(item.id) || Object.keys(item).some(key => !fields.includes(key))) throw new Error('章节配置不正确。')
    if (item.menuId !== undefined && (typeof item.menuId !== 'string' || !/^[A-Za-z0-9_-]{1,32}$/.test(item.menuId))) throw new Error('请选择有效的快速开始菜单。')
    if (item.id === 'quick-start' && item.enabled && !item.menuId) throw new Error('启用快速开始时，需要选择一个自定义菜单。')
    if (item.menuIds !== undefined && (!item.menuIds || typeof item.menuIds !== 'object' || Array.isArray(item.menuIds) || Object.entries(item.menuIds).some(([role, id]) => !moreMenuCatalog.some(item => item.id === role) || typeof id !== 'string' || !/^[A-Za-z0-9_-]{1,32}$/.test(id)))) throw new Error('请选择有效的更多功能菜单。')
    seen.add(item.id)
    return { id: item.id, enabled: item.enabled, ...(item.menuId ? { menuId: item.menuId } : {}), ...(item.menuIds && Object.keys(item.menuIds).length ? { menuIds: { ...item.menuIds } } : {}) }
  })
  const boundIds = modules.flatMap(item => [...(item.menuId ? [item.menuId] : []), ...Object.values(item.menuIds || {})])
  if (new Set(boundIds).size !== boundIds.length) throw new Error('不同用途需要选择不同的自定义菜单。')
  return { schemaVersion: 1, modules }
}

export const enabledModules = (value: unknown): ModuleId[] => normalizeConfig(value).modules.filter(item => item.enabled).map(item => item.id)
export const registrationModules = (value: unknown): ModuleId[] => enabledModules(value)

// Read once for the lifetime of this document, including the opt-out query.
export const initialOptOut = new URLSearchParams(location.search).get('onboarding') === 'off'
const previewRequested = new URLSearchParams(location.search).get('onboarding-preview') === '1'
export let configuration: GuideConfig | null = null
export let configPreview = false
try {
  configuration = normalizeConfig(bundledConfiguration)
  if (previewRequested) {
    try {
      const draft = localStorage.getItem(previewConfigKey)
      if (draft) { configuration = normalizeConfig(JSON.parse(draft)); configPreview = true }
    } catch { /* An invalid or inaccessible browser draft leaves the build configuration intact. */ }
  }
} catch { console.warn('新手引导配置无效，引导已关闭。') }

export const buildConfiguration = (): GuideConfig => normalizeConfig(bundledConfiguration)
export const boundQuickStartMenuId = (): string | undefined => configuration?.modules.find(item => item.id === 'quick-start')?.menuId
