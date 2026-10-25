import { describe, expect, it } from 'vitest'
import prototype from './fixtures/prototype-steps.json'
import { FLOW_VERSION, anchors, steps } from '../steps'
import { moduleCatalog } from '../config'

describe('approved prototype copy migration', () => {
  it('keeps every original step and exact copy except the explicitly approved changes', () => {
    const expected: Record<string, any> = structuredClone(prototype)
    delete expected['quick-loading']
    delete expected.submit.nextBtnText
    expected.submit.showButtons = ['previous']
    expected.review.description = '密钥已创建。请在列表里找到刚才的密钥，核对名称和分组，确认后结束引导。'
    expected.welcome.title = '欢迎来到本站'
    expected.welcome.description = '如果您是第一次使用，可以跟随引导完成几个简单操作，快速了解本站的接入和使用方法。已有使用经验的用户，可以点击右上角“退出”，自行操作。'
    expected.submit.description = '额度限制不是充值金额，请勿填写充值金额。无需再填写其他内容，直接点击“创建”即可。'
    expected['quick-key-empty'].description = '这里还没有密钥。先退出并创建密钥，再从右下角「使用引导」查看本节。'
    expected.finish = { title: '本次引导结束', description: '需要时，点右下角「使用引导」选择章节再次查看。', nextBtnText: '完成' }
    expected['more-help'] = { title: '更多功能与帮助', description: '<p>这里介绍几个常用功能。</p><ul><li><strong>邀请返利</strong>：邀请好友，查看返利规则和记录。</li><li><strong>使用教程</strong>：查看使用方法和常见操作说明。</li><li><strong>联系客服（新用户福利）</strong>：查看客服联系方式和新用户福利。</li></ul>', nextBtnText: '继续' }
    expected['more-tools'] = { title: '模型与工具', description: '<ul><li><strong>模型价格</strong>：查看模型和分组价格。</li><li><strong>智商雷达</strong>：查看 GPT 表现相关工具及说明。</li><li><strong>GPT生图</strong>：在线使用 GPT 生成图片。</li></ul><p>具体入口以当前菜单为准。</p>', nextBtnText: '继续', prevBtnText: '上一步', showButtons: ['previous', 'next'] }
    expected['more-help'].description += '<p><strong>遇到问题怎么办</strong>：先查看使用教程和使用记录；仍无法解决时，联系客服，说明所用客户端、模型和出错时间，并提供报错截图。截图中请隐藏密钥等敏感信息。</p>'
    expect(steps).toEqual(expected)
    expect(FLOW_VERSION).toBe('ordinary-source-v1')
    expect(anchors.menu).toBe('[data-tour="header-menu"]')
    expect(moduleCatalog.map(module => module.title)).toEqual(['创建密钥', '使用密钥', '快速开始', '余额充值', '更多功能与帮助'])
  })
})
