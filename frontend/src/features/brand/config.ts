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
