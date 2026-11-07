import { vi } from 'vitest'

/** Component-only transport driver; real MessageChannel behavior is verified in the browser. */
export function createBridgeDriver(frame: HTMLIFrameElement, token: string) {
  const target = frame.contentWindow!
  const post = vi.fn()
  const queued: Record<string, unknown>[] = []
  let port: MessagePort | undefined
  const deliver = (data: Record<string, unknown>) => port?.onmessage?.(new MessageEvent('message', { data }))
  const windowMessages = vi.spyOn(target, 'postMessage').mockImplementation(data => {
    if (data.type !== 'bridge.challenge') return
    const current = {
      onmessage: null,
      start: vi.fn(),
      close: vi.fn(),
      postMessage: (data: Record<string, unknown>) => {
        post(data)
        if (data.type === 'bridge.connected') for (const message of queued.splice(0)) deliver(message)
      },
    } as unknown as MessagePort
    port = current
    window.dispatchEvent(new MessageEvent('message', {
      origin: 'null', source: target,
      data: { source: 'sub2api-plugin-ui', type: 'bridge.connect', bridge_token: token, challenge: data.challenge },
      ports: [current],
    }))
  })
  const message = (type: string, requestID: string, payload: Record<string, unknown> = {}, overrides: MessageEventInit = {}) => {
    const data = { source: 'sub2api-plugin-ui', bridge_token: token, type, request_id: requestID, ...payload }
    if (Object.keys(overrides).length) {
      window.dispatchEvent(new MessageEvent('message', { origin: 'null', source: target, data, ...overrides }))
    } else if (!port) queued.push(data)
    else deliver(data)
  }
  return { post, message, windowMessages }
}
