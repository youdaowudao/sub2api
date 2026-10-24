<script setup lang="ts">
import { Marked, Renderer } from 'marked'
import DOMPurify from 'dompurify'
import markdown from '../content/quick-start/quick-start.md?raw'
import '../content/quick-start/quick-start.css'

const images = import.meta.glob('../content/quick-start/images/*.jpg', { eager: true, query: '?url', import: 'default' }) as Record<string, string>
const escape = (value: string) => value.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]!)

function render(): string {
  try {
    const marked = new Marked()
    const tokens = marked.lexer(markdown.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, ''))
    const command = tokens.find((token, index) => token.type === 'code' && tokens[index - 1]?.type === 'html' && tokens[index - 1].raw.includes('data-onboarding="windows-command"'))
    const renderer = new Renderer()
    const renderCode = renderer.code
    renderer.code = function (token) {
      if (token !== command) return renderCode.call(this, token)
      return '<label class="command-label" for="windows-command">Windows PowerShell 配置命令</label><textarea id="windows-command" aria-label="Windows PowerShell 配置命令" readonly spellcheck="false" rows="2">' + escape(token.text.trim()) + '</textarea><p class="manual-copy-hint">双击命令行选中整条命令 → Ctrl+C 复制。</p>\n'
    }
    marked.use({ renderer })
    const clean = DOMPurify.sanitize(marked.parser(tokens), { ADD_ATTR: ['data-onboarding', 'readonly', 'target'], ADD_TAGS: ['textarea'] })
    const template = document.createElement('template')
    template.innerHTML = clean
    const imageUrl = (value: string) => {
      const name = value.match(/^\/images\/quick-start\/([^/]+\.jpg)$/)?.[1]
      return name ? images[`../content/quick-start/images/${name}`] : undefined
    }
    for (const image of template.content.querySelectorAll('img')) {
      const url = imageUrl(image.getAttribute('src') || '')
      if (url) image.setAttribute('src', url)
    }
    for (const link of template.content.querySelectorAll('a')) {
      const href = link.getAttribute('href') || ''
      const localImage = imageUrl(href)
      const url = localImage || (href.startsWith('/') ? 'https://docs.usegoodai.com' + href : href)
      link.setAttribute('href', url)
      if (/^https?:/.test(url) || localImage) { link.target = '_blank'; link.rel = 'noopener noreferrer' }
    }
    return template.innerHTML
  } catch { return '<pre>' + escape(markdown) + '</pre>' }
}
const html = render()
function selectCommand(event: MouseEvent) {
  const input = event.target
  if (!(input instanceof HTMLTextAreaElement) || input.id !== 'windows-command' || !input.readOnly) return
  event.preventDefault()
  input.select()
}
function commandCopied(event: ClipboardEvent) {
  const input = event.target
  if (!(input instanceof HTMLTextAreaElement) || input.id !== 'windows-command' || !input.readOnly) return
  if (input.selectionStart === 0 && input.selectionEnd === input.value.length) {
    input.dispatchEvent(new CustomEvent('s2o:command-copied', { bubbles: true }))
  }
}
</script>

<template>
  <div class="s2o-quick-start" @dblclick="selectCommand" @copy="commandCopied">
    <article v-html="html"></article>
  </div>
</template>
