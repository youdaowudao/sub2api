import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { URL as NodeURL } from 'node:url'
import { describe, expect, it } from 'vitest'
import { generateDriver, ORIGINAL_JS_SHA256, ORIGINAL_CSS_SHA256 } from '../driver/build-driver.mjs'

describe('private Driver 1.4.0 build', () => {
  it('reproduces the committed bundle byte for byte from the exact upstream source', () => {
    const generated = generateDriver()
    expect(generated.js).toBe(readFileSync(new NodeURL('../driver/driver.mjs', import.meta.url), 'utf8'))
    expect(generated.css).toBe(readFileSync(new NodeURL('../driver/driver.css', import.meta.url), 'utf8'))
    for (const [file, hash] of [['driver.js.mjs', ORIGINAL_JS_SHA256], ['driver.css', ORIGINAL_CSS_SHA256]]) {
      const content = readFileSync(new NodeURL(`../../../../node_modules/driver.js/dist/${file}`, import.meta.url))
      expect(createHash('sha256').update(content).digest('hex')).toBe(hash)
    }
    expect(generated.js).not.toMatch(/(["'`.#\s])driver-/)
    expect(generated.css).not.toMatch(/(["'`.#\s])driver-/)
    expect(generated.css).not.toContain('.s2o-driver-active *{pointer-events:none}')
    expect(readFileSync(new NodeURL('../styles/guide.css', import.meta.url), 'utf8')).not.toMatch(/\.s2o-driver-active-element\s*\{[^}]*outline/)
  })
})
