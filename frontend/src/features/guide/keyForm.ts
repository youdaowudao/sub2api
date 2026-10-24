// Public form markers only; business field values are never read or changed here.
const one = (selector: string): HTMLElement | undefined => {
  const matches = document.querySelectorAll<HTMLElement>(selector)
  if (matches.length > 1) throw new Error('ambiguous-key-form')
  return matches[0]
}
export function createKeyFormGuide() {
  let warning: HTMLElement | null = null
  const dialog = () => one('[role="dialog"][aria-modal="true"]:has(#key-form) .modal-content')
  const name = () => one('#key-form [data-tour="key-form-name"]')
  const group = () => one('#key-form [data-tour="key-form-group"]')
  const submit = () => one('[data-tour="key-form-submit"][form="key-form"]')
  const section = () => one('#key-form [data-tour="key-form-quota"]')
  function remove() { warning?.remove(); warning = null }
  return {
    dialog, name, group, submit, section,
    isEdit: () => Boolean(name() && !one('#key-form [data-tour="key-form-provider"]')),
    groupAttention() {
      const trigger = group()?.querySelector('button')
      if (trigger?.getAttribute('aria-expanded') !== 'true') return trigger
      const lists = [...document.querySelectorAll<HTMLElement>('.select-dropdown-portal[role="listbox"]')]
        .filter(node => node.getBoundingClientRect().width > 0 && getComputedStyle(node).visibility !== 'hidden')
      if (lists.length !== 1) return null
      return [...lists[0].querySelectorAll<HTMLElement>('[role="option"]')]
        .find(node => node.getAttribute('aria-disabled') !== 'true' && !node.classList.contains('select-option-group'))
    },
    showWarning({ scroll = false } = {}) {
      const target = section()
      if (!target) throw new Error('quota-target-missing')
      if (warning?.parentElement !== target) {
        remove()
        warning = document.createElement('div'); warning.id = 's2o-quota-warning'; warning.setAttribute('role', 'note')
        const cross = document.createElement('span'); cross.className = 's2o-quota-cross'; cross.setAttribute('aria-hidden', 'true'); cross.textContent = '×'
        const text = document.createElement('span')
        const title = document.createElement('strong'); title.textContent = '这里不是充值，请勿填写充值金额'
        const detail = document.createElement('span'); detail.textContent = '这是密钥消费上限。下方限制保持原样，核对后确认即可。'
        text.append(title, detail); warning.append(cross, text); target.prepend(warning)
      }
      if (scroll) target.scrollIntoView({ block: 'start', inline: 'nearest' })
      return warning
    },
    remove,
  }
}
