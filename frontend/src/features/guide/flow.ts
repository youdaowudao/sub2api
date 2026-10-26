import { createTourSession } from './tourSession';
import { anchors, steps } from './steps';
import { createKeyUse } from './keyUse';
import { createKeyFormGuide } from './keyForm';
import { bindGuideBusinessSession } from './businessEvents';

const editSteps = ['usage-edit-name', 'usage-edit-group', 'usage-edit-save'];
const documentTargets: Record<string, string[]> = { 'quick-command': ['windows-step', 'windows-command'], 'quick-wait': ['windows-step', 'windows-run'], 'quick-test': ['codex-test'], 'quick-prices': ['models'], 'quick-problems': ['problems'], 'quick-support': ['support'] };
const quickKeySteps = ['quick-key-pick', 'quick-key-copy', 'quick-key-close', 'quick-key-empty', 'quick-paste-wait'];

export function visible(selector: any) {
  const element = typeof selector === 'function' ? selector() : document.querySelector(selector);
  if (!element) return false;
  if (innerWidth < 1024 && element.closest('.sidebar')?.classList.contains('-translate-x-full')) return false;
  const r = element.getBoundingClientRect(), css = getComputedStyle(element);
  return r.width > 0 && r.height > 0 && r.left + r.width / 2 > 0 && r.left + r.width / 2 < innerWidth && css.visibility !== 'hidden' && css.display !== 'none';
}
const first = (selector: string): any => document.querySelector(selector);
const one = (selector: string): any => {
  const matches = document.querySelectorAll(selector);
  if (matches.length > 1) throw new Error('ambiguous-target');
  return matches[0];
};
const userButton = () => one('[data-tour="header-user-menu"]');
const mobileBalance = () => one('[data-tour="header-balance-mobile"]');
const desktopBalance = () => one('[data-tour="header-balance"]');
const rechargeTargets: Record<string, any> = {
  'balance-menu': userButton, 'balance-close': userButton, balance: desktopBalance, 'balance-mobile': mobileBalance,
  'recharge-menu': anchors.menu, 'recharge-nav': '[data-tour="sidebar-purchase"]',
  'recharge-amount': '[data-tour="recharge-amount"]', 'recharge-method': '[data-tour="recharge-method"]', 'recharge-confirm': '[data-tour="recharge-submit"]',
  'orders-menu': anchors.menu, 'orders-nav': '[data-tour="sidebar-orders"]', 'orders-list': '[data-tour="orders-list"]',
};

// All article targets live in this document. There is no iframe, message bridge or network read.
function createQuickStart({ menuId, getPublicSettings, onUpdate, onError }: any) {
  let primary = '', focus = '', listening = false, destroyed = false;
  const resources = { listeners: 0, timers: 0, nodes: 0 };
  const state = { copyGesture: false };
  const path = '/custom/' + encodeURIComponent(menuId || '');
  const root = () => document.querySelector<HTMLElement>('.s2o-quick-start');
  const find = (id: string) => root()?.querySelector<HTMLElement>(`[data-onboarding="${id}"]`);
  const copied = (event: Event) => {
    if (destroyed || !primary || !(event.target instanceof Element) || !root()?.contains(event.target)) return;
    state.copyGesture = true; onUpdate();
  };
  function suspend() {
    if (listening) { document.removeEventListener('s2o:command-copied', copied); resources.listeners--; listening = false; }
    primary = ''; focus = ''; state.copyGesture = false;
  }
  return {
    path, state, resources,
    entry: () => one(`[data-tour="sidebar-custom-${menuId}"]`),
    load() {
      if (!/^[A-Za-z0-9_-]{1,32}$/.test(menuId || '')) throw new Error('menu-binding-missing');
      const matches = getPublicSettings()?.custom_menu_items?.filter((item: any) => item.id === menuId && item.visibility !== 'admin') || [];
      if (matches.length !== 1) throw new Error('menu-binding-missing');
    },
    activate(target: string, attention = target) {
      if (destroyed) return;
      primary = target; focus = attention;
      if (!listening) { document.addEventListener('s2o:command-copied', copied); resources.listeners++; listening = true; }
    },
    target() { this.refresh(); return find(primary); },
    attention: () => find(focus),
    refresh() {
      if (destroyed || !primary || !root()) return;
      const ids = ['windows-step', 'windows-run', 'windows-command', 'codex-test', 'models', 'problems', 'support'];
      if (ids.some(id => root()!.querySelectorAll(`[data-onboarding="${id}"]`).length !== 1) || root()!.querySelectorAll('#windows-command').length !== 1) onError('error:document-target');
    },
    suspend,
    destroy() { if (destroyed) return; suspend(); destroyed = true; },
    get clean() { return resources.listeners === 0; },
  };
}

export function createFlow(driver: any, { modules = ['keys'], registrationModules = modules, moduleConfig = [], getPublicSettings = () => null, isCurrentUser = () => true, onStep = () => {}, onStop = () => {}, onContinue = null }: any = {}) {
  let session: any = null, step = '', changing = false, lastQuick: ReturnType<typeof createQuickStart> | null = null;
  const resources = { listeners: 0, observers: 0, timers: 0 };
  const tour = createTourSession(driver, { onStop: finish });
  function finish(reason: string) {
    if (reason === 'paused:chapter') return;
    if (!session) return;
    const owned = session; session = null;
    owned.releaseBusiness?.();
    owned.keyUse.destroy();
    owned.keyForm.remove();
    owned.quick.destroy();
    owned.controller.abort(); owned.observer.disconnect(); resources.observers--;
    clearInterval(owned.tick); resources.timers--;
    document.removeEventListener('click', owned.click, true);
    document.removeEventListener('keydown', owned.pickKey, true);
    resources.listeners -= 2;
    document.body.classList.remove('s2o-active');
    onStop(reason, step);
  }
  function stop(reason = 'cancelled') { if (session) { if (reason.startsWith('completed') && session.quickCompleted) tour.preserveQuickStartScroll(); tour.stop(reason); if (session) finish(reason); } }
  const moduleId = () => session?.modules[session.index];
  async function go(id: string, firstStep = false, manual = false) {
    const owned = session;
    if (!owned || changing) return;
    changing = true; step = id; owned.deadline = 0;
    try {
      if (!['usage-pick', 'quick-key-pick'].includes(id)) owned.keyUse.restoreNames();
      if (id === 'usage-open') owned.usageOpening = false;
      if (id === 'usage-edit') owned.usageEditing = false;
      if (['name', 'provider', 'group', 'submit', ...editSteps].includes(id)) owned.keyForm.showWarning({ scroll: ['submit', 'usage-edit-save'].includes(id) });
      else owned.keyForm.remove();
      const popover = { ...steps[id] };
      if (id === 'review' && owned.index + 1 < owned.modules.length) {
        popover.description = '请在列表里核对刚创建的密钥名称和分组。';
        popover.nextBtnText = '已创建，继续';
      }
      if (id === 'quick-nav' && !owned.confirmed) popover.description = '点击这里，配置 Windows 版 Codex。';
      if (id === 'provider' && !first(anchors.provider + ' input[value="openai"]:not(:disabled)')) {
        popover.description = 'OpenAI 当前没有可选分组。可以稍后重试，或退出引导继续使用页面。'; popover.showButtons = ['previous'];
      }
      if (id === 'usage-copy' && owned.copySeen) popover.nextBtnText = '继续';
      popover.doneBtnText = popover.nextBtnText;
      const usage = owned.keyUse;
      const usageTargets: Record<string, any> = {
        'usage-menu': anchors.menu, 'usage-nav': anchors.navigate, 'usage-pick': () => { usage.prepareNames(); return usage.list(); },
        'usage-row': usage.overview, 'usage-copy': usage.copy, 'usage-group': usage.group,
        'usage-groups': usage.firstGroup, 'usage-group-close': usage.group,
        'usage-open': usage.useButton, 'usage-dialog': usage.dialog, 'usage-actions': usage.actions,
        'usage-ccs': usage.ccsButton, 'usage-edit': usage.editButton,
        'usage-edit-name': owned.keyForm.dialog, 'usage-edit-group': owned.keyForm.dialog, 'usage-edit-save': owned.keyForm.dialog,
      };
      if (documentTargets[id]) owned.quick.activate(...documentTargets[id]);
      const quickTargets: Record<string, any> = {
        'quick-menu': anchors.menu, 'quick-nav': owned.quick.entry, 'quick-return-menu': anchors.menu, 'quick-return-nav': owned.quick.entry,
        'quick-keys-menu': anchors.menu, 'quick-keys-nav': anchors.navigate,
        'quick-key-pick': () => { usage.prepareNames(); return usage.list(); },
        'quick-key-copy': () => { const copy = usage.copy(), rect = copy?.getBoundingClientRect(); return copy && !copy.disabled && rect.width > 0 && rect.height > 0 && getComputedStyle(copy).visibility !== 'hidden' ? usage.overview() : undefined; },
        'quick-key-close': usage.group, 'quick-key-empty': usage.empty,
      };
      const selector = documentTargets[id] ? () => owned.quick.target() : quickTargets[id] || usageTargets[id] || rechargeTargets[id] || (id === 'submit' ? owned.keyForm.dialog : anchors[id]);
      const informational = ['balance', 'balance-mobile', 'recharge-amount', 'recharge-method', 'recharge-confirm', 'orders-list'].includes(id);
      const nextButton = () => first('.s2o-popover .s2o-driver-popover-next-btn');
      const usageAttention: Record<string, any> = {
        'usage-pick': usage.pickTarget, 'usage-copy': () => owned.copySeen ? nextButton() : usage.copy(),
        'usage-group': usage.group, 'usage-groups': usage.firstGroup, 'usage-group-close': usage.group,
        'usage-open': usage.useButton, 'usage-dialog': usage.close,
        'usage-ccs': usage.ccsButton, 'usage-edit': usage.editButton,
        'usage-edit-name': owned.keyForm.name, 'usage-edit-group': owned.keyForm.groupAttention, 'usage-edit-save': owned.keyForm.submit,
      };
      const placementAnchors: Record<string, any> = {
        name: owned.keyForm.name, provider: () => first(anchors.provider + ' input[value="openai"]:not(:disabled)')?.closest('label')?.querySelector('span'),
        group: () => owned.keyForm.group()?.querySelector('button'), submit: owned.keyForm.submit,
        'usage-pick': usage.list, 'usage-dialog': usage.close,
        'usage-edit-name': owned.keyForm.name, 'usage-edit-group': () => owned.keyForm.group()?.querySelector('button'), 'usage-edit-save': owned.keyForm.submit,
        'usage-groups': usage.group,
        'quick-key-pick': usage.list,
        'quick-key-copy': () => usage.popup() ? usage.group() : usage.copy(),
      };
      const settings = { id, manual, popover, onNext: next, onPrevious: previous,
        placement: { anchor: documentTargets[id] ? owned.quick.attention : placementAnchors[id],
          required: id === 'quick-command',
          side: ['submit', 'usage-edit-save'].includes(id) ? 'top' : popover.side,
          align: ['submit', 'usage-edit-save'].includes(id) ? 'end' : 'center' },
        alignRowStart: ['usage-row', 'quick-key-copy'].includes(id),
        allowDisabled: id === 'recharge-confirm',
        attention: id.startsWith('more-') ? { mode: 'next' } : id.startsWith('quick-') ? { selector: id === 'quick-command' ? () => owned.quick.state.copyGesture ? nextButton() : owned.quick.attention()
          : id === 'quick-wait' ? () => owned.quick.attention()
          : id === 'quick-key-pick' ? usage.pickTarget
          : id === 'quick-key-copy' ? () => usage.popup() ? usage.firstGroup() : owned.quickCopySeen ? nextButton() : usage.copy()
          : quickTargets[id] && id !== 'quick-key-empty' ? selector : nextButton }
          : id.startsWith('usage-') ? { selector: usageAttention[id] || (['usage-menu', 'usage-nav'].includes(id) ? selector : nextButton) }
          : selector && (id.startsWith('recharge-') || id.startsWith('balance') || id.startsWith('orders-')) ? { selector, mode: informational ? 'next' : 'target' } : undefined };
      const result = firstStep || tour.state === 'idle' ? await tour.start(selector, { ...settings, manual: owned.manual }) : await tour.show(selector, settings);
      if (session !== owned) return;
      if (!result) { stop('error:engine-unavailable'); if (session) finish('error:engine-unavailable'); return; }
      onStep(id, moduleId());
    } catch { stop('error:step-target');
    } finally { if (session === owned) changing = false; }
  }
  function beginModule() {
    if (!session) return;
    if (moduleId() === 'keys') {
      if (location.pathname === '/keys') return go(visible(anchors.name) ? 'name' : 'create');
      return go(visible(anchors.navigate) ? 'navigate' : 'menu');
    }
    if (moduleId() === 'key-use') {
      if (session.keyUse.dialog()) return stop('cancelled:dialog-open');
      return go(location.pathname === '/keys' ? 'usage-pick' : visible(anchors.navigate) ? 'usage-nav' : 'usage-menu');
    }
    if (moduleId() === 'quick-start') return beginQuick();
    if (moduleId() === 'more') return go('more-help');
    if (visible(anchors.name)) return stop('cancelled:dialog-open');
    if (visible(desktopBalance)) return go('balance');
    return go(visible(mobileBalance) ? 'balance-mobile' : 'balance-menu');
  }
  async function beginQuick(resumeStep = 'quick-command') {
    const owned = session; if (!owned) return;
    owned.checking = true;
    try {
      await owned.quick.load(); if (session !== owned) return;
      if (quickKeySteps.includes(resumeStep) || ['quick-keys-nav', 'quick-keys-menu'].includes(resumeStep)) return openQuickKeys(resumeStep);
      return openQuickDocument(Object.prototype.hasOwnProperty.call(documentTargets, resumeStep) ? resumeStep : ['quick-return-menu', 'quick-return-nav'].includes(resumeStep) ? 'quick-test' : 'quick-command');
    } catch { if (session === owned) stop('error:quick-start-entry'); }
    finally { owned.checking = false; }
  }
  function openQuickDocument(target: string) {
    const owned = session; if (!owned) return;
    owned.quickResume = target;
    if (location.pathname === owned.quick.path) return go(target);
    return go(visible(owned.quick.entry) ? target === 'quick-command' ? 'quick-nav' : 'quick-return-nav' : target === 'quick-command' ? 'quick-menu' : 'quick-return-menu');
  }
  function openQuickKeys(resumeStep = 'quick-key-pick') {
    const owned = session; if (!owned) return;
    owned.quick.suspend();
    if (owned.keyUse.dialog()) return stop('cancelled:dialog-open');
    if (location.pathname === '/keys') return go(resumeStep === 'quick-paste-wait' ? resumeStep : 'quick-key-pick');
    return go(visible(anchors.navigate) ? 'quick-keys-nav' : 'quick-keys-menu');
  }
  function enterRecharge() {
    const owned = session; if (!owned || owned.checking) return;
    const marker = first('[data-guide-recharge]')?.getAttribute('data-guide-recharge');
    if (!marker || marker === 'loading') {
      if (!owned.rechargeDeadline) owned.rechargeDeadline = Date.now() + 8000;
      if (Date.now() >= owned.rechargeDeadline) stop('error:target-timeout');
      return;
    }
    owned.rechargeDeadline = 0; owned.rechargePending = false;
    if (marker === 'busy') return stop('cancelled:recharge-busy');
    return go(marker === 'unavailable' ? 'recharge-unavailable' : 'recharge-amount');
  }
  function openRecharge() {
    if (!session) return;
    if (getPublicSettings()?.payment_enabled === false) return go('recharge-unavailable');
    if (location.pathname === '/purchase') { session.rechargePending = true; return enterRecharge(); }
    return go(visible(rechargeTargets['recharge-nav']) ? 'recharge-nav' : first(rechargeTargets['recharge-nav']) ? 'recharge-menu' : 'recharge-nav');
  }
  function openOrders() {
    if (location.pathname === '/orders') return go('orders-list');
    const selector = rechargeTargets['orders-nav'];
    return go(first(selector) && !visible(selector) ? 'orders-menu' : 'orders-nav');
  }
  function completeModule() {
    const owned = session; if (!owned) return;
    if (moduleId() === 'keys') owned.confirmed = true;
    if (moduleId() === 'quick-start') { owned.quickCompleted = true; owned.quick.destroy(); }
    if (owned.index + 1 < owned.modules.length) {
      owned.index++; step = 'chapter-break'; owned.deadline = 0;
      const paused = tour.pause();
      if (session !== owned) return;
      if (!paused) return stop('error:engine-frame');
      document.body.classList.remove('s2o-active');
      owned.deadline = Date.now() + 5000;
      onStep(step, moduleId());
      return;
    }
    if (owned.modules.length === 1 && owned.modules[0] === 'keys') return stop('completed:user_confirmed');
    return go('finish');
  }
  function afterClient() { return go(session?.keyUse.ccsButton() ? 'usage-ccs' : 'usage-edit'); }
  const paused = () => !!session && step === 'chapter-break';
  const dialogOpen = () => [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].some(element => !element.matches('.s2o-popover') && visible(() => element));
  async function continueChapter() {
    const owned = session;
    if (!owned || !paused() || changing || owned.checking || !isCurrentUser(owned.event.userId) || dialogOpen()) return false;
    owned.checking = true;
    document.body.classList.add('s2o-active');
    try { await beginModule(); return session === owned && !paused(); }
    finally { if (session === owned) owned.checking = false; }
  }
  async function next() {
    if (!session || changing) return;
    if (step === 'welcome') return beginModule();
    if (step === 'more-help') return go('more-tools');
    if (step === 'more-tools') return completeModule();
    if (step === 'name') { if (!first(anchors.name)?.value.trim()) { first(anchors.name)?.focus(); return; } return go('provider'); }
    if (step === 'provider') {
      if (!first(anchors.provider + ' input[value="openai"]:checked:not(:disabled)')) { first(anchors.provider + ' input[value="openai"]:not(:disabled)')?.focus({ preventScroll: true }); return; }
      return go('group');
    }
    if (step === 'group') return go('submit');

    if (step === 'review') return completeModule();
    if (step === 'usage-row') return go('usage-copy');
    if (step === 'usage-copy') return go('usage-group');
    if (['usage-group', 'usage-groups'].includes(step)) return go(session.keyUse.popup() ? 'usage-group-close' : 'usage-open');
    if (step === 'usage-open') return afterClient();
    if (step === 'usage-ccs') return go('usage-edit');
    if (step === 'usage-edit') return go('usage-actions');
    if (step === 'usage-edit-name') return go('usage-edit-group');
    if (step === 'usage-edit-group') return go('usage-edit-save');
    if (['usage-empty', 'usage-actions'].includes(step)) return completeModule();
    if (step === 'quick-command') return go('quick-wait');
    if (step === 'quick-wait') return openQuickKeys();
    if (step === 'quick-key-copy') return session.keyUse.popup() ? go('quick-key-close') : go('quick-paste-wait');
    if (step === 'quick-paste-wait') return openQuickDocument('quick-test');
    if (step === 'quick-test') { session.quickConfirmed = true; return go('quick-prices'); }
    if (['quick-prices', 'quick-problems'].includes(step)) return go('quick-support');
    if (step === 'quick-support') return completeModule();
    if (step === 'balance') return openRecharge();
    if (step === 'balance-mobile') return visible(mobileBalance) ? go('balance-close') : openRecharge();
    if (step === 'recharge-amount') return go('recharge-method');
    if (step === 'recharge-method') return go('recharge-confirm');
    if (step === 'recharge-confirm') return openOrders();
    if (['orders-list', 'recharge-unavailable'].includes(step)) return completeModule();
    if (step === 'finish') return stop(session.confirmed ? 'completed:user_confirmed' : session.quickConfirmed ? 'completed:quickstart_user_confirmed' : 'completed:tour_viewed');
  }
  function previous() {
    return go(({ provider: 'name', group: 'provider', submit: 'group', review: visible(anchors.name) ? 'name' : 'create', 'usage-edit-save': 'usage-edit-name', 'quick-wait': 'quick-command', 'quick-test': 'quick-problems', 'recharge-method': 'recharge-amount', 'recharge-confirm': 'recharge-method', 'more-tools': 'more-help' } as Record<string, string>)[step] || 'welcome');
  }
  function inspect() {
    const owned = session;
    if (!owned || changing || owned.checking) return;
    try {
    if (/^\/(login|register|email-verify|auth|setup)(\/|$)/.test(location.pathname)) return stop('cancelled:account-route');
    if (paused()) {
      if (owned.deadline && Date.now() >= owned.deadline) {
        owned.deadline = 0;
        if (onContinue) onContinue();
        else void continueChapter();
      }
      return;
    }
    if (['create', 'name', 'provider', 'group', 'submit', 'review'].includes(step) && location.pathname !== '/keys') return stop('cancelled:route-changed');
    if (['recharge-amount', 'recharge-method', 'recharge-confirm'].includes(step) && location.pathname !== '/purchase') return stop('cancelled:route-changed');
    if (['recharge-amount', 'recharge-method', 'recharge-confirm'].includes(step)) {
      const status = first('[data-guide-recharge]')?.getAttribute('data-guide-recharge');
      if (status === 'busy') return stop('cancelled:recharge-busy');
      if (status === 'unavailable') return void go('recharge-unavailable');
    }
    if (step === 'orders-list' && location.pathname !== '/orders') return stop('cancelled:route-changed');
    if (step.startsWith('usage-') && !['usage-menu', 'usage-nav'].includes(step) && location.pathname !== '/keys') return stop('cancelled:route-changed');
    if (quickKeySteps.includes(step) && location.pathname !== '/keys') return stop('cancelled:route-changed');
    if (owned.rechargePending) { if (location.pathname !== '/purchase') return stop('cancelled:route-changed'); enterRecharge(); return; }
    if (owned.deadline && Date.now() > owned.deadline) return stop('error:action-timeout');
    if (documentTargets[step]) {
      if (location.pathname !== owned.quick.path) return stop('cancelled:route-changed');
      owned.quick.refresh();
    }
    if (step === 'quick-menu' && visible(owned.quick.entry)) return void go('quick-nav');
    if (step === 'quick-nav' && location.pathname === owned.quick.path) return void go(owned.quickResume || 'quick-command');
    if (step === 'quick-return-menu' && visible(owned.quick.entry)) return void go('quick-return-nav');
    if (step === 'quick-return-nav' && location.pathname === owned.quick.path) return void go(owned.quickResume || 'quick-test');
    if (step === 'quick-keys-menu' && visible(anchors.navigate)) return void go('quick-keys-nav');
    if (step === 'quick-keys-nav' && location.pathname === '/keys') return void go('quick-key-pick');
    if (quickKeySteps.includes(step)) {
      const usage = owned.keyUse;
      if (usage.dialog()) return stop('cancelled:dialog-open');
      if (step === 'quick-key-pick') {
        const rows = usage.rows();
        if (!rows.length && usage.empty()) return void go('quick-key-empty');
        usage.prepareNames();
        if (!usage.hasSelection && owned.created?.keyId) usage.selectById(owned.created.keyId);
        if (usage.row()) return void go('quick-key-copy');
        if (rows.length === 1 && !usage.hasSelection) { usage.choose(rows[0]); return void go('quick-key-copy'); }
      }
      if (['quick-key-copy', 'quick-key-close'].includes(step) && !usage.row()) {
        owned.quickCopySeen = false; return void go('quick-key-pick');
      }
      if (step === 'quick-key-copy') {
        const open = Boolean(usage.popup());
        if (owned.quickGroupOpen && !open) { owned.quickGroupOpen = false; return void go('quick-key-copy'); }
        owned.quickGroupOpen = open;
      }
      if (step === 'quick-key-close' && !usage.popup()) return void go('quick-key-copy');
    }
    if (step === 'menu' && visible(anchors.navigate)) return void go('navigate');
    if (step === 'navigate' && location.pathname === '/keys') return void go('create');
    if (step === 'create' && visible(anchors.name)) return void go('name');
    if (step === 'balance-menu' && visible(mobileBalance)) return void go('balance-mobile');
    if (step === 'balance-close' && !visible(mobileBalance)) return void openRecharge();
    if (step === 'recharge-menu' && visible(rechargeTargets['recharge-nav'])) return void go('recharge-nav');
    if (step === 'recharge-nav' && location.pathname === '/purchase') return void enterRecharge();
    if (owned.rechargePending && location.pathname === '/purchase') return void enterRecharge();
    if (step === 'orders-menu' && visible(rechargeTargets['orders-nav'])) return void go('orders-nav');
    if (step === 'orders-nav' && location.pathname === '/orders') return void go('orders-list');
    if (moduleId() === 'key-use' && step.startsWith('usage-')) {
      const usage = owned.keyUse;
      if (step === 'usage-menu' && visible(anchors.navigate)) return void go('usage-nav');
      if (step === 'usage-nav' && location.pathname === '/keys') return void go('usage-pick');
      if (step === 'usage-pick') {
        const rows = usage.rows();
        if (!rows.length && usage.empty()) return void go('usage-empty');
        usage.prepareNames();
        if (owned.usageResume && usage.row()) { const resume = owned.usageResume; owned.usageResume = null; return void go(resume); }
        if (rows.length === 1 && !usage.hasSelection) { usage.choose(rows[0]); return void go('usage-row'); }
      } else if (!['usage-menu', 'usage-nav', 'usage-empty', 'usage-dialog', ...editSteps].includes(step) && !usage.row()) {
        owned.usageResume = ['usage-groups', 'usage-group-close'].includes(step) ? 'usage-group' : step;
        return void go('usage-pick');
      }
      if (step === 'usage-copy' && owned.copySeen) {
        const button = first('.s2o-popover .s2o-driver-popover-next-btn'); if (button && button.textContent !== '继续') button.textContent = '继续';
      }
      if (step === 'usage-group' && usage.popup()) return void go('usage-groups');
      if (step === 'usage-groups' && !usage.popup()) return void go('usage-group');
      if (step === 'usage-group-close' && !usage.popup()) return void go('usage-open');
      if (step === 'usage-open' && owned.usageOpening && usage.dialog()) {
        owned.usageDialog = usage.dialog(); return void go('usage-dialog');
      }
      if (step === 'usage-edit' && owned.usageEditing && owned.keyForm.isEdit()) {
        owned.usageDialog = usage.dialog(); return void go('usage-edit-name');
      }
      if (step === 'usage-dialog' || editSteps.includes(step)) {
        if (!usage.dialog()) return void (step === 'usage-dialog' ? afterClient() : go('usage-actions'));
        if (usage.dialog() !== owned.usageDialog) return stop('cancelled:dialog-changed');
        if (editSteps.includes(step) && !owned.keyForm.isEdit()) return stop('cancelled:dialog-changed');
      } else if (usage.dialog()) return stop('cancelled:dialog-open');
    }
    if (['name', 'provider', 'group', 'submit'].includes(step) && owned.created) return void go('review');
    if (['name', 'provider', 'group', 'submit'].includes(step) && !visible(anchors.name)) {
      return stop('cancelled:dialog-closed');
    }
    } catch { stop('error:ambiguous-target'); }
  }
  async function start(event: { userId: number }, { manual = false, resume, module }: any = {}) {
    if (session) return false;
    const selected = module ? modules.filter((value: string) => value === module) : manual ? modules : registrationModules;
    if (!selected.length) return false;
    const owned: any = { event, manual, modules: [...selected], index: 0, confirmed: false, controller: new AbortController(), created: null, checking: false, deadline: 0,
      keyUse: createKeyUse(), keyForm: createKeyFormGuide(), copySeen: false, usageResume: null, usageOpening: false, usageEditing: false, usageDialog: null, quickConfirmed: false, quickResume: 'quick-command', quickCopySeen: false, quickGroupOpen: false };
    owned.quick = createQuickStart({ menuId: moduleConfig.find((item: any) => item.id === 'quick-start')?.menuId, getPublicSettings,
      onUpdate: () => { if (session === owned) tour.refresh(); }, onError: (reason: string) => stop(reason) });
    lastQuick = owned.quick;
    session = owned; changing = false;
    owned.releaseBusiness = bindGuideBusinessSession({
      sessionId: owned, userId: event.userId,
      accepts: (kind, keyId) => session === owned && isCurrentUser(event.userId) && (kind === 'key-create'
        ? ['name', 'provider', 'group', 'submit'].includes(step)
        : ['usage-copy', 'quick-key-copy'].includes(step) && keyId === owned.keyUse.selectedKeyId),
      complete: (kind, keyId) => {
        if (kind === 'key-create') owned.created = { keyId };
        else if (step === 'usage-copy') owned.copySeen = true;
        else owned.quickCopySeen = true;
        inspect(); tour.refresh();
      },
    });
    const pick = (target: Element) => {
      if (!(target instanceof Element) || !['usage-pick', 'quick-key-pick'].includes(step) || changing || !owned.keyUse.chooseAt(target)) return false;
      if (step === 'quick-key-pick') { owned.quickCopySeen = false; void go('quick-key-copy'); return true; }
      owned.copySeen = false;
      const resume = owned.usageResume; owned.usageResume = null; void go(resume || 'usage-row'); return true;
    };
    owned.pickKey = (e: KeyboardEvent) => { if (['Enter', ' '].includes(e.key) && !e.isComposing && pick(e.target as Element)) { e.preventDefault(); e.stopPropagation(); } };
    owned.click = (e: MouseEvent) => {
      if (changing || !(e.target instanceof Element)) return;
      const target = e.target;
      if (pick(e.target as Element)) return;
      if (step === 'create' && target.closest(anchors.create)) owned.created = null;
      if (step === 'usage-open' && owned.keyUse.useButton()?.contains(target)) { owned.usageOpening = true; owned.deadline = Date.now() + 8000; }
      if (step === 'usage-edit' && owned.keyUse.editButton()?.contains(target)) { owned.usageEditing = true; owned.deadline = Date.now() + 8000; }
      const selector = ({ 'recharge-menu': anchors.menu, 'recharge-nav': rechargeTargets['recharge-nav'], 'balance-menu': 'header button[aria-label]', 'balance-close': 'header button[aria-label]', 'orders-menu': anchors.menu, 'orders-nav': rechargeTargets['orders-nav'] } as Record<string, string>)[step] || anchors[step];
      if (['menu', 'navigate', 'create', 'recharge-menu', 'recharge-nav', 'balance-menu', 'balance-close', 'orders-menu', 'orders-nav'].includes(step) && selector && target.closest(selector)) owned.deadline = Date.now() + 8000;
      if (['usage-menu', 'usage-nav'].includes(step) && target.closest(step === 'usage-menu' ? anchors.menu : anchors.navigate)) owned.deadline = Date.now() + 8000;
      if (step === 'quick-nav' && owned.quick.entry()?.contains(target) || step === 'quick-menu' && target.closest(anchors.menu)) owned.deadline = Date.now() + 8000;
      if (step === 'quick-return-nav' && owned.quick.entry()?.contains(target) || ['quick-return-menu', 'quick-keys-menu'].includes(step) && target.closest(anchors.menu) || step === 'quick-keys-nav' && target.closest(anchors.navigate)) owned.deadline = Date.now() + 8000;
    };
    document.addEventListener('click', owned.click, true); document.addEventListener('keydown', owned.pickKey, true); resources.listeners += 2;
    owned.observer = new MutationObserver(inspect); owned.observer.observe(document.body, { childList: true, subtree: true }); resources.observers++;
    owned.tick = setInterval(inspect, 100); resources.timers++;
    if (resume?.step === 'chapter-break' && owned.modules.includes(resume.moduleId)) {
      owned.index = owned.modules.indexOf(resume.moduleId); step = 'chapter-break'; owned.deadline = Date.now() + 5000;
      onStep(step, moduleId());
      return true;
    }
    document.body.classList.add('s2o-active');
    await go(manual && selected.length === 1 && selected[0] === 'more' ? 'more-help' : 'welcome', true, manual);
    if (session === owned && resume) {
      const index = owned.modules.indexOf(resume.moduleId);
      if (index >= 0) {
        owned.index = index;
        if (resume.moduleId === 'keys' && location.pathname === '/keys') await go(resume.step === 'review' ? 'review' : 'create');
        else if (resume.moduleId === 'key-use') await go(location.pathname === '/keys' ? 'usage-pick' : visible(anchors.navigate) ? 'usage-nav' : 'usage-menu');
        else if (resume.moduleId === 'quick-start') await beginQuick(resume.step);
        else if (resume.moduleId === 'recharge') await (location.pathname === '/purchase' ? openRecharge() : go(location.pathname === '/orders' ? 'orders-list' : visible(desktopBalance) ? 'balance' : 'balance-menu'));
        else if (resume.moduleId === 'more') await go(['more-help', 'more-tools', 'finish'].includes(resume.step) ? resume.step : 'more-help');
      }
    }
    return session === owned;
  }
  const clean = () => !session && tour.clean && (!lastQuick || lastQuick.clean) && Object.values(resources).every(v => v === 0);
  return { start, stop, continue: continueChapter, resetForRegistration() { return clean() && tour.resetForRegistration(); }, get paused() { return paused(); }, get dialogOpen() { return dialogOpen(); }, get step() { return step; }, get moduleId() { return moduleId(); }, get modules() { return session ? [...session.modules] : []; }, get active() { return Boolean(session); },
    get clean() { return clean(); },
    get resources() { return { ...resources, engineSession: { ...tour.resources }, document: { ...(lastQuick?.resources || { listeners: 0, timers: 0, nodes: 0 }) } }; } };
}
