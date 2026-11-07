import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPluginBridge, type PluginBridgeRequest } from '../bridge'

function testPort() {
  return { onmessage: null, start: vi.fn(), close: vi.fn(), postMessage: vi.fn() } as unknown as MessagePort
}

describe('Bridge 2 document proof and revocation', () => {
  let frame: HTMLIFrameElement
  let outgoing: ReturnType<typeof vi.spyOn>
  let bridge: ReturnType<typeof createPluginBridge>
  let calls: PluginBridgeRequest[]

  beforeEach(() => {
    frame = document.createElement('iframe')
    document.body.appendChild(frame)
    outgoing = vi.spyOn(frame.contentWindow!, 'postMessage').mockImplementation(() => {})
    calls = []
    bridge = createPluginBridge({
      frame: () => frame, token: () => 'document-proof', version: () => 2,
      accepts: type => type === 'plugin.call', expectsResponse: () => true,
      handle: (_message, request) => { calls.push(request) },
    })
    bridge.start()
  })
  afterEach(() => { bridge.stop(); frame.remove(); vi.restoreAllMocks() })

  function message(data: Record<string, unknown>, ports: MessagePort[] = [], source: Window = frame.contentWindow!, origin = 'null') {
    window.dispatchEvent(new MessageEvent('message', { source, origin, ports, data: { source: 'sub2api-plugin-ui', ...data } }))
  }
  function currentChallenge(): string {
    return outgoing.mock.calls.at(-1)![0].challenge as string
  }
  function connect(port: MessagePort, challenge = currentChallenge()) {
    message({ type: 'bridge.connect', bridge_token: 'document-proof', challenge }, [port])
  }
  function invoke(port: MessagePort, id = 'request') {
    port.onmessage?.(new MessageEvent('message', { data: { source: 'sub2api-plugin-ui', bridge_token: 'document-proof', type: 'plugin.call', request_id: id } }))
  }

  it('rejects unproved documents and invalid transfers, closing every unwanted port', () => {
    const beforeLoad = testPort()
    message({ type: 'bridge.connect', bridge_token: 'document-proof', challenge: 'guessed' }, [beforeLoad])
    expect(beforeLoad.close).toHaveBeenCalledOnce()
    bridge.documentLoaded()
    const challenge = currentChallenge()
    expect(challenge.length).toBeGreaterThanOrEqual(32)
    for (const [value] of outgoing.mock.calls) {
      expect(value).not.toHaveProperty('bridge_token')
      expect(value).not.toHaveProperty('context')
      expect(value).not.toHaveProperty('result')
    }
    const badProof = testPort()
    message({ type: 'bridge.connect', bridge_token: 'wrong', challenge }, [badProof])
    const staleChallenge = testPort()
    connect(staleChallenge, `${challenge}-stale`)
    const otherWindow = testPort()
    message({ type: 'bridge.connect', bridge_token: 'document-proof', challenge }, [otherWindow], window)
    const nonOpaque = testPort()
    message({ type: 'bridge.connect', bridge_token: 'document-proof', challenge }, [nonOpaque], frame.contentWindow!, window.location.origin)
    const first = testPort(); const second = testPort()
    message({ type: 'bridge.connect', bridge_token: 'document-proof', challenge }, [first, second])
    for (const port of [badProof, staleChallenge, nonOpaque, first, second]) expect(port.close).toHaveBeenCalledOnce()
    // Another mounted bridge owns that window's transfer; this instance must
    // neither accept it nor close a port that the rightful owner can authenticate.
    expect(otherWindow.close).not.toHaveBeenCalled()
    message({ type: 'plugin.call', bridge_token: 'document-proof', request_id: 'window-business' })
    expect(calls).toHaveLength(0)
    const valid = testPort()
    connect(valid, challenge)
    invoke(valid)
    expect(calls).toHaveLength(1)
  })

  it('consumes a challenge once and keeps the proved port despite duplicate ready or connect', () => {
    bridge.documentLoaded()
    const challenge = currentChallenge(); const active = testPort()
    connect(active, challenge)
    const challengeCount = outgoing.mock.calls.length
    message({ type: 'bridge.ready' })
    expect(outgoing).toHaveBeenCalledTimes(challengeCount)
    const replacement = testPort()
    connect(replacement, challenge)
    expect(replacement.close).toHaveBeenCalledOnce()
    expect(active.close).not.toHaveBeenCalled()
    invoke(replacement, 'unproved')
    invoke(active, 'proved')
    expect(calls).toHaveLength(1)
    expect(active.postMessage).toHaveBeenCalledWith(expect.objectContaining({ type: 'bridge.connected' }))
  })

  it('lets the page and shell prove independent ports without closing each other\'s transfer', () => {
    const secondFrame = document.createElement('iframe')
    document.body.appendChild(secondFrame)
    const secondWindow = vi.spyOn(secondFrame.contentWindow!, 'postMessage').mockImplementation(() => {})
    const secondHandler = vi.fn()
    const second = createPluginBridge({
      frame: () => secondFrame, token: () => 'second-proof', version: () => 2,
      accepts: () => true, expectsResponse: () => true, handle: secondHandler,
    })
    second.start()
    try {
      bridge.documentLoaded()
      second.documentLoaded()
      const port = testPort()
      window.dispatchEvent(new MessageEvent('message', {
        source: secondFrame.contentWindow!, origin: 'null', ports: [port],
        data: { source: 'sub2api-plugin-ui', type: 'bridge.connect', bridge_token: 'second-proof', challenge: secondWindow.mock.calls.at(-1)![0].challenge },
      }))
      expect(port.close).not.toHaveBeenCalled()
      expect(port.postMessage).toHaveBeenCalledWith(expect.objectContaining({ type: 'bridge.connected' }))
      port.onmessage?.(new MessageEvent('message', { data: { source: 'sub2api-plugin-ui', bridge_token: 'second-proof', type: 'plugin.call', request_id: 'second-request' } }))
      expect(secondHandler).toHaveBeenCalledOnce()
      expect(calls).toHaveLength(0)
    } finally { second.stop(); secondFrame.remove() }
  })

  it('keeps the port on a route reset and never delivers an old request into a reloaded document', () => {
    bridge.documentLoaded()
    const oldChallenge = currentChallenge(); const oldPort = testPort()
    connect(oldPort, oldChallenge)
    invoke(oldPort)
    const beforeRoute = calls[0]!
    bridge.reset()
    expect(beforeRoute.signal.aborted).toBe(true)
    expect(oldPort.close).not.toHaveBeenCalled()
    bridge.send({ type: 'context.changed', context: { path: '/profile' } })
    expect(oldPort.postMessage).toHaveBeenLastCalledWith(expect.objectContaining({ context: { path: '/profile' } }))
    invoke(oldPort, 'same-id')
    const oldRequest = calls[1]!
    const oldListener = oldPort.onmessage!
    bridge.documentLoaded()
    expect(oldPort.close).toHaveBeenCalledOnce()
    expect(oldRequest.signal.aborted).toBe(true)
    expect(currentChallenge()).not.toBe(oldChallenge)
    const newPort = testPort()
    connect(newPort)
    invoke(newPort, 'same-id')
    const newRequest = calls[2]!
    oldListener.call(oldPort, new MessageEvent('message', { data: { source: 'sub2api-plugin-ui', bridge_token: 'document-proof', type: 'plugin.call', request_id: 'late-old-port' } }))
    expect(calls).toHaveLength(3)
    expect(oldRequest.reply({ ok: true, result: 'old-secret' })).toBe(false)
    expect(newPort.postMessage).not.toHaveBeenCalledWith(expect.objectContaining({ result: 'old-secret' }))
    expect(newRequest.reply({ ok: true, result: 'current' })).toBe(true)
    expect(newPort.postMessage).toHaveBeenLastCalledWith(expect.objectContaining({ request_id: 'same-id', result: 'current' }))
  })
})
