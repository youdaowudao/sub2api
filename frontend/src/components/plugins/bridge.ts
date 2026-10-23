export interface PluginBridgeMessage extends Record<string, unknown> {
  source: string
  bridge_token: string
  type: string
  request_id?: string
}

export interface PluginBridgeRequest {
  signal: AbortSignal
  active: () => boolean
  reply: (payload: Record<string, unknown>) => boolean
}

interface PendingRequest {
  controller: AbortController
  timeout: number
}

interface BridgeConnection {
  port: MessagePort
  token: string
}

interface BridgeOptions {
  frame: () => HTMLIFrameElement | null
  token: () => string | undefined
  version?: () => number
  accepts: (type: string) => boolean
  expectsResponse: (type: string) => boolean
  handle: (message: PluginBridgeMessage, request: PluginBridgeRequest) => Promise<void> | void
  timeoutPayload?: Record<string, unknown>
  connected?: () => void
}

/** The same bounded request lifecycle is used by configuration and business frames. */
export function createPluginBridge(options: BridgeOptions) {
  const pending = new Map<string, PendingRequest>()
  let generation = 0
  let listening = false
  let challenge: string | undefined
  let connection: BridgeConnection | undefined
  // Bridge1's 30-second timer expires only the reply. Its operations share the
  // document lifetime so even an expired request is cancelled on close/reload.
  let legacyController = new AbortController()

  const usesPort = () => options.version?.() === 2

  function reset(): void {
    generation++
    legacyController.abort()
    legacyController = new AbortController()
    for (const request of pending.values()) {
      window.clearTimeout(request.timeout)
      request.controller.abort()
    }
    pending.clear()
  }

  function disconnect(): void {
    reset()
    challenge = undefined
    if (connection) {
      connection.port.onmessage = null
      connection.port.close()
      connection = undefined
    }
  }

  function sendChallenge(): void {
    if (!challenge || connection) return
    options.frame()?.contentWindow?.postMessage({
      source: 'sub2api-plugin-host', type: 'bridge.challenge', challenge,
    }, '*')
  }

  /** A new load authorizes only a public challenge, never the window itself. */
  function documentLoaded(): void {
    disconnect()
    if (!usesPort() || !options.token()) return
    challenge = Array.from(crypto.getRandomValues(new Uint8Array(32)), byte => byte.toString(16).padStart(2, '0')).join('')
    sendChallenge()
  }

  function sendPort(target: BridgeConnection, payload: Record<string, unknown>): void {
    target.port.postMessage({ ...payload, source: 'sub2api-plugin-host', bridge_token: target.token })
  }

  function send(payload: Record<string, unknown>): void {
    if (usesPort()) {
      if (connection) sendPort(connection, payload)
      return
    }
    const token = options.token()
    if (!token) return
    options.frame()?.contentWindow?.postMessage({
      ...payload, source: 'sub2api-plugin-host', bridge_token: token,
    }, '*')
  }

  function receiveRequest(data: unknown, target?: BridgeConnection): void {
    const token = target?.token || options.token()
    const message = data as PluginBridgeMessage | null
    if (!message || typeof message !== 'object' || message.source !== 'sub2api-plugin-ui' ||
      message.bridge_token !== token || typeof message.type !== 'string' || !options.accepts(message.type)) return
    const expectsResponse = options.expectsResponse(message.type)
    const requestID = typeof message.request_id === 'string' ? message.request_id.trim() : ''
    if (expectsResponse && (!requestID || pending.has(requestID) || pending.size >= 32)) return

    const currentGeneration = generation
    const controller = target ? new AbortController() : legacyController
    const current: PendingRequest = { controller, timeout: 0 }
    const active = () => generation === currentGeneration && !controller.signal.aborted &&
      (!target || connection === target) && options.token() === token &&
      (!expectsResponse || pending.get(requestID) === current)
    const reply = (payload: Record<string, unknown>): boolean => {
      if (!expectsResponse || !active()) return false
      window.clearTimeout(current.timeout)
      pending.delete(requestID)
      const result = { type: `${message.type}.result`, request_id: requestID, ...payload }
      if (target) sendPort(target, result)
      else send(result)
      return true
    }
    if (expectsResponse) {
      pending.set(requestID, current)
      current.timeout = window.setTimeout(() => {
        if (options.timeoutPayload) reply(options.timeoutPayload)
        if (pending.get(requestID) === current) pending.delete(requestID)
        if (target) controller.abort()
      }, 30_000)
    }
    // Handlers own user-visible error messages. Keep the event listener free of
    // unhandled rejections if a handler is destroyed while awaiting a request.
    void Promise.resolve(options.handle(message, { signal: controller.signal, active, reply })).catch(() => {
      if (pending.get(requestID) === current) {
        window.clearTimeout(current.timeout)
        pending.delete(requestID)
      }
    })
  }

  function receive(event: MessageEvent): void {
    const frameWindow = options.frame()?.contentWindow
    if (!frameWindow || event.source !== frameWindow) return
    const message = event.data as PluginBridgeMessage | null
    const token = options.token()
    const trustedOrigin = event.origin === 'null'
    if (!usesPort()) {
      if (token && trustedOrigin) receiveRequest(message)
      return
    }
    if (!message || typeof message !== 'object' || message.source !== 'sub2api-plugin-ui') return
    const closeUnusedPorts = () => { for (const port of event.ports) port.close() }
    if (!token || !trustedOrigin) {
      closeUnusedPorts()
      return
    }
    if (message.type === 'bridge.ready' && event.ports.length === 0) {
      sendChallenge()
      return
    }
    if (message.type !== 'bridge.connect' || connection || !challenge || message.challenge !== challenge ||
      message.bridge_token !== token || event.ports.length !== 1) {
      closeUnusedPorts()
      return
    }
    const current: BridgeConnection = { port: event.ports[0]!, token }
    challenge = undefined
    connection = current
    current.port.onmessage = event => {
      if (connection === current && options.token() === current.token) receiveRequest(event.data, current)
    }
    current.port.start()
    sendPort(current, { type: 'bridge.connected' })
    options.connected?.()
  }

  function start(): void {
    if (listening) return
    window.addEventListener('message', receive)
    listening = true
  }

  function stop(): void {
    disconnect()
    window.removeEventListener('message', receive)
    listening = false
  }

  return { start, stop, reset, disconnect, documentLoaded, send }
}
