/* Shared example asset; packaged with each UI, never loaded by the host SPA. */
(() => {
  const version = Number(document.currentScript?.dataset.bridgeVersion);
  if (version !== 1 && version !== 2) throw new Error('Select Bridge version 1 or 2 explicitly');
  const token = new URLSearchParams(location.hash.slice(1)).get('bridge_token');
  const origin = new URL(location.href).origin;
  const pending = new Map();
  let sequence = 0;
  let port;
  let connected = false;
  let submittedChallenge;
  let documentLoaded = document.readyState === 'complete';

  function receive(message) {
    if (!message || message.source !== 'sub2api-plugin-host' || message.bridge_token !== token) return;
    if (message.type === 'context.changed') {
      window.dispatchEvent(new CustomEvent('plugin-context', { detail: message.context }));
      return;
    }
    const waiter = pending.get(message.request_id);
    if (!waiter) return;
    clearTimeout(waiter.timeout);
    pending.delete(message.request_id);
    if (message.ok) waiter.resolve(message);
    else waiter.reject(new Error(message.error?.message || message.error || 'Request failed'));
  }

  function dispatch(waiter) {
    if (waiter.sent) return;
    if (version === 2 && !connected) return;
    waiter.sent = true;
    if (version === 2) port.postMessage(waiter.message);
    else parent.postMessage(waiter.message, origin);
  }

  window.addEventListener('message', event => {
    const message = event.data;
    if (event.source !== parent || event.origin !== origin || !message || message.source !== 'sub2api-plugin-host') return;
    if (version === 1) {
      receive(message);
      return;
    }
    if (!documentLoaded || !token || message.type !== 'bridge.challenge' ||
        typeof message.challenge !== 'string' || !message.challenge || message.challenge === submittedChallenge) return;
    submittedChallenge = message.challenge;
    connected = false;
    port?.close();
    for (const [id, waiter] of pending) {
      if (!waiter.sent) continue;
      clearTimeout(waiter.timeout);
      pending.delete(id);
      waiter.reject(new Error('Connection changed; a dispatched operation may have completed. Do not retry a write automatically.'));
    }
    const channel = new MessageChannel();
    const current = channel.port1;
    port = current;
    current.onmessage = event => {
      if (port !== current) return;
      const reply = event.data;
      if (!reply || reply.source !== 'sub2api-plugin-host' || reply.bridge_token !== token) return;
      if (reply.type === 'bridge.connected') {
        if (connected) return;
        connected = true;
        for (const waiter of pending.values()) dispatch(waiter);
      } else receive(reply);
    };
    current.start();
    parent.postMessage({ source: 'sub2api-plugin-ui', type: 'bridge.connect', bridge_token: token, challenge: message.challenge }, origin, [channel.port2]);
  });

  function ready() {
    documentLoaded = true;
    if (version === 2) parent.postMessage({ source: 'sub2api-plugin-ui', type: 'bridge.ready' }, origin);
  }
  if (documentLoaded) ready();
  else window.addEventListener('load', ready, { once: true });

  window.bridge = {
    request(type, values = {}) {
      const request_id = `example-${++sequence}`;
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => { pending.delete(request_id); reject(new Error('Request expired')); }, 35000);
        const waiter = { resolve, reject, timeout, sent: false, message: { ...values, source: 'sub2api-plugin-ui', bridge_token: token, request_id, type } };
        pending.set(request_id, waiter);
        dispatch(waiter);
      });
    }
  };
  window.addEventListener('pagehide', () => {
    connected = false;
    port?.close();
    port = undefined;
    for (const item of pending.values()) {
      clearTimeout(item.timeout);
      item.reject(new Error('Page closed; a dispatched operation may have completed.'));
    }
    pending.clear();
  });
})();
