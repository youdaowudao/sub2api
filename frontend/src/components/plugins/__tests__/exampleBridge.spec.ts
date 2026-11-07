import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const sdk = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../../../../backend/pkg/pluginapi/examples/common/bridge.js'), 'utf8')
type SDKMessage = Record<string, unknown>
class TestPort {
  onmessage: ((event: { data: SDKMessage }) => void) | null = null
  postMessage = vi.fn()
  close = vi.fn()
  start = vi.fn()
}

function documentSDK(version: 1 | 2, loaded = false) {
  const listeners = new Map<string, Array<(event: unknown) => void>>()
  const channels: Array<{ port1: TestPort; port2: TestPort }> = []
  const parent = { postMessage: vi.fn() }
  const document = { currentScript: { dataset: { bridgeVersion: String(version) } }, readyState: loaded ? 'complete' : 'loading' }
  const window = {
    bridge: undefined as unknown as { request: (type: string, values?: SDKMessage) => Promise<SDKMessage> },
    addEventListener: (type: string, callback: (event: unknown) => void) => { listeners.set(type, [...listeners.get(type) || [], callback]) },
    dispatchEvent: vi.fn(),
  }
  class TestChannel {
    port1 = new TestPort()
    port2 = new TestPort()
    constructor() { channels.push(this) }
  }
  runInNewContext(sdk, {
    document, window, parent, location: { href: 'https://host.test/plugin.html#bridge_token=document-proof', hash: '#bridge_token=document-proof' },
    URL, URLSearchParams, MessageChannel: TestChannel, CustomEvent, setTimeout, clearTimeout,
  })
  const emit = (type: string, event: unknown = {}) => { for (const listener of listeners.get(type) || []) listener(event) }
  const fromHost = (data: SDKMessage) => emit('message', { source: parent, origin: 'https://host.test', data: { source: 'sub2api-plugin-host', ...data } })
  const challenge = (value = 'public-challenge') => fromHost({ type: 'bridge.challenge', challenge: value })
  const acknowledge = () => channels.at(-1)!.port1.onmessage!({ data: { source: 'sub2api-plugin-host', bridge_token: 'document-proof', type: 'bridge.connected' } })
  return { window, parent, channels, emit, fromHost, challenge, acknowledge }
}

describe('the actual shared example SDK', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

  it('queues early operations until its own load and authenticated port acknowledgement', async () => {
    const client = documentSDK(2)
    const completion = client.window.bridge.request('plugin.call', { method: 'read' })
    client.challenge()
    expect(client.parent.postMessage).not.toHaveBeenCalled()
    expect(client.channels).toHaveLength(0)
    client.emit('load')
    expect(client.parent.postMessage).toHaveBeenLastCalledWith({ source: 'sub2api-plugin-ui', type: 'bridge.ready' }, 'https://host.test')
    client.challenge()
    const channel = client.channels[0]!
    expect(client.parent.postMessage).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'bridge.connect', bridge_token: 'document-proof', challenge: 'public-challenge' }), 'https://host.test', [channel.port2])
    expect(channel.port1.postMessage).not.toHaveBeenCalled()
    client.acknowledge()
    expect(channel.port1.postMessage).toHaveBeenCalledOnce()
    const request = channel.port1.postMessage.mock.calls[0]![0] as SDKMessage
    // A window response cannot complete a Bridge 2 business operation.
    client.fromHost({ type: 'plugin.call.result', bridge_token: 'document-proof', request_id: request.request_id, ok: true, result: 'wrong-window' })
    channel.port1.onmessage!({ data: { source: 'sub2api-plugin-host', bridge_token: 'document-proof', type: 'plugin.call.result', request_id: request.request_id, ok: true, result: 'proved-port' } })
    await expect(completion).resolves.toHaveProperty('result', 'proved-port')
    client.emit('pagehide')
  })

  it('supports late installation and never automatically replays a dispatched write', async () => {
    const client = documentSDK(2, true)
    expect(client.parent.postMessage).toHaveBeenLastCalledWith({ source: 'sub2api-plugin-ui', type: 'bridge.ready' }, 'https://host.test')
    client.challenge()
    client.acknowledge()
    const write = client.window.bridge.request('plugin.call', { method: 'write', payload: { value: 'only-once' } })
    const rejected = write.catch(error => String(error.message))
    const old = client.channels[0]!
    expect(old.port1.postMessage).toHaveBeenCalledOnce()
    client.challenge()
    expect(client.channels).toHaveLength(1)
    expect(old.port1.postMessage).toHaveBeenCalledOnce()
    client.challenge('next-document-challenge')
    expect(old.port1.close).toHaveBeenCalledOnce()
    expect(await rejected).toContain('Do not retry a write automatically')
    client.acknowledge()
    expect(client.channels[1]!.port1.postMessage).not.toHaveBeenCalled()
    client.emit('pagehide')
  })

  it('preserves the explicit Bridge 1 window request and response contract', async () => {
    const client = documentSDK(1)
    const completion = client.window.bridge.request('config.load')
    expect(client.channels).toHaveLength(0)
    expect(client.parent.postMessage).toHaveBeenCalledWith(expect.objectContaining({ type: 'config.load', bridge_token: 'document-proof' }), 'https://host.test')
    const request = client.parent.postMessage.mock.calls[0]![0] as SDKMessage
    client.fromHost({ type: 'config.load.result', bridge_token: 'document-proof', request_id: request.request_id, ok: true, config: { saved: true } })
    await expect(completion).resolves.toHaveProperty('config.saved', true)
    client.emit('pagehide')
  })
})
