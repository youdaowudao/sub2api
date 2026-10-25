// Presentation only. Never clicks, selects, fills, or submits a host control.
export function createAttention(onCount: (count: number) => void = () => {}, onUnavailable: () => void = () => {}) {
  let step = '', hint: any = {}, active: any = null, beacon: HTMLElement | null = null, failedTarget: Element | null = null, destroyed = false, composing = false;
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const visible = (element: any) => {
    if (!element?.isConnected || element.disabled) return false;
    const rect = element.getBoundingClientRect(), style = getComputedStyle(element);
    return rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.left < innerWidth && rect.bottom > 0 && rect.top < innerHeight && style.visibility !== 'hidden' && style.display !== 'none';
  };
  const find = (selector: string): any => document.querySelector(selector);
  const next = () => find('.s2o-popover .s2o-driver-popover-next-btn:not([disabled])');
  function target() {
    if (hint.mode === 'next') return next();
    if (hint.selector) return typeof hint.selector === 'function' ? hint.selector() : find(hint.selector);
    const anchor = ({ menu: '[data-tour="header-menu"]', navigate: '[data-tour="sidebar-my-keys"]', create: '[data-tour="keys-create-btn"]', submit: '[data-tour="key-form-submit"]' } as Record<string, string>)[step];
    if (anchor) return find(anchor);
    if (step === 'name') {
      const input = find('[data-tour="key-form-name"]');
      return !input?.value.trim() || composing ? input : next();
    }
    if (step === 'provider') {
      const input = find('[data-tour="key-form-provider"] input[value="openai"]:not(:disabled)');
      return input?.checked ? next() : input?.closest('label')?.querySelector('span');
    }
    if (step === 'group') {
      const trigger = find('[data-tour="key-form-group"] button');
      if (trigger?.getAttribute('aria-expanded') !== 'true') {
        // A selected option renders GroupBadge with nested spans; the placeholder
        // is one plain span. This is a pinned public DOM contract, not Vue state.
        return trigger?.querySelector('.select-value > span > span') ? next() : trigger;
      }
      // This Select teleports its list to body and exposes no aria-controls.
      // Require its trigger to be open and exactly one visible matching portal.
      const lists = [...document.querySelectorAll('.select-dropdown-portal[role="listbox"]')].filter(visible);
      if (lists.length !== 1) return null;
      return [...lists[0].querySelectorAll('[role="option"]')].find(option => option.getAttribute('aria-disabled') !== 'true' && !option.classList.contains('select-option-group') && visible(option));
    }
    if (['welcome', 'review', 'created', 'finish', 'recharge-unavailable'].includes(step)) return next();
    return null;
  }
  function waiting(message = '正在等待页面控件…') {
    if (!beacon) {
      beacon = document.createElement('div');
      beacon.id = 's2o-attention-wait';
      document.body.append(beacon);
    }
    if (beacon.textContent !== message) beacon.textContent = message;
    if (beacon.hidden) beacon.hidden = false;
    return beacon;
  }
  function clear() {
    active?.animation.cancel();
    if (active?.row) active.element.classList.remove('s2o-row-attention');
    active = null; onCount(0);
  }
  function animate(element: HTMLElement) {
    const row = element.matches('tr[data-row-id]');
    const emerald = { outline: '4px solid #047857', outlineOffset: '3px', boxShadow: 'inset 0 0 0 3px #047857' };
    const mint = { outline: '4px solid #2dd4bf', outlineOffset: '3px', boxShadow: 'inset 0 0 0 3px #2dd4bf' };
    if (element.matches('.s2o-popover .s2o-driver-popover-next-btn, #s2o-attention-wait')) {
      Object.assign(emerald, { backgroundColor: '#047857', color: '#ffffff' });
      Object.assign(mint, { backgroundColor: '#a7f3d0', color: '#064e3b' });
    }
    // Sticky cells paint above their row. One inherited color animation drives
    // a continuous inner border on the cells, without changing their stacking,
    // position or background and without adding an animation to each cell.
    if (row) element.classList.add('s2o-row-attention');
    try {
      const frames = row ? [
        { '--s2o-row-attention-color': '#047857', offset: 0 },
        { '--s2o-row-attention-color': '#2dd4bf', offset: 0.5 },
        { '--s2o-row-attention-color': '#047857', offset: 1 },
      ] : [{ ...emerald, offset: 0 }, { ...mint, offset: 0.5 }, { ...emerald, offset: 1 }];
      const animation = element.animate(frames, { duration: reduced.matches ? 2000 : 1200, iterations: Infinity, easing: 'steps(2, end)' });
      animation.id = 's2o-attention'; return animation;
    } catch (error) {
      if (row) element.classList.remove('s2o-row-attention');
      throw error;
    }
  }
  function refresh() {
    if (destroyed) return;
    try {
      let element = target();
      // While awaiting the next DOM target, do not invite clicking a stale Next.
      if (!step) element = waiting();
      else if (!visible(element)) element = find('.s2o-popover');
      if (!visible(element)) element = waiting();
      if (element === failedTarget) element = waiting('请按引导提示继续操作');
      if (beacon && beacon !== element && !beacon.hidden) beacon.hidden = true;
      if (active?.element === element && active.reduced === reduced.matches && active.animation.playState === 'running') return;
      clear();
      let animation;
      try { animation = animate(element); }
      catch { failedTarget = element; element = waiting('请按引导提示继续操作'); animation = animate(element); }
      active = { element, animation, row: element.matches('tr[data-row-id]'), reduced: reduced.matches }; onCount(1);
    } catch {
      clear(); onUnavailable(); // Stop cleanly if even our own indicator cannot render.
    }
  }
  return {
    setStep(value: string, context: any = {}) { step = value || ''; hint = context || {}; failedTarget = null; refresh(); },
    setComposing(value: boolean) { composing = value; refresh(); }, refresh,
    destroy() { destroyed = true; clear(); beacon?.remove(); beacon = null; },
  };
}
