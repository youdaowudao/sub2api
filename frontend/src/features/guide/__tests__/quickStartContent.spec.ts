import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createHash } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import hashes from './fixtures/quick-start-sha256.json'

describe('owned quick-start content', () => {
  it('preserves the prototype article and images byte for byte, without an installer or bridge', () => {
    const base = resolve('src/features/guide/content/quick-start')
    for (const [path, digest] of Object.entries(hashes)) expect(createHash('sha256').update(readFileSync(resolve(base, path))).digest('hex')).toBe(digest)
    const markdown = readFileSync(resolve(base, 'quick-start.md'), 'utf8')
    for (const id of ['windows-step', 'windows-run', 'windows-command', 'codex-test', 'models', 'problems', 'support']) expect(markdown.split(`data-onboarding="${id}"`)).toHaveLength(2)
    expect(markdown.match(/irm https:\/\/docs\.usegoodai\.com\/install\/codex\.ps1 \| iex/g)).toHaveLength(1)
    const css = readFileSync(resolve(base, 'quick-start.css'), 'utf8')
    expect(css).toContain('overflow-y: auto'); expect(css).toContain('font: 17px/1.8'); expect(css).toContain('max-width: 860px')
    expect(css).toContain('padding: 30px 28px 90px'); expect(css).toContain('@media(max-width:600px)')
    expect(css).not.toMatch(/(^|\})\s*(body|:root|main)\s*\{/)
  })
})
