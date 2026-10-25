/* global s, _ */
// Local lifecycle repair for the exact Driver.js 1.4.0 bundle. No global monkey patches.
const __s2oListeners = [];
const __s2oFrames = new Set();
const __s2oTargets = new Map();
let __s2oEpoch = 0;

function __s2oListen(target, type, handler, options) {
  target.addEventListener(type, handler, options);
  __s2oListeners.push(() => target.removeEventListener(type, handler, options));
}
function __s2oFrame(callback) {
  const epoch = __s2oEpoch;
  const id = window.requestAnimationFrame(time => {
    __s2oFrames.delete(id);
    if (epoch === __s2oEpoch) {
      try { callback(time); }
      catch {
        const onError = s('onS2oError');
        if (onError) onError();
        else _()?.destroy();
      }
    }
  });
  __s2oFrames.add(id);
  return id;
}
function __s2oCancelFrame(id) {
  __s2oFrames.delete(id);
  window.cancelAnimationFrame(id);
}
function __s2oRelease() {
  __s2oEpoch++;
  for (const remove of __s2oListeners.splice(0)) remove();
  for (const id of __s2oFrames) window.cancelAnimationFrame(id);
  __s2oFrames.clear();
}
function __s2oRemember(element) {
  if (__s2oTargets.has(element)) return;
  __s2oTargets.set(element, {
    attrs: ['aria-haspopup', 'aria-expanded', 'aria-controls'].map(name => [name, element.getAttribute(name)]),
    classes: ['driver-active-element', 'driver-no-interaction'].map(name => [name, element.classList.contains(name)]),
  });
}
function __s2oRestore(element) {
  const saved = __s2oTargets.get(element);
  if (!saved) return;
  for (const [name, value] of saved.attrs) {
    if (value === null) element.removeAttribute(name);
    else element.setAttribute(name, value);
  }
  for (const [name, present] of saved.classes) element.classList.toggle(name, present);
}
function __s2oRestorePrevious(previous, next) {
  if (previous !== next) __s2oRestore(previous);
}
function __s2oRestoreAll() {
  for (const element of __s2oTargets.keys()) __s2oRestore(element);
  __s2oTargets.clear();
  document.getElementById('s2o-driver-dummy-element')?.remove();
}
export function getCleanupStats() {
  return { listeners: __s2oListeners.length, frames: __s2oFrames.size, targets: __s2oTargets.size };
}
