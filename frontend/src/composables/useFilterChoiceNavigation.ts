import type { Ref } from 'vue'

/** Keep focus in the search field, navigating only selectable dropdown rows. */
export function useFilterChoiceNavigation(container: Ref<HTMLElement | null>, open: Ref<boolean>, onClose?: () => void) {
  let active: HTMLElement | undefined
  function reset() { active?.removeAttribute('data-keyboard-active'); active = undefined }
  function keydown(event: KeyboardEvent) {
    if (event.isComposing || event.keyCode === 229) return
    if (event.key === 'Escape' && open.value) { event.preventDefault(); open.value = false; reset(); onClose?.(); return }
    const rows = Array.from(container.value?.querySelectorAll<HTMLElement>('[data-filter-choice]') || []).filter(el => !el.hasAttribute('disabled'))
    if (!open.value || !rows.length) return
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const current = rows.indexOf(active!)
      reset()
      const next = current < 0 ? (event.key === 'ArrowDown' ? 0 : rows.length - 1) : (current + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length
      active = rows[next]
      active.setAttribute('data-keyboard-active', 'true')
      active.scrollIntoView?.({ block: 'nearest' })
    } else if (event.key === 'Enter' && active && rows.includes(active)) {
      event.preventDefault()
      const chosen = active
      reset(); chosen.click()
    }
  }
  return { keydown, reset }
}
