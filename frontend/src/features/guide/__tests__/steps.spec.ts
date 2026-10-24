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
    expect(steps).toEqual(expected)
    expect(FLOW_VERSION).toBe('ordinary-source-v1')
    expect(anchors.menu).toBe('[data-tour="header-menu"]')
    expect(moduleCatalog.map(module => module.title)).toEqual(['创建密钥', '使用密钥', '快速开始', '余额充值'])
  })
})
