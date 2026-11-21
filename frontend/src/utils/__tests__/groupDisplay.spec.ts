import { describe, expect, it } from 'vitest'
import {
  GROUP_DISPLAY_CATEGORIES,
  GROUP_DISPLAY_ICON_PLATFORMS,
  buildGroupSelectOptionsByCategory,
  groupCategory,
  groupGroupsByCategory,
  groupIconPlatform,
} from '../groupDisplay'
import { sortGroupsForDisplay } from '../groupDisplayOrder'

describe('group display', () => {
  it.each([
    ['openai', 'openai'], ['anthropic', 'anthropic'], ['gemini', 'gemini'],
    ['kimi', 'domestic'], ['zhipu', 'domestic'], ['deepseek', 'domestic'], ['minimax', 'domestic'],
    ['grok', 'other'], ['antigravity', 'other'], ['composite', 'other'], ['opencode_go', 'other'],
    ['typesafe', 'other'], ['command_code', 'other'], ['cline', 'other'], ['unknown', 'other'],
  ])('classifies %s as %s by default', (platform, category) => {
    expect(groupCategory({ platform })).toBe(category)
  })

  it('accepts the four manual categories, with the same rule for image groups', () => {
    const imageGroup = { platform: 'openai', allow_image_generation: true }
    expect(GROUP_DISPLAY_CATEGORIES).toEqual(['openai', 'anthropic', 'gemini', 'domestic', 'other'])
    expect(groupCategory(imageGroup)).toBe('openai')
    for (const category of ['openai', 'anthropic', 'gemini', 'domestic']) {
      expect(groupCategory({ platform: 'grok', display_category: ` ${category} ` })).toBe(category)
    }
    expect(groupCategory({ ...imageGroup, display_category: ' domestic ' })).toBe('domestic')
    expect(groupCategory({ ...imageGroup, display_category: 'other' })).toBe('openai')
    expect(groupCategory({ ...imageGroup, display_category: 'invalid' })).toBe('openai')
  })

  it('uses only dedicated logos in the fixed order and otherwise follows the platform', () => {
    const allowed = [
      'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
      'minimax', 'deepseek', 'opencode_go', 'command_code', 'cline',
    ]
    expect(GROUP_DISPLAY_ICON_PLATFORMS).toEqual(allowed)
    for (const icon of allowed) {
      expect(groupIconPlatform({ platform: 'composite', display_icon: ` ${icon} ` })).toBe(icon)
    }
    for (const icon of [undefined, '', 'typesafe', 'composite', 'invalid']) {
      expect(groupIconPlatform({ platform: 'openai', display_icon: icon })).toBe('openai')
    }
  })

  it('returns only populated categories and builds disabled category headings', () => {
    const openai = { id: 1, platform: 'openai', name: 'GPT' }
    const grok = { id: 2, platform: 'grok', name: 'Grok' }
    expect(groupGroupsByCategory([grok, openai]).map(section => section.category)).toEqual(['openai', 'other'])
    expect(groupGroupsByCategory([openai]).map(section => section.category)).toEqual(['openai'])
    expect(groupGroupsByCategory([grok]).map(section => section.category)).toEqual(['other'])
    expect(groupGroupsByCategory([])).toEqual([])
    expect(buildGroupSelectOptionsByCategory([grok, openai], group => ({ value: group.id, label: group.name }), category => category)).toEqual([
      { value: '__group_category:openai', label: 'openai', kind: 'group', disabled: true },
      { value: 1, label: 'GPT' },
      { value: '__group_category:other', label: 'other', kind: 'group', disabled: true },
      { value: 2, label: 'Grok' },
    ])
  })

  it('keeps the sort order within a category based on the real platforms', () => {
    const groups = [
      { id: 101, platform: 'anthropic', sort_order: 2, display_category: 'gemini', display_icon: 'openai' },
      { id: 102, platform: 'openai', sort_order: 3, display_category: 'gemini', display_icon: 'anthropic' },
      { id: 103, platform: 'anthropic', display_category: 'gemini', display_icon: 'openai' },
      { id: 104, platform: 'openai', sort_order: 1, display_category: 'gemini', display_icon: 'anthropic' },
      { id: 105, platform: 'anthropic', sort_order: 4, display_category: 'gemini', display_icon: 'openai' },
      { id: 106, platform: 'grok', sort_order: 0 },
    ]
    const expected = sortGroupsForDisplay(groups).filter(group => group.display_category === 'gemini')
    const sections = groupGroupsByCategory(groups)
    expect(sections.map(section => section.category)).toEqual(['gemini', 'other'])
    expect(sections[0].groups).toEqual(expected)
    const newIcons = groups.map(group => ({ ...group, display_icon: 'gemini' }))
    expect(groupGroupsByCategory(newIcons)[0].groups.map(group => group.id)).toEqual(expected.map(group => group.id))
  })
})
