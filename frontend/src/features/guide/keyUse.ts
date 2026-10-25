// Stable public markers only. Never reads key text, input values or the clipboard.
const unique = (root: ParentNode | null | undefined, selector: string): HTMLElement | undefined => {
  const nodes = root?.querySelectorAll<HTMLElement>(selector) || []
  if (nodes.length > 1) throw new Error('ambiguous-key-control')
  return nodes[0]
}
export function createKeyUse() {
  let selected: { node: HTMLElement; id: string } | null = null
  const borrowed = new Map<HTMLElement, Record<string, string | null>>()
  const name = (node: HTMLElement) => unique(node, '[data-tour="key-name"]')
  const rows = () => [...document.querySelectorAll<HTMLElement>('main [data-row-id]')].filter(node => name(node))
  function row(): HTMLElement | undefined {
    const current = rows()
    if (selected && current.includes(selected.node)) return selected.node
    if (selected) {
      const matches = current.filter(node => node.dataset.rowId === selected!.id)
      if (matches.length === 1) return (selected.node = matches[0])
    }
    return undefined
  }
  function restoreNames() {
    const ours: Record<string, string> = { role: 'button', tabindex: '0', 'aria-label': '选择这把密钥' }
    for (const [node, saved] of borrowed) for (const [attribute, value] of Object.entries(saved)) {
      if (node.getAttribute(attribute) === ours[attribute]) value === null ? node.removeAttribute(attribute) : node.setAttribute(attribute, value)
    }
    borrowed.clear()
  }
  function prepareNames() {
    for (const node of rows().map(name)) {
      if (!node || borrowed.has(node)) continue
      borrowed.set(node, Object.fromEntries(['role', 'tabindex', 'aria-label'].map(attribute => [attribute, node.getAttribute(attribute)])))
      node.setAttribute('role', 'button'); node.setAttribute('tabindex', '0'); node.setAttribute('aria-label', '选择这把密钥')
    }
  }
  const empty = () => unique(document, 'main .empty-state')
  const list = () => {
    const item = rows()[0]
    return item ? item.closest<HTMLElement>('.table-wrapper') || item.parentElement : empty()
  }
  const control = (id: string) => () => unique(row(), `[data-tour="${id}"]`)
  const actions = control('key-actions'), copy = control('key-copy'), group = control('key-group')
  const popup = () => unique(document, '[data-tour="key-group-dropdown"]')
  const dialog = () => unique(document, '[role="dialog"][aria-modal="true"] .modal-content')
  const close = () => unique(dialog(), '.modal-header button[aria-label="Close modal"]')
  function choose(node: HTMLElement): boolean {
    if (!rows().includes(node) || !node.dataset.rowId) return false
    selected = { node, id: node.dataset.rowId }; restoreNames(); return true
  }
  return {
    rows, row, list, empty, name, copy, group, popup, dialog, close, actions, overview: row,
    useButton: control('key-use'), ccsButton: control('key-ccs'), editButton: control('key-edit'),
    pickTarget: () => rows()[0] || empty(),
    firstGroup: () => popup()?.querySelector<HTMLElement>('[data-tour="key-group-option"]'),
    prepareNames, restoreNames, choose,
    selectById(id: number) { const node = rows().find(node => node.dataset.rowId === String(id)); return node ? choose(node) : false },
    chooseAt(target: Element): boolean {
      if (target.closest('button, a, input, select, textarea, [role="checkbox"], [role="switch"]')) return false
      const match = rows().find(node => node.contains(target))
      return !!match && choose(match)
    },
    get selectedKeyId() { return row() ? Number(selected?.id) : undefined },
    get hasSelection() { return Boolean(selected) },
    destroy() { restoreNames(); selected = null },
  }
}
