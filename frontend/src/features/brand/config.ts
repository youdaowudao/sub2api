/**
 * 品牌页面固定配置：QQ 交流群、品牌名高亮词、背景特效里的模型名。
 * 改群号或群名片需要改这里并重新发版。
 */

export const qqGroup = {
  number: '1020015031',
  name: '好用AI客服通知群',
  link: 'https://qm.qq.com/q/o80PFAbey4',
  /** 群名片原图，二维码区域在 (205, 753)，边长 873 */
  cardImage: '/brand/qq-group-card.jpg',
  penguinImage: '/brand/qq-penguin.png'
} as const

/** 群名片中二维码的位置，用于在弹窗里只露出二维码部分 */
export const qqCardCrop = {
  width: 1284,
  x: 205,
  y: 753,
  size: 873
} as const

/** 站点名里第一次出现的这个词使用渐变色 */
export const brandHighlight = 'Good'

/** 首页背景两侧的模型名 */
export const streamModels = {
  left: ['Claude', 'GPT', 'Gemini', 'GLM'],
  right: ['DeepSeek', 'Grok', 'Kimi', 'MiniMax']
} as const

/** 仪表盘「快速开始」：固定的助手命令与文档首页；命令不含密钥 */
export const quickConnect = {
  helperCommand: 'irm https://docs.usegoodai.com/install/helper.ps1 | iex',
  docsHome: 'https://docs.usegoodai.com/'
} as const

/** 收起横条右侧的小提示：顺序就是轮换顺序，action 为可选的跳转或弹窗 */
export const quickStartTips = [
  { id: 'invite', to: '/affiliate' },
  { id: 'newChat' },
  { id: 'oneAccount', to: '/keys' },
  { id: 'keySafe', to: '/keys' },
  { id: 'error', qr: true }
] as const

/** 每条提示展示的小时数 */
export const tipRotateHours = 5

/** 按绝对时间（UTC 纪元）每 5 小时换一条，所有人同一时刻看到的是同一条 */
export function currentTipIndex(now = Date.now(), count: number = quickStartTips.length): number {
  return Math.floor(now / (tipRotateHours * 3600_000)) % count
}

/** 一键导入的目标软件，对应导入链接里的 app 参数 */
export type ImportApp = 'codex' | 'claude' | 'image' | 'antigravity'

type ImportGroup = { id?: number; platform?: string; status?: string }

/** 两个入口按站点设置和分组 ID 决定导入目标。 */
export function importAppForGroup(group?: ImportGroup | null, imageGroupId = 0): ImportApp | null {
  if (!group) return null
  // Antigravity App 只使用 Gemini 原生协议，不依赖 OpenAI 画图分组设置。
  if (group.platform === 'gemini') return 'antigravity'
  if (imageGroupId > 0 && group.id === imageGroupId) {
    return group.platform === 'openai' && group.status === 'active' ? 'image' : null
  }
  if (group.platform === 'anthropic') return 'claude'
  if (group.platform === 'openai' && isCodingImportConfigured(imageGroupId)) return 'codex'
  return null
}

export function isCodingImportConfigured(imageGroupId?: number): boolean {
  return Number.isSafeInteger(imageGroupId) && (imageGroupId === -1 || (imageGroupId ?? 0) > 0)
}

export function canQuickImport(group?: ImportGroup | null, imageGroupId?: number): boolean {
  return importAppForGroup(group, imageGroupId) !== null
}

/** 接入助手的导入链接，格式与助手 connect/link.go 一致；不带其它参数（带地址参数助手会拒绝） */
export function buildQuickConnectLink(key: string, name?: string, app: ImportApp = 'codex'): string {
  const params = new URLSearchParams({ v: app === 'claude' ? '1' : '2', app, key })
  if (name) params.set('name', name)
  return `usegoodai://import?${params.toString()}`
}
