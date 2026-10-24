// Positions the existing Driver popover beside the current task. No host layout changes.
export function createPopoverPosition(onUnavailable = () => {}) {
  let current: any = null;
  const resolve = (value: any): any => typeof value === 'function' ? value() : typeof value === 'string' ? document.querySelector(value) : value;
  const bounds = (element: any): DOMRect | null => {
    if (!element?.isConnected) return null;
    const r = element.getBoundingClientRect(), css = getComputedStyle(element);
    return r.width > 0 && r.height > 0 && css.display !== 'none' && css.visibility !== 'hidden' ? r : null;
  };
  const intersects = (a: any, b: any, gap = 0) => a.left < b.right + gap && a.right > b.left - gap && a.top < b.bottom + gap && a.bottom > b.top - gap;
  const put = (node: HTMLElement, property: string, value: string) => { if (node.style.getPropertyValue(property) !== value) node.style.setProperty(property, value); };
  function unavailable() {
    if (!current) return;
    if (current.deadline === null) current.deadline = performance.now() + 8000;
    if (!current.wrapper.hasAttribute('data-s2o-position-waiting')) current.wrapper.setAttribute('data-s2o-position-waiting', '');
    if (performance.now() >= current.deadline) onUnavailable();
  }
  function position() {
    if (!current?.wrapper.isConnected) return;
    const { wrapper, anchor, fallback, required = false, side = 'bottom', align = 'center' } = current;
    const target = resolve(anchor) || resolve(fallback), raw = bounds(target);
    if (!target) return; // Centered welcome, ending or external acknowledgment.
    if (!raw) return unavailable();
    const viewport = window.visualViewport;
    const view = { left: (viewport?.offsetLeft || 0) + 12, top: (viewport?.offsetTop || 0) + 12,
      right: (viewport?.offsetLeft || 0) + (viewport?.width || innerWidth) - 12,
      bottom: (viewport?.offsetTop || 0) + (viewport?.height || innerHeight) - 12 };
    const r = { left: Math.max(view.left, raw.left), top: Math.max(view.top, raw.top), right: Math.min(view.right, raw.right), bottom: Math.min(view.bottom, raw.bottom) };
    if (r.right <= r.left || r.bottom <= r.top) return unavailable();
    let maxHeight = Math.max(100, view.bottom - view.top);
    const naturalHeight = wrapper.scrollHeight + wrapper.offsetHeight - wrapper.clientHeight;
    const protectedNodes = [...document.querySelectorAll('[role="dialog"] .modal-footer button, [role="dialog"] .modal-header button, button[aria-expanded="true"], .select-dropdown-portal[role="listbox"], [data-tour="key-group-dropdown"], [data-tour="recharge-tabs"] button, #s2o-exit, #s2o-quota-warning')];
    const protectedRects = [r, ...protectedNodes.map(bounds).filter((value): value is DOMRect => !!value)];
    const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(max, v));
    const candidates = () => {
      const width = wrapper.getBoundingClientRect().width, height = Math.min(naturalHeight, maxHeight);
      const x = align === 'end' ? r.right - width : align === 'start' ? r.left : (r.left + r.right - width) / 2;
      const y = align === 'end' ? r.bottom - height : align === 'start' ? r.top : (r.top + r.bottom - height) / 2;
      return [...new Set([side, 'top', 'bottom', 'left', 'right'])].map(position => {
        const left = clamp(position === 'left' ? r.left - width - 12 : position === 'right' ? r.right + 12 : x, view.left, view.right - width);
        const top = clamp(position === 'top' ? r.top - height - 12 : position === 'bottom' ? r.bottom + 12 : y, view.top, view.bottom - height);
        return { side: position, left, top, right: left + width, bottom: top + height, width, height };
      });
    };
    const choose = () => {
      const boxes = candidates();
      const clear = (box: typeof boxes[number]) => protectedRects.every(item => !intersects(box, item, 6));
      const adjacent = boxes.find(clear);
      if (adjacent) return adjacent;
      // A native horizontal control can block every adjacent placement. Try its
      // edges with the same horizontal position before shrinking the guide.
      return boxes.flatMap(box => protectedRects
        .filter(item => box.left < item.right + 6 && box.right > item.left - 6)
        .flatMap(item => [item.bottom + 6, item.top - box.height - 6].map(edge => {
          const top = clamp(edge, view.top, view.bottom - box.height);
          return { ...box, top, bottom: top + box.height, distance: Math.abs(top - box.top) };
        })))
        .sort((a, b) => a.distance - b.distance).find(clear);
    };
    let result = choose();
    if (!result) {
      // Narrow screens: shrink the guide, rather than cover a native action/footer.
      const boxes = candidates(), top = boxes.find(box => box.side === 'top')!, bottom = boxes.find(box => box.side === 'bottom')!;
      const crosses = (box: any, item: any) => box.left < item.right + 6 && box.right > item.left - 6;
      const aboveStart = Math.max(view.top, ...protectedRects.filter(item => item.bottom <= r.top && crosses(top, item)).map(item => item.bottom + 6));
      const belowEnd = Math.min(view.bottom, ...protectedRects.filter(item => item.top >= r.bottom && crosses(bottom, item)).map(item => item.top - 6));
      const above = r.top - aboveStart - 12, below = belowEnd - r.bottom - 12;
      const space = Math.max(above, below);
      if (space >= 120) {
        maxHeight = Math.floor(space);
        result = choose();
      }
    }
    // An oversized information region may fill the screen. Keep its guide in
    // view; action steps use a specific input/button anchor instead of that region.
    if (!result) {
      if (required || target.matches('button,input,textarea,select,[role="option"]') || target.closest('label')?.querySelector('input')) return unavailable();
      const controls = protectedNodes.filter(node => node.id !== 's2o-quota-warning').map(bounds).filter((value): value is DOMRect => !!value);
      result = candidates().filter(box => controls.every(item => !intersects(box, item, 6)))
        .sort((a, b) => protectedRects.filter(r => intersects(a, r)).length - protectedRects.filter(r => intersects(b, r)).length)[0];
      if (!result) return unavailable();
    }
    current.deadline = null;
    if (wrapper.hasAttribute('data-s2o-position-waiting')) wrapper.removeAttribute('data-s2o-position-waiting');
    put(wrapper, '--s2o-popover-max-height', maxHeight + 'px');
    put(wrapper, '--s2o-popover-x', Math.round(result.left) + 'px');
    put(wrapper, '--s2o-popover-y', Math.round(result.top) + 'px');
    if (wrapper.dataset.s2oPlacement !== result.side) wrapper.dataset.s2oPlacement = result.side;
  }
  function refresh() { try { position(); } catch { onUnavailable(); } }
  return {
    set(wrapper: HTMLElement, options: any) { current = { wrapper, ...options, deadline: options.deadline ?? performance.now() + 8000 }; refresh(); },
    refresh,
    destroy() { current = null; },
  };
}
