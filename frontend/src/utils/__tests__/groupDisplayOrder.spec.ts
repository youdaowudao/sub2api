import { describe, expect, it } from 'vitest'
import { sortGroupsForDisplay, groupByPlatformForDisplay, buildGroupSelectOptions, sortGroupIdsForDisplay } from '../groupDisplayOrder'

const rows = [
  { id: 6, platform: '', sort_order: -10 },
  { id: 5, platform: 'zzz', sort_order: 0 },
  { id: 4, platform: 'composite', sort_order: 0 },
  { id: 3, platform: 'openai', sort_order: -100 },
  { id: 2, platform: 'anthropic', sort_order: 0 },
  { id: 1, platform: 'anthropic', sort_order: -1 },
]
describe('group display order', () => {
  it('uses catalog order and administrator order, preserving zero and negative values', () => {
    const frozen = Object.freeze(rows.map(g => Object.freeze({ ...g, rate_multiplier: g.id, name: `name ${g.id}` })))
    expect(sortGroupsForDisplay(frozen).map(g => g.id)).toEqual([1, 2, 3, 4, 5, 6])
    expect(frozen.map(g => g.id)).toEqual([6, 5, 4, 3, 2, 1])
    expect(groupByPlatformForDisplay(frozen, '未识别平台').map(s => s.label)).toEqual(['Anthropic', 'OpenAI', 'Composite', 'zzz', '未识别平台'])
  })
  it('breaks ties by ID, places missing/invalid values last and keeps their original order', () => {
    const input = [
      { id: 9, platform: 'openai' }, { id: 8, platform: 'openai', sort_order: Number.NaN },
      { id: 4, platform: 'openai', sort_order: 2 }, { id: 2, platform: 'openai', sort_order: 2 },
      { id: 7, platform: 'openai', sort_order: 1.2 },
    ]
    expect(sortGroupsForDisplay(input).map(g => g.id)).toEqual([2, 4, 9, 8, 7])
  })
  it('sorts known selected IDs for rendering and retains unknown IDs at the end without mutating business order', () => {
    const ids = Object.freeze([99, 3, 2, 98, 1])
    expect(sortGroupIdsForDisplay(ids, rows)).toEqual([1, 2, 3, 99, 98])
    expect(ids).toEqual([99, 3, 2, 98, 1])
  })
  it('creates unique non-selectable headings without losing IDs', () => {
    const options = buildGroupSelectOptions(rows, g => ({ value: g.id, label: String(g.id) }))
    const headers = options.filter(o => 'kind' in o)
    expect(new Set(headers.map(o => o.value)).size).toBe(5)
    expect(headers.every(o => 'disabled' in o && o.disabled)).toBe(true)
    expect(options.filter(o => !('kind' in o)).map(o => o.value)).toEqual([1, 2, 3, 4, 5, 6])
  })
})
