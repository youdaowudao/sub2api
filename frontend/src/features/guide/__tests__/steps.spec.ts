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
    expect(Object.fromEntries(Object.entries(steps).filter(([id]) => !id.startsWith('more-')))).toEqual(expected)
    expect(Object.values(steps).map(step => step.description).join(' ')).not.toContain('GPT生图')
    expect(FLOW_VERSION).toBe('ordinary-source-v1')
    expect(anchors.menu).toBe('[data-tour="header-menu"]')
    expect(moduleCatalog.map(module => module.title)).toEqual(['创建密钥', '使用密钥', '快速开始', '余额充值', '更多功能与帮助'])
  })
})
