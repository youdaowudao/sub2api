import { createAttention } from './attention';
import { createPopoverPosition } from './popoverPosition';

// Owns one Driver lifecycle. Business forms and requests are never controlled here.
export function createTourSession(driverFactory: any, options: { storage?: Storage | null; onStop?: (reason: string) => void } = {}) {
  let storage = options.storage;
  if (!('storage' in options)) { try { storage = globalThis.sessionStorage; } catch { storage = null; } }
  let active: any = null, suppressed = false;
  const history: string[] = [], resources = { listeners: 0, observers: 0, timers: 0, frames: 0, animations: 0 };
  const storageKey = 's2o:visit-suppressed:v1';
  const saved = () => { try { return storage?.getItem(storageKey) === '1'; } catch { return false; } };
  const zero = () => Object.values(resources).every(n => n === 0);

  function waitTarget(selector: any, signal: AbortSignal, timeout: number, allowDisabled = false): Promise<any> {
    if (!selector) return Promise.resolve(undefined); // A centered introduction/confirmation.
    return new Promise<any>((resolve, reject) => {
      let done = false, frame: number | undefined;
      const observer = new MutationObserver(check);
      resources.observers++;
      const timer = setTimeout(() => finish(new Error('target-timeout')), Math.min(Math.max(timeout, 1), 8000));
      resources.timers++;
      const abort = () => finish(new DOMException('Cancelled', 'AbortError'));
      function finish(error: Error | null, target?: Element) {
        if (done) return;
        done = true; observer.disconnect(); resources.observers--;
        clearTimeout(timer); resources.timers--;
        if (frame !== undefined) { cancelAnimationFrame(frame); frame = undefined; resources.frames--; }
        signal.removeEventListener('abort', abort);
        error ? reject(error) : resolve(target);
      }
      function check() {
        if (done) return;
        try {
          const candidates = typeof selector === 'function' ? [selector()].filter(Boolean) : document.querySelectorAll(selector);
          if (candidates.length > 1) return finish(new Error('ambiguous-target'));
          const target = candidates[0];
          if (!target) return;
          const rect = target.getBoundingClientRect(), style = getComputedStyle(target);
          if (rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.left < innerWidth && style.visibility !== 'hidden' && style.display !== 'none' && (allowDisabled || !target.disabled)) finish(null, target);
        } catch { finish(new Error('invalid-target')); }
      }
      function tick() { frame = undefined; resources.frames--; check(); if (!done) { frame = requestAnimationFrame(tick); resources.frames++; } }
      signal.addEventListener('abort', abort, { once: true });
      observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true });
      if (signal.aborted) abort();
      else { check(); if (!done) { frame = requestAnimationFrame(tick); resources.frames++; } }
    });
  }

  async function show(selector: any, settings: any = {}) {
    const mine = active;
    if (!mine || mine.controller.signal.aborted) return false;
    mine.attention.setStep('');
    if (active !== mine || mine.controller.signal.aborted) return false;
    mine.wait?.abort();
    const waiting = new AbortController(); mine.wait = waiting;
    const abort = () => waiting.abort();
    mine.controller.signal.addEventListener('abort', abort, { once: true });
    const layoutDeadline = performance.now() + Math.min(Math.max(settings.timeout ?? 8000, 1), 8000);
    try {
      const element = await waitTarget(selector, waiting.signal, settings.timeout ?? 8000, settings.allowDisabled);
      if (active !== mine || waiting.signal.aborted) return false;
      const horizontal: [HTMLElement, number][] = [];
      for (let parent = element?.parentElement; parent; parent = parent.parentElement) {
        if (!mine.scroll.has(parent)) mine.scroll.set(parent, { x: parent.scrollLeft, y: parent.scrollTop });
        if (settings.alignRowStart) horizontal.push([parent, parent === element.closest('.table-wrapper') ? 0 : parent.scrollLeft]);
      }
      const horizontalWindow = scrollX;
      mine.next = settings.onNext;
      mine.previous = settings.onPrevious;
      mine.step = settings.id || '';
      mine.placement = { anchor: settings.placement?.anchor, fallback: element, required: settings.placement?.required, side: settings.placement?.side || settings.popover?.side, align: settings.placement?.align, deadline: layoutDeadline };
      const popover = { title: '本地验证', description: '请自行操作真实页面；随时可退出。', showButtons: ['next'], ...settings.popover };
      const step = { element, popover };
      if (!mine.engine) {
        mine.engine = driverFactory({
          animate: mine.animate, allowClose: false, allowKeyboardControl: false,
          showButtons: ['next'], popoverClass: settings.popoverClass || 's2o-popover',
          onNextClick: () => {
            try { Promise.resolve(mine.next ? mine.next() : mine.stop('completed')).catch(() => mine.stop('error:step')); }
            catch { mine.stop('error:step'); }
          },
          onPrevClick: () => {
            try { Promise.resolve(mine.previous?.()).catch(() => mine.stop('error:step')); }
            catch { mine.stop('error:step'); }
          },
          onS2oError: () => mine.stop('error:engine-frame'),
          onPopoverRender: ({ wrapper }: { wrapper: HTMLElement }) => {
            wrapper.dataset.s2oStep = mine.step;
            if (mine.renderFailure) throw new Error('render-failure');
            const quit = document.createElement('button');
            quit.type = 'button'; quit.className = 's2o-popover-exit'; quit.textContent = '退出'; quit.setAttribute('aria-label', '退出引导');
            wrapper.append(quit);
            mine.position.set(wrapper, mine.placement);
            if (active !== mine || mine.controller.signal.aborted) throw new Error('popover-position');
          },
          steps: [step],
        });
        mine.state = 'active'; mine.engine.drive();
      } else { mine.state = 'active'; mine.engine.highlight(step); }
      // Driver centers a wide row horizontally, moving its bounds across the
      // sidebar. Introduce it from the first column instead. The saved original
      // offsets are still restored on exit; Driver retains vertical scrolling.
      if (active === mine && settings.alignRowStart) {
        for (const [parent, x] of horizontal) parent.scrollLeft = x;
        window.scrollTo(horizontalWindow, scrollY);
        mine.engine.refresh();
      }
      if (active === mine) { mine.position.refresh(); mine.attention.setStep(mine.step, settings.attention); mine.attention.refresh(); }
      return active === mine;
    } catch (error: any) {
      if (!waiting.signal.aborted) mine.stop('error:' + error.message);
      return false;
    } finally {
      mine.controller.signal.removeEventListener('abort', abort);
      if (mine.wait === waiting) mine.wait = null;
    }
  }

  async function start(selector: any, { manual = true, animate = false, renderFailure = false, ...settings }: any = {}) {
    if (new URLSearchParams(location.search).get('onboarding') === 'off') return false;
    if (active || (!manual && (suppressed || saved()))) return false;
    if (document.querySelector('.driver-overlay, .driver-popover') || document.body.classList.contains('driver-active')) return false;
    const mine: any = { controller: new AbortController(), engine: null, state: 'waiting', cleanups: [], scroll: new Map<HTMLElement, { x: number; y: number }>(), focus: document.activeElement, animate, renderFailure };
    active = mine;
    mine.position = createPopoverPosition(() => mine.stop('error:popover-position'));
    mine.attention = createAttention(count => { resources.animations = count; }, () => mine.stop('error:attention-unavailable'));
    const refresh = () => { mine.position.refresh(); mine.attention.refresh(); };
    const attentionTimer = setInterval(refresh, 150);
    resources.timers++;
    mine.cleanups.push(() => { clearInterval(attentionTimer); resources.timers--; });
    const position = { x: scrollX, y: scrollY };
    const exit = document.createElement('button');
    exit.id = 's2o-exit'; exit.type = 'button'; exit.textContent = '退出引导';
    Object.assign(exit.style, { position: 'fixed', left: 'max(12px, env(safe-area-inset-left, 0px))', bottom: 'max(12px, env(safe-area-inset-bottom, 0px))', zIndex: '2147483647', pointerEvents: 'auto', padding: '8px 16px', color: 'white', background: '#b91c1c', border: '2px solid white', borderRadius: '6px', cursor: 'pointer' });
    document.body.append(exit);
    function listen(target: any, type: string, handler: (event: any) => void, eventOptions: AddEventListenerOptions = {}) {
      target.addEventListener(type, handler, eventOptions); resources.listeners++;
      mine.cleanups.push(() => { target.removeEventListener(type, handler, eventOptions); resources.listeners--; });
    }
    function stop(reason = 'cancelled') {
      if (active !== mine || mine.state === 'stopped') return;
      const pausing = reason === 'paused:chapter';
      const currentFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      const guideFocused = !!currentFocus?.closest('.s2o-popover, #s2o-exit');
      const currentPosition = { x: scrollX, y: scrollY };
      const currentScroll = pausing ? new Map<HTMLElement, { x: number; y: number }>([...mine.scroll.keys()].filter((element: HTMLElement) => element.isConnected).map((element: HTMLElement) => [element, { x: element.scrollLeft, y: element.scrollTop }] as const)) : null;
      mine.state = 'stopped';
      if (reason !== 'suspended' && !pausing) {
        suppressed = true;
        try { storage?.setItem(storageKey, '1'); } catch { /* Memory still suppresses this document. */ }
      }
      history.push(reason); mine.controller.abort(); mine.position.destroy(); mine.attention.destroy();
      let destroyFailed = false;
      try { mine.engine?.destroy(); mine.engine?.setConfig({}); } catch { destroyFailed = true; }
      finally {
        for (const cleanup of mine.cleanups.splice(0)) cleanup();
        exit.remove(); active = null;
        if (pausing) {
          if (currentFocus?.isConnected && currentFocus !== document.body) currentFocus.focus({ preventScroll: true });
          else if ((guideFocused || currentFocus === document.body) && document.activeElement instanceof HTMLElement) document.activeElement.blur();
          for (const [element, offset] of currentScroll!) element.scrollTo(offset.x, offset.y);
          window.scrollTo(currentPosition.x, currentPosition.y);
        } else {
          const dialog = [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].filter(el => !el.classList.contains('s2o-driver-popover') && el.getBoundingClientRect().width > 0).at(-1);
          let focus = mine.focus;
          if (dialog && !dialog.contains(focus)) {
            focus = dialog.contains(document.activeElement) ? document.activeElement : dialog.querySelector('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]');
          }
          if (focus?.isConnected) focus.focus({ preventScroll: true });
          for (const [element, offset] of mine.scroll) element.scrollTo(offset.x, offset.y);
          window.scrollTo(position.x, position.y);
        }
        if (destroyFailed) history.push('error:destroy');
        mine.engine = null;
        options.onStop?.(destroyFailed ? 'error:destroy' : reason);
      }
    }
    mine.stop = stop;
    const attentionObserver = new MutationObserver(refresh);
    attentionObserver.observe(document.body, { childList: true, attributes: true, subtree: true }); resources.observers++;
    mine.cleanups.push(() => { attentionObserver.disconnect(); resources.observers--; });
    listen(document, 'input', () => mine.attention.refresh(), { capture: true });
    listen(document, 'change', () => mine.attention.refresh(), { capture: true });
    let composing = false;
    listen(document, 'compositionstart', () => { composing = true; mine.attention.setComposing(true); }, { capture: true });
    listen(document, 'compositionend', () => { composing = false; mine.attention.setComposing(false); }, { capture: true });
    listen(window, 'blur', () => { composing = false; mine.attention.setComposing(false); });
    listen(exit, 'click', () => stop('cancelled:exit-button'));
    listen(document, 'click', event => {
      if (!event.target.closest?.('.s2o-popover .s2o-popover-exit')) return;
      event.preventDefault(); event.stopImmediatePropagation(); stop('cancelled:exit-button');
    }, { capture: true });
    listen(window, 'resize', refresh);
    listen(window, 'scroll', refresh, { capture: true, passive: true });
    if (window.visualViewport) { listen(window.visualViewport, 'resize', refresh); listen(window.visualViewport, 'scroll', refresh); }
    listen(window, 'keydown', event => {
      if (composing || event.isComposing || event.keyCode === 229) {
        // Preserve native IME default handling; candidate Escape/Tab must not
        // reach page dialog shortcuts or Driver's focus trap during this tour.
        if (event.key === 'Escape' || event.key === 'Tab') event.stopImmediatePropagation();
        return;
      }
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); stop('cancelled:escape'); }
      else if (event.key === 'Tab' && event.shiftKey && document.activeElement !== exit) {
        event.preventDefault(); event.stopImmediatePropagation(); exit.focus();
      }
    }, { capture: true });
    return show(selector, settings);
  }
  return { start, show, stop: (reason?: string) => active?.stop(reason), refresh: () => { active?.engine?.refresh(); active?.position.refresh(); active?.attention.refresh(); }, history, resources,
    pause() {
      if (!active) return false;
      active.stop('paused:chapter');
      return !active && zero() && history[history.length - 1] === 'paused:chapter';
    },
    preserveQuickStartScroll() {
      if (!active) return;
      for (const element of active.scroll.keys()) if (element.matches('.s2o-quick-start')) active.scroll.delete(element);
    },
    get state() { return active?.state || 'idle'; }, get clean() { return !active && zero(); } };
}
