import { describe, it, expect } from 'vitest'
import { buildApiKeyGroupFilterOptions } from '../apiKeyGroupFilterOptions'
import type { AdminGroup } from '@/types'

const labels = { all: 'All', exclusive: 'Exclusive', public: 'Public', subscription: 'Subscription', disabled: 'Disabled', categoryLabel: (key: string) => key.split('.').pop()! }
function g(partial: Partial<AdminGroup>): AdminGroup {
  return { id: 0, name: '', platform: 'openai', status: 'active', is_exclusive: false, subscription_type: 'standard', ...partial } as AdminGroup
}
describe('buildApiKeyGroupFilterOptions', () => {
  it('groups by shared display category while keeping business type and real IDs', () => {
    const opts = buildApiKeyGroupFilterOptions([
      g({id:1, name:'Excl',is_exclusive:true}),
      g({id:2,name:'Pub',platform:'gemini'}),
      g({id:3,name:'Sub',platform:'grok',display_category:'openai',subscription_type:'subscription'}),
      g({id:4,name:'Disabled',platform:'gemini',status:'inactive'}),
    ], labels)
    expect(opts.map(o => o.value)).toEqual([null,'__group_category:openai',1,3,'__group_category:gemini',2,4])
    expect(opts).toContainEqual({value:1,label:'Excl · Exclusive'})
    expect(opts).toContainEqual({value:2,label:'Pub · Public'})
    expect(opts).toContainEqual({value:3,label:'Sub · Subscription'})
    expect(opts).toContainEqual({value:4,label:'Disabled · Disabled'})
    for (const heading of opts.filter(o => o.kind === 'group')) expect(heading.disabled).toBe(true)
  })
  it('gives subscription precedence over exclusive and keeps disabled group selectable', () => {
    const opts = buildApiKeyGroupFilterOptions([g({id:9,name:'X',is_exclusive:true,subscription_type:'subscription'}),g({id:10,name:'Y',status:'inactive',is_exclusive:true})],labels)
    expect(opts).toContainEqual({value:9,label:'X · Subscription'})
    expect(opts).toContainEqual({value:10,label:'Y · Disabled'})
    expect(opts.filter(o => o.kind === 'group')).toHaveLength(1)
  })
  it('skips empty categories and returns only all on empty input', () => {
    expect(buildApiKeyGroupFilterOptions([],labels)).toEqual([{value:null,label:'All'}])
    const opts=buildApiKeyGroupFilterOptions([g({id:2,name:'Image',display_category:'image'})],labels)
    expect(opts.filter(o=>o.kind==='group')).toEqual([{value:'__group_category:image',label:'image',kind:'group',disabled:true}])
  })
})
