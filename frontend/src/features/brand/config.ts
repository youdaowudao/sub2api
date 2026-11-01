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

/** 仪表盘「快速开始」：接入助手下载地址、导入失败时的教程、自动收起的使用记录条数 */
export const quickConnect = {
  downloads: {
    windows: 'https://docs.usegoodai.com/download/usegoodai-quick-connect-windows-x64.exe',
    mac: 'https://docs.usegoodai.com/download/usegoodai-quick-connect-mac-arm64.zip'
  },
  /** 一键导入没反应时，跳到文档「快速开始」的「运行接入命令」一节 */
  fallbackDoc: 'https://docs.usegoodai.com/quick-start#_2-运行接入命令',
  docsHome: 'https://docs.usegoodai.com/',
  /** 使用记录超过这个条数后面板默认收起 */
  collapseAfterRequests: 10
} as const

/** 接入助手的导入链接，格式与助手 connect/link.go 一致 */
export function buildQuickConnectLink(key: string, name?: string): string {
  const params = new URLSearchParams({ v: '1', app: 'codex', key })
  if (name) params.set('name', name)
  return `usegoodai://import?${params.toString()}`
}
